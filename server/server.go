package main

import (
	"DEMO2/TCPIP/mysql"
	"DEMO2/TCPIP/pre"
	"DEMO2/TCPIP/protocol"
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
)

// clientConn 表示服务端眼中的一个客户端连接。
// 这个结构既保存底层 TCP 连接，也保存登录后的用户名。
type clientConn struct {
	// conn 是客户端和服务端之间的 TCP 连接。
	conn net.Conn
	// username 登录成功前为空字符串，登录成功后保存用户名。
	username string
	// writeMu 保护当前连接的写操作。
	// 只要服务端要给这个客户端发消息，都必须先拿到这个锁。
	writeMu sync.Mutex
}

// chatServer 表示整个聊天服务端实例。
// 它把监听器、在线用户表、关闭控制都集中管理起来。
type chatServer struct {
	// listener 负责接收新的 TCP 连接。
	listener net.Listener
	// peersByConn 按连接查客户端对象。
	// 未登录和已登录的连接都会放进这个表，方便关服时统一关闭。
	peersByConn map[net.Conn]*clientConn
	// peersByName 按用户名查客户端对象。
	// 只有登录成功的用户才会放进这个表，私聊和 /list 都依赖它。
	peersByName map[string]*clientConn
	// mu 保护 peersByConn 和 peersByName 两张表。
	mu sync.RWMutex
	// shutdownCh 关闭后表示服务端正在停机。
	shutdownCh chan struct{}
	// shutdownWg 等待所有连接处理 goroutine 退出。
	shutdownWg sync.WaitGroup
	// closeOnce 保证关服流程只执行一次。
	closeOnce sync.Once
}

// main 是服务端入口函数。
// 它负责初始化数据库、启动监听、启动控制台命令监听。
func main() {
	// 初始化 MySQL 连接。
	if err := mysql.InitMysql(); err != nil {
		// 数据库连不上时，服务端不能继续运行。
		fmt.Println("初始化数据库失败:", err)
		return
	}
	// main 退出时关闭数据库连接。
	defer func() {
		// 关闭数据库失败只打印日志，不需要再做额外恢复。
		if err := mysql.CloseMysql(); err != nil {
			fmt.Println("关闭数据库失败:", err)
		}
	}()

	// 确保 user/news 表和用户名唯一索引存在。
	if err := mysql.EnsureSchema(); err != nil {
		// 表结构初始化失败时，后续注册和聊天记录都无法保证正常。
		fmt.Println("初始化数据表失败:", err)
		return
	}

	// 启动 TCP 监听，监听所有网卡的 8888 端口。
	listener, err := net.Listen("tcp", "0.0.0.0:8888")
	// 监听失败一般是端口被占用或权限问题。
	if err != nil {
		fmt.Println("启动服务端失败:", err)
		return
	}

	// 创建服务端对象，把 listener 和在线表放进去。
	server := newChatServer(listener)
	// 打印启动成功提示。
	fmt.Println("服务端已启动: 0.0.0.0:8888")
	// 单独启动一个 goroutine 读取服务端控制台命令。
	go server.watchConsoleExit()

	// serve 会阻塞运行，直到 listener 被关闭或发生异常。
	if err := server.serve(); err != nil && !server.isShuttingDown() {
		// 如果不是主动关服导致的错误，就打印异常。
		fmt.Println("服务端异常退出:", err)
	}
	// 等待所有客户端处理协程结束，防止 main 太早退出。
	server.shutdownWg.Wait()
}

// newChatServer 创建服务端对象。
// 所有 map 和 channel 都在这里初始化，避免使用时出现 nil。
func newChatServer(listener net.Listener) *chatServer {
	// 返回一个完整可用的 chatServer 指针。
	return &chatServer{
		// 保存 TCP 监听器。
		listener: listener,
		// 初始化连接表。
		peersByConn: make(map[net.Conn]*clientConn),
		// 初始化在线用户名表。
		peersByName: make(map[string]*clientConn),
		// 初始化停机信号 channel。
		shutdownCh: make(chan struct{}),
	}
}

// serve 持续接收客户端连接。
// 每接入一个连接，就启动一个 goroutine 独立处理。
func (s *chatServer) serve() error {
	// 服务端主循环一直运行，直到 listener 关闭。
	for {
		// Accept 会阻塞等待新的客户端连接。
		conn, err := s.listener.Accept()
		// Accept 返回错误时，需要区分是主动关服还是异常。
		if err != nil {
			// 主动关服时 listener.Close 会让 Accept 返回错误，这种情况不是异常。
			if s.isShuttingDown() {
				return nil
			}
			// 非关服导致的错误交给 main 打印。
			return err
		}

		// 新连接刚进来时还没有用户名。
		peer := &clientConn{conn: conn}
		// 先把连接登记到连接表，方便关服时也能关闭未登录连接。
		s.addPeer(peer)
		// 每个连接处理 goroutine 都加入 WaitGroup。
		s.shutdownWg.Add(1)
		// 为这个连接启动独立处理协程。
		go func() {
			// 协程结束时通知 WaitGroup。
			defer s.shutdownWg.Done()
			// 处理这个连接的完整生命周期。
			s.handleConnection(peer)
		}()
	}
}

// handleConnection 处理一个客户端连接从接入到断开的全过程。
// 它会根据 peer.username 是否为空，把连接分成未登录和已登录两个阶段。
func (s *chatServer) handleConnection(peer *clientConn) {
	// 连接处理结束后，无论原因是什么，都统一清理在线表和连接。
	defer s.disconnectPeer(peer)

	// 每个连接一直读包，直到客户端退出、网络断开或服务端关服。
	for {
		// 从客户端读取一个完整业务包。
		raw, err := pre.ReadPacket(peer.conn)
		// 读包失败通常表示连接断开。
		if err != nil {
			// 主动关服时会关闭连接，这类错误不需要当成异常打印。
			if !s.isShuttingDown() && !errors.Is(err, net.ErrClosed) {
				fmt.Printf("连接断开: %v, 用户: %s, 错误: %v\n", peer.conn.RemoteAddr(), peer.username, err)
			}
			// 结束当前连接处理。
			return
		}

		// 把客户端原始字符串包解析成统一命令结构。
		cmd, err := protocol.ParseClientPacket(raw)
		// 解析失败说明客户端发来的协议格式不正确。
		if err != nil {
			// 给客户端返回无效命令提示，然后继续等待下一条命令。
			_ = s.sendPacket(peer, protocol.BuildSystemErr("无效命令"))
			continue
		}

		// username 为空表示这个连接还没有登录成功。
		if peer.username == "" {
			// 未登录阶段只能处理登录或注册。
			if shouldClose := s.handleGuestCommand(peer, cmd); shouldClose {
				return
			}
			// 未登录阶段处理完后继续等下一条命令。
			continue
		}

		// username 非空表示已经登录，可以处理聊天相关命令。
		if shouldClose := s.handleAuthedCommand(peer, cmd); shouldClose {
			return
		}
	}
}

// handleGuestCommand 处理未登录连接发来的命令。
// 这个阶段只允许 LOGIN 和 REGISTER。
func (s *chatServer) handleGuestCommand(peer *clientConn, cmd protocol.Packet) bool {
	// 根据认证动作分发。
	switch cmd.Cmd {
	case protocol.CmdRegister:
		// 处理注册。
		s.handleRegister(peer, cmd)
	case protocol.CmdLogin:
		// 处理登录，登录成功后 peer.username 会被设置。
		if s.handleLogin(peer, cmd) {
			// 服务端控制台打印登录成功信息。
			fmt.Printf("用户登录成功: %s\n", peer.username)
		}
	default:
		// 未登录状态下发其他命令，都提示先登录或注册。
		_ = s.sendPacket(peer, protocol.BuildSystemErr("请先登录或注册"))
	}
	// 当前设计下，未登录命令不会主动关闭连接。
	return false
}

// handleAuthedCommand 处理已登录用户发来的聊天命令。
// 所有登录后的功能入口都集中在这里路由。
func (s *chatServer) handleAuthedCommand(peer *clientConn, cmd protocol.Packet) bool {
	// 按命令类型和动作做分发。
	switch {
	case cmd.Cmd == protocol.CmdPublic:
		// 公聊消息。
		s.handlePublicMessage(peer, cmd)
	case cmd.Cmd == protocol.CmdPrivateEnter:
		// 请求进入私聊。
		s.handlePrivateEnter(peer, cmd)
	case cmd.Cmd == protocol.CmdPrivate:
		// 私聊消息。
		s.handlePrivateMessage(peer, cmd)
	case cmd.Cmd == protocol.CmdList:
		// 在线用户列表。
		s.handleUserList(peer)
	case cmd.Cmd == protocol.CmdQuit:
		// 客户端主动退出。
		return true
	default:
		// 其他已登录命令都视为无效。
		_ = s.sendPacket(peer, protocol.BuildSystemErr("无效命令"))
	}
	// false 表示连接继续保持。
	return false
}

// handleRegister 处理用户注册。
// 注册成功后不会自动登录，只返回注册成功响应。
func (s *chatServer) handleRegister(peer *clientConn, cmd protocol.Packet) {
	// 服务端再次校验用户名，防止客户端绕过本地校验。
	if err := protocol.ValidateUsername(cmd.Username); err != nil {
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeInvalidUsername))
		return
	}
	// 服务端再次校验密码。
	if err := protocol.ValidatePassword(cmd.Password); err != nil {
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeInvalidPassword))
		return
	}

	// 调用 mysql 包注册用户，业务层不直接写 SQL。
	err := mysql.RegisterUser(cmd.Username, cmd.Password)
	// 根据数据库层返回结果转换成协议响应。
	switch {
	case err == nil:
		// 注册成功。
		_ = s.sendPacket(peer, protocol.BuildAuthOK(protocol.CmdRegister))
	case errors.Is(err, mysql.ErrNameExists):
		// 用户名重复。
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeNameExists))
	default:
		// 其他数据库错误打印到服务端控制台。
		fmt.Println("注册失败:", err)
		// 给客户端只返回数据库错误，不暴露底层细节。
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeDBError))
	}
}

// handleLogin 处理用户登录。
// 登录成功后会把这个连接登记为在线用户。
func (s *chatServer) handleLogin(peer *clientConn, cmd protocol.Packet) bool {
	// 服务端再次校验用户名。
	if err := protocol.ValidateUsername(cmd.Username); err != nil {
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeInvalidUsername))
		return false
	}
	// 服务端再次校验密码。
	if err := protocol.ValidatePassword(cmd.Password); err != nil {
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeInvalidPassword))
		return false
	}

	// 调用数据库层检查账号密码。
	result, err := mysql.CheckLogin(cmd.Username, cmd.Password)
	// 数据库查询异常时返回 DB_ERROR。
	if err != nil {
		fmt.Println("登录校验失败:", err)
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeDBError))
		return false
	}
	// 登录结果不是成功时，映射成对应错误码返回。
	if result != mysql.LoginSuccess {
		_ = s.sendPacket(peer, protocol.BuildAuthErr(mapLoginResultToCode(result)))
		return false
	}

	// 修改在线用户表前必须加写锁。
	s.mu.Lock()
	// 函数返回前释放锁。
	defer s.mu.Unlock()
	// 检查当前账号是否已经在线。
	if _, exists := s.peersByName[cmd.Username]; exists {
		_ = s.sendPacket(peer, protocol.BuildAuthErr(protocol.CodeAlreadyOnline))
		return false
	}
	// 把用户名写入当前连接对象，标志它进入已登录状态。
	peer.username = cmd.Username
	// 把用户名和连接对象绑定起来，供私聊和 /list 使用。
	s.peersByName[cmd.Username] = peer
	// 给客户端返回登录成功。
	_ = s.sendPacket(peer, protocol.BuildAuthOK(protocol.CmdLogin))
	// true 表示登录成功。
	return true
}

// handlePublicMessage 处理公聊消息。
// 公聊消息会保存到数据库，并广播给所有在线用户。
func (s *chatServer) handlePublicMessage(peer *clientConn, cmd protocol.Packet) {
	// 先校验消息不能为空。
	if err := protocol.ValidateMessage(cmd.Content); err != nil {
		_ = s.sendPacket(peer, protocol.BuildSystemErr(err.Error()))
		return
	}
	// 保存公聊消息到数据库。
	if err := mysql.SavePublicMessage(peer.username, cmd.Content); err != nil {
		// 服务端记录详细错误。
		fmt.Println("保存公聊消息失败:", err)
		// 客户端只收到简短提示。
		_ = s.sendPacket(peer, protocol.BuildSystemErr("保存消息失败"))
		return
	}

	// 构造广播给客户端的公聊消息包。
	message := protocol.BuildPublicBroadcast(peer.username, cmd.Content)
	// 取在线用户快照，避免边持锁边写网络。
	for _, onlinePeer := range s.snapshotOnlinePeers() {
		// 给每个在线用户发送消息，包括发送者自己。
		_ = s.sendPacket(onlinePeer, message)
	}
}

// handlePrivateEnter 处理“进入私聊”请求。
// 服务端只负责判断能不能进入，真正切换私聊状态由客户端完成。
func (s *chatServer) handlePrivateEnter(peer *clientConn, cmd protocol.Packet) {
	// 私聊目标用户名也必须符合用户名规则。
	if err := protocol.ValidateUsername(cmd.Target); err != nil {
		_ = s.sendPacket(peer, protocol.BuildPrivateEnterErr(protocol.CodeInvalidUsername))
		return
	}

	// 取当前在线用户集合，用于判断目标是否在线。
	online := s.snapshotOnlineUserSet()
	// 按业务规则判断是否允许进入私聊。
	if code := canEnterPrivateMode(peer.username, cmd.Target, online); code != "" {
		// 失败时返回具体错误码。
		_ = s.sendPacket(peer, protocol.BuildPrivateEnterErr(code))
		return
	}

	// 成功时返回目标用户名，让客户端切换私聊模式。
	_ = s.sendPacket(peer, protocol.BuildPrivateEnterOK(cmd.Target))
}

// handlePrivateMessage 处理私聊消息。
// 私聊消息只发给目标用户和发送者自己，不会广播给其他人。
func (s *chatServer) handlePrivateMessage(peer *clientConn, cmd protocol.Packet) {
	// 校验目标用户名。
	if err := protocol.ValidateUsername(cmd.Target); err != nil {
		_ = s.sendPacket(peer, protocol.BuildSystemErr("私聊对象无效"))
		return
	}
	// 校验消息内容不能为空。
	if err := protocol.ValidateMessage(cmd.Content); err != nil {
		_ = s.sendPacket(peer, protocol.BuildSystemErr(err.Error()))
		return
	}

	// 查在线用户表前加读锁。
	s.mu.RLock()
	// 根据目标用户名找目标连接。
	targetPeer, ok := s.peersByName[cmd.Target]
	// 查完立即释放读锁，避免后续网络写长时间占锁。
	s.mu.RUnlock()
	// 如果目标不在线，提示发送者退出私聊。
	if !ok {
		_ = s.sendPacket(peer, protocol.BuildSystemErr("私聊对象已离线，请输入 /exit 退出私聊"))
		return
	}
	// 保险校验：不允许给自己发私聊。
	if targetPeer.username == peer.username {
		_ = s.sendPacket(peer, protocol.BuildSystemErr("不能给自己发送私聊"))
		return
	}

	// 保存私聊消息到数据库。
	if err := mysql.SavePrivateMessage(peer.username, targetPeer.username, cmd.Content); err != nil {
		// 服务端记录详细错误。
		fmt.Println("保存私聊消息失败:", err)
		// 客户端只收到统一提示。
		_ = s.sendPacket(peer, protocol.BuildSystemErr("保存消息失败"))
		return
	}

	// 给目标用户发送真正的私聊消息。
	_ = s.sendPacket(targetPeer, protocol.BuildPrivateInbound(peer.username, cmd.Content))
	// 给发送者发送回执，证明消息已经发出。
	_ = s.sendPacket(peer, protocol.BuildPrivateAck(targetPeer.username, cmd.Content))
}

// handleUserList 处理 /list。
// 它会返回当前在线用户列表。
func (s *chatServer) handleUserList(peer *clientConn) {
	// 获取已排序的在线用户名。
	names := s.snapshotOnlineUsernames()
	// 构造成协议包后发给请求者。
	_ = s.sendPacket(peer, protocol.BuildUserList(names))
}

// addPeer 把新连接加入连接表。
// 注意：此时用户可能还没有登录，所以只加入 peersByConn，不加入 peersByName。
func (s *chatServer) addPeer(peer *clientConn) {
	// 修改连接表前加写锁。
	s.mu.Lock()
	// 函数返回时释放锁。
	defer s.mu.Unlock()
	// 按连接对象保存客户端对象。
	s.peersByConn[peer.conn] = peer
}

// disconnectPeer 清理一个连接。
// 无论客户端正常退出、异常断开、服务端关服，最终都会走这里。
func (s *chatServer) disconnectPeer(peer *clientConn) {
	// 修改在线表和连接表前加写锁。
	s.mu.Lock()
	// 如果这个连接已经登录，需要从用户名表删除。
	if peer.username != "" {
		delete(s.peersByName, peer.username)
	}
	// 从连接表删除。
	delete(s.peersByConn, peer.conn)
	// 删除完成后释放锁。
	s.mu.Unlock()

	// 最后关闭底层连接。
	_ = peer.conn.Close()
}

// sendPacket 是服务端唯一的发包入口。
// 所有写客户端连接的地方都必须通过这个函数。
func (s *chatServer) sendPacket(peer *clientConn, payload string) error {
	// 对单个连接加写锁，防止多个 goroutine 同时写同一个 TCP 连接。
	peer.writeMu.Lock()
	// 发送完成后释放连接写锁。
	defer peer.writeMu.Unlock()
	// 使用 pre.WritePacket 发送带长度头的完整包。
	return pre.WritePacket(peer.conn, []byte(payload))
}

// snapshotOnlinePeers 返回当前在线用户连接快照。
// 快照的目的，是避免边持锁边做网络发送。
func (s *chatServer) snapshotOnlinePeers() []*clientConn {
	// 读取在线表前加读锁。
	s.mu.RLock()
	// 函数返回前释放读锁。
	defer s.mu.RUnlock()

	// 提前分配切片容量，减少扩容。
	peers := make([]*clientConn, 0, len(s.peersByName))
	// 遍历当前在线用户表。
	for _, peer := range s.peersByName {
		// 把每个在线连接放进快照。
		peers = append(peers, peer)
	}
	// 返回快照。
	return peers
}

// snapshotOnlineUsernames 返回当前在线用户名快照。
// 返回前会排序，保证 /list 显示稳定。
func (s *chatServer) snapshotOnlineUsernames() []string {
	// 读取在线表前加读锁。
	s.mu.RLock()
	// 函数返回前释放读锁。
	defer s.mu.RUnlock()

	// 提前分配用户名切片。
	names := make([]string, 0, len(s.peersByName))
	// 遍历在线用户名。
	for name := range s.peersByName {
		// 保存用户名。
		names = append(names, name)
	}
	// 排序后输出更稳定。
	sort.Strings(names)
	// 返回用户名列表。
	return names
}

// snapshotOnlineUserSet 返回当前在线用户名集合。
// 集合适合快速判断某个用户是否在线。
func (s *chatServer) snapshotOnlineUserSet() map[string]struct{} {
	// 读取在线表前加读锁。
	s.mu.RLock()
	// 函数返回前释放读锁。
	defer s.mu.RUnlock()

	// 初始化集合，struct{} 不占额外值空间。
	users := make(map[string]struct{}, len(s.peersByName))
	// 遍历在线用户名。
	for name := range s.peersByName {
		// 把用户名放入集合。
		users[name] = struct{}{}
	}
	// 返回在线用户集合。
	return users
}

// watchConsoleExit 持续读取服务端控制台输入。
// 管理员在服务端输入 /exit 时会触发关服。
func (s *chatServer) watchConsoleExit() {
	// 创建控制台输入读取器。
	reader := bufio.NewReader(os.Stdin)
	// 持续读取控制台命令。
	for {
		// 按行读取控制台输入。
		line, err := reader.ReadString('\n')
		// 读取失败时结束控制台监听。
		if err != nil {
			// 如果不是关服导致的读取结束，就打印错误。
			if !s.isShuttingDown() {
				fmt.Println("读取控制台命令失败:", err)
			}
			return
		}
		// 去掉换行后判断是否是 /exit。
		if isExitCommand(strings.TrimSpace(line)) {
			// 触发服务端优雅关闭。
			s.shutdownServer("服务器已关闭")
			return
		}
	}
}

// shutdownServer 执行服务端优雅关闭。
// 关闭顺序：发通知 -> 关客户端连接 -> 关 listener。
func (s *chatServer) shutdownServer(message string) {
	// closeOnce 保证并发调用时只执行一次。
	s.closeOnce.Do(func() {
		// 关闭 shutdownCh，通知其他代码服务端正在停机。
		close(s.shutdownCh)

		// 取所有连接快照，包括未登录连接。
		for _, peer := range s.snapshotAllPeers() {
			// 先发送关服通知，让客户端能打印“服务器已关闭”。
			_ = s.sendPacket(peer, protocol.BuildShutdown(message))
			// 再关闭客户端连接。
			_ = peer.conn.Close()
		}

		// 关闭监听器，让 Accept 停止阻塞。
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			// 非正常关闭错误才打印。
			fmt.Println("关闭监听器失败:", err)
		}
	})
}

// snapshotAllPeers 返回所有连接快照。
// 与 snapshotOnlinePeers 不同，它包含未登录连接。
func (s *chatServer) snapshotAllPeers() []*clientConn {
	// 读取连接表前加读锁。
	s.mu.RLock()
	// 函数返回前释放读锁。
	defer s.mu.RUnlock()

	// 提前分配切片容量。
	peers := make([]*clientConn, 0, len(s.peersByConn))
	// 遍历所有连接。
	for _, peer := range s.peersByConn {
		// 保存到快照。
		peers = append(peers, peer)
	}
	// 返回所有连接。
	return peers
}

// isShuttingDown 判断服务端是否正在关闭。
// 用它可以避免把主动关服产生的网络错误误判成异常。
func (s *chatServer) isShuttingDown() bool {
	// select default 是非阻塞检查 channel 是否关闭的常见写法。
	select {
	case <-s.shutdownCh:
		// 能读到说明 shutdownCh 已经关闭。
		return true
	default:
		// default 分支说明还没有关服。
		return false
	}
}

// mapLoginResultToCode 把数据库层登录结果转换成协议错误码。
// 这样 server 不会把 mysql.LoginResult 直接暴露给客户端。
func mapLoginResultToCode(result mysql.LoginResult) string {
	// 根据数据库层结果逐项映射。
	switch result {
	case mysql.LoginUserNotFound:
		return protocol.CodeUserNotFound
	case mysql.LoginPasswordIncorrect:
		return protocol.CodePasswordIncorrect
	default:
		return protocol.CodeDBError
	}
}

// canEnterPrivateMode 判断是否允许 sender 进入 target 的私聊。
// 这个函数不依赖网络和数据库，所以适合单元测试。
func canEnterPrivateMode(sender, target string, online map[string]struct{}) string {
	// 不能和自己进入私聊。
	if sender == target {
		return protocol.CodeTargetSelf
	}
	// 目标不在在线集合中，说明目标不在线或不存在。
	if _, ok := online[target]; !ok {
		return protocol.CodeTargetNotFound
	}
	// 空字符串表示允许进入私聊。
	return ""
}

// formatUserList 把用户名排序后拼成逗号分隔字符串。
// 这个函数主要用于测试稳定输出。
func formatUserList(users []string) string {
	// 复制一份，避免修改调用方传入的切片。
	result := append([]string(nil), users...)
	// 排序保证输出稳定。
	sort.Strings(result)
	// 用逗号拼接成协议里的用户列表格式。
	return strings.Join(result, ",")
}

// isExitCommand 判断服务端控制台命令是否为退出命令。
func isExitCommand(input string) bool {
	// 只接受完全等于 /exit 的输入。
	return input == "/exit"
}
