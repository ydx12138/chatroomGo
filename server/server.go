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

// clientConn 记录一个客户端连接，以及这个连接登录后的用户名。
// writeMu 用来保证同一个连接一次只写一条消息，避免多条消息混在一起。
type clientConn struct {
	conn     net.Conn
	username string
	writeMu  sync.Mutex
}

// chatServer 只保存服务端运行时需要共用的数据。
// peersByConn 保存所有连接，peersByName 只保存已经登录成功的用户。
type chatServer struct {
	listener    net.Listener
	peersByConn map[net.Conn]*clientConn
	peersByName map[string]*clientConn
	mu          sync.RWMutex
	shutdownCh  chan struct{}
	shutdownWg  sync.WaitGroup
	closeOnce   sync.Once
}

// main 先准备数据库和端口监听，然后开始接收客户端连接。
func main() {
	if err := mysql.InitMysql(); err != nil {
		fmt.Println("初始化数据库失败:", err)
		return
	}
	defer func() {
		if err := mysql.CloseMysql(); err != nil {
			fmt.Println("关闭数据库失败:", err)
		}
	}()

	listener, err := net.Listen("tcp", "0.0.0.0:8888")
	if err != nil {
		fmt.Println("启动服务端失败:", err)
		return
	}

	server := newChatServer(listener)
	fmt.Println("服务端已启动: 0.0.0.0:8888")
	go watchConsoleExit(server)

	if err := serve(server); err != nil && !isShuttingDown(server) {
		fmt.Println("服务端异常退出:", err)
	}
	server.shutdownWg.Wait()
}

// newChatServer 创建服务端要用的连接表、用户表和关服信号。
func newChatServer(listener net.Listener) *chatServer {
	return &chatServer{
		listener:    listener,
		peersByConn: make(map[net.Conn]*clientConn),
		peersByName: make(map[string]*clientConn),
		shutdownCh:  make(chan struct{}),
	}
}

// serve 一直接收新客户端；每来一个客户端，就单独开一个 goroutine 处理它。
// 关服时 listener 会被关闭，Accept 返回的错误这时是正常现象，不需要当成程序出错。
func serve(server *chatServer) error {
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			if isShuttingDown(server) {
				return nil
			}
			return err
		}

		peer := &clientConn{conn: conn}
		addPeer(server, peer)
		server.shutdownWg.Add(1)
		go func() {
			defer server.shutdownWg.Done()
			handleConnection(server, peer)
		}()
	}
}

// handleConnection 负责一个客户端连接的完整过程：读消息、判断命令、处理退出。
// 用户登录前只能发注册或登录命令；登录后才能发聊天、私聊、列表等命令。
func handleConnection(server *chatServer, peer *clientConn) {
	defer disconnectPeer(server, peer)

	for {
		raw, err := pre.ReadPacket(peer.conn)
		if err != nil {
			if !isShuttingDown(server) && !errors.Is(err, net.ErrClosed) {
				fmt.Printf("连接断开: %v, 用户: %s, 错误: %v\n", peer.conn.RemoteAddr(), peer.username, err)
			}
			return
		}

		cmd, err := protocol.ParseClientPacket(raw)
		if err != nil {
			_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "无效命令"))
			continue
		}

		if peer.username == "" {
			if shouldClose := handleGuestCommand(server, peer, cmd); shouldClose {
				return
			}
			continue
		}

		if shouldClose := handleAuthedCommand(server, peer, cmd); shouldClose {
			return
		}
	}
}

// handleGuestCommand 处理还没登录的客户端命令。
func handleGuestCommand(server *chatServer, peer *clientConn, cmd protocol.Packet) bool {
	switch cmd.Cmd {
	case protocol.CmdRegister:
		handleRegister(peer, cmd)
	case protocol.CmdLogin:
		if handleLogin(server, peer, cmd) {
			fmt.Printf("用户登录成功: %s\n", peer.username)
		}
	default:
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "请先登录或注册"))
	}
	return false
}

// handleAuthedCommand 处理已经登录的用户发来的聊天命令。
func handleAuthedCommand(server *chatServer, peer *clientConn, cmd protocol.Packet) bool {
	switch {
	case cmd.Cmd == protocol.CmdPublic:
		handlePublicMessage(server, peer, cmd)
	case cmd.Cmd == protocol.CmdPrivateEnter:
		handlePrivateEnter(server, peer, cmd)
	case cmd.Cmd == protocol.CmdPrivate:
		handlePrivateMessage(server, peer, cmd)
	case cmd.Cmd == protocol.CmdList:
		handleUserList(server, peer)
	case cmd.Cmd == protocol.CmdQuit:
		return true
	default:
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "无效命令"))
	}
	return false
}

// handleRegister 检查用户名和密码是否合规，然后写入数据库。
// 注册成功只告诉客户端 OK，不会顺便把这个连接变成已登录状态。
func handleRegister(peer *clientConn, cmd protocol.Packet) {
	if err := protocol.ValidateUsername(cmd.Username); err != nil {
		_ = sendErr(peer, "INVALID_USERNAME")
		return
	}
	if err := protocol.ValidatePassword(cmd.Password); err != nil {
		_ = sendErr(peer, "INVALID_PASSWORD")
		return
	}

	switch err := mysql.RegisterUser(cmd.Username, cmd.Password); {
	case err == nil:
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdOK, protocol.CmdRegister))
	case errors.Is(err, mysql.ErrNameExists):
		_ = sendErr(peer, "NAME_EXISTS")
	default:
		fmt.Println("注册失败:", err)
		_ = sendErr(peer, "DB_ERROR")
	}
}

// handleLogin 检查账号密码；成功后把这个连接标记为在线用户。
func handleLogin(server *chatServer, peer *clientConn, cmd protocol.Packet) bool {
	if err := protocol.ValidateUsername(cmd.Username); err != nil {
		_ = sendErr(peer, "INVALID_USERNAME")
		return false
	}
	if err := protocol.ValidatePassword(cmd.Password); err != nil {
		_ = sendErr(peer, "INVALID_PASSWORD")
		return false
	}

	result, err := mysql.CheckLogin(cmd.Username, cmd.Password)
	if err != nil {
		fmt.Println("登录校验失败:", err)
		_ = sendErr(peer, "DB_ERROR")
		return false
	}
	if result != mysql.LoginSuccess {
		_ = sendErr(peer, mapLoginResultToCode(result))
		return false
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if _, exists := server.peersByName[cmd.Username]; exists {
		_ = sendErr(peer, "ALREADY_ONLINE")
		return false
	}
	peer.username = cmd.Username
	server.peersByName[cmd.Username] = peer
	_ = sendPacket(peer, protocol.MakePacket(protocol.CmdOK, protocol.CmdLogin))
	return true
}

// handlePublicMessage 先保存公聊消息，再发给所有在线用户。
// 发送前先复制一份当前在线连接列表，后面写网络时就不用一直占着锁。
func handlePublicMessage(server *chatServer, peer *clientConn, cmd protocol.Packet) {
	if err := protocol.ValidateMessage(cmd.Content); err != nil {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, err.Error()))
		return
	}
	if err := mysql.SavePublicMessage(peer.username, cmd.Content); err != nil {
		fmt.Println("保存公聊消息失败:", err)
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "保存消息失败"))
		return
	}

	message := protocol.MakePacket(protocol.CmdPublic, peer.username, cmd.Content)
	for _, onlinePeer := range snapshotOnlinePeers(server) {
		_ = sendPacket(onlinePeer, message)
	}
}

// handlePrivateEnter 只检查对方能不能私聊，比如是否在线、是不是自己。
// 客户端收到 ENTEROK 后，才会在本地切到私聊输入模式。
func handlePrivateEnter(server *chatServer, peer *clientConn, cmd protocol.Packet) {
	if err := protocol.ValidateUsername(cmd.Target); err != nil {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdPrivateEnterErr, "INVALID_USERNAME"))
		return
	}

	online := snapshotOnlineUserSet(server)
	if code := canEnterPrivateMode(peer.username, cmd.Target, online); code != "" {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdPrivateEnterErr, code))
		return
	}

	_ = sendPacket(peer, protocol.MakePacket(protocol.CmdPrivateEnterOK, cmd.Target))
}

// handlePrivateMessage 保存私聊消息，然后分别发给对方和自己。
func handlePrivateMessage(server *chatServer, peer *clientConn, cmd protocol.Packet) {
	if err := protocol.ValidateUsername(cmd.Target); err != nil {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "私聊对象无效"))
		return
	}
	if err := protocol.ValidateMessage(cmd.Content); err != nil {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, err.Error()))
		return
	}

	server.mu.RLock()
	targetPeer, ok := server.peersByName[cmd.Target]
	server.mu.RUnlock()
	if !ok {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "私聊对象已离线，请输入 /exit 退出私聊"))
		return
	}
	if targetPeer.username == peer.username {
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "不能给自己发送私聊"))
		return
	}

	if err := mysql.SavePrivateMessage(peer.username, targetPeer.username, cmd.Content); err != nil {
		fmt.Println("保存私聊消息失败:", err)
		_ = sendPacket(peer, protocol.MakePacket(protocol.CmdSystem, "保存消息失败"))
		return
	}

	_ = sendPacket(targetPeer, protocol.MakePacket(protocol.CmdPrivate, peer.username, cmd.Content))
	_ = sendPacket(peer, protocol.MakePacket(protocol.CmdPrivateAck, targetPeer.username, cmd.Content))
}

// handleUserList 把当前在线用户名发给客户端。
// 用户名会先排序，这样每次看到的顺序更稳定。
func handleUserList(server *chatServer, peer *clientConn) {
	names := snapshotOnlineUsernames(server)
	_ = sendPacket(peer, protocol.MakePacket(protocol.CmdList, strings.Join(names, ",")))
}

// addPeer 把新连接记下来。
// 没登录的连接也要记录，关服时才能一起通知并关闭。
func addPeer(server *chatServer, peer *clientConn) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.peersByConn[peer.conn] = peer
}

// disconnectPeer 清理断开的客户端：从连接表、在线表里删掉，并关闭 TCP 连接。
func disconnectPeer(server *chatServer, peer *clientConn) {
	server.mu.Lock()
	if peer.username != "" {
		delete(server.peersByName, peer.username)
	}
	delete(server.peersByConn, peer.conn)
	server.mu.Unlock()

	_ = peer.conn.Close()
}

// sendPacket 给某个客户端发送一条完整消息。
func sendPacket(peer *clientConn, payload string) error {
	peer.writeMu.Lock()
	defer peer.writeMu.Unlock()
	return pre.WritePacket(peer.conn, []byte(payload))
}

// sendErr 给客户端发送一条 ERR 消息，内容是具体错误码。
func sendErr(peer *clientConn, code string) error {
	return sendPacket(peer, protocol.MakePacket(protocol.CmdErr, code))
}

// snapshotOnlinePeers 复制一份当前在线连接列表。
// 这样调用方后面群发消息时，不需要一直锁住用户表。
func snapshotOnlinePeers(server *chatServer) []*clientConn {
	server.mu.RLock()
	defer server.mu.RUnlock()

	peers := make([]*clientConn, 0, len(server.peersByName))
	for _, peer := range server.peersByName {
		peers = append(peers, peer)
	}
	return peers
}

// snapshotOnlineUsernames 复制当前在线用户名，并按字母顺序排好。
func snapshotOnlineUsernames(server *chatServer) []string {
	server.mu.RLock()
	defer server.mu.RUnlock()

	names := make([]string, 0, len(server.peersByName))
	for name := range server.peersByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// snapshotOnlineUserSet 把在线用户名整理成集合，方便快速判断某个人是否在线。
func snapshotOnlineUserSet(server *chatServer) map[string]struct{} {
	server.mu.RLock()
	defer server.mu.RUnlock()

	users := make(map[string]struct{}, len(server.peersByName))
	for name := range server.peersByName {
		users[name] = struct{}{}
	}
	return users
}

// watchConsoleExit 等待服务端控制台输入 /exit，用来手动关服。
func watchConsoleExit(server *chatServer) {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !isShuttingDown(server) {
				fmt.Println("读取控制台命令失败:", err)
			}
			return
		}
		if strings.TrimSpace(line) == "/exit" {
			shutdownServer(server, "服务器已关闭")
			return
		}
	}
}

// shutdownServer 只执行一次关服：通知所有客户端，然后关闭连接和监听端口。
func shutdownServer(server *chatServer, message string) {
	server.closeOnce.Do(func() {
		close(server.shutdownCh)

		for _, peer := range snapshotAllPeers(server) {
			_ = sendPacket(peer, protocol.MakePacket(protocol.CmdShutdown, message))
			_ = peer.conn.Close()
		}

		if err := server.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			fmt.Println("关闭监听器失败:", err)
		}
	})
}

// snapshotAllPeers 复制一份所有客户端连接，包括还没登录的连接。
func snapshotAllPeers(server *chatServer) []*clientConn {
	server.mu.RLock()
	defer server.mu.RUnlock()

	peers := make([]*clientConn, 0, len(server.peersByConn))
	for _, peer := range server.peersByConn {
		peers = append(peers, peer)
	}
	return peers
}

// isShuttingDown 看服务端是不是已经开始关服；这个检查不会卡住当前 goroutine。
func isShuttingDown(server *chatServer) bool {
	select {
	case <-server.shutdownCh:
		return true
	default:
		return false
	}
}

// mapLoginResultToCode 把数据库返回的登录结果，转成客户端能看懂的错误码。
func mapLoginResultToCode(result mysql.LoginResult) string {
	switch result {
	case mysql.LoginUserNotFound:
		return "USER_NOT_FOUND"
	case mysql.LoginPasswordIncorrect:
		return "PASSWORD_INCORRECT"
	default:
		return "DB_ERROR"
	}
}

// canEnterPrivateMode 检查能不能进入私聊；返回空字符串表示可以进入。
func canEnterPrivateMode(sender, target string, online map[string]struct{}) string {
	if sender == target {
		return "TARGET_SELF"
	}
	if _, ok := online[target]; !ok {
		return "TARGET_NOT_FOUND"
	}
	return ""
}
