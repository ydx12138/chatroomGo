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

// clientConn 是服务端保存的单个客户端连接状态。
// writeMu 保证广播、私聊回执、关服通知等并发写不会交错写入同一条 TCP 连接。
type clientConn struct {
	conn     net.Conn
	username string
	writeMu  sync.Mutex
}

// chatServer 集中管理监听器、连接表、在线用户表和关服流程。
// peersByConn 包含未登录连接，peersByName 只包含已登录用户。
type chatServer struct {
	listener    net.Listener
	peersByConn map[net.Conn]*clientConn
	peersByName map[string]*clientConn
	mu          sync.RWMutex
	shutdownCh  chan struct{}
	shutdownWg  sync.WaitGroup
	closeOnce   sync.Once
}

// main 初始化数据库和 TCP 监听，然后启动服务端主循环。
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

// newChatServer 初始化服务端运行时状态。
func newChatServer(listener net.Listener) *chatServer {
	return &chatServer{
		listener:    listener,
		peersByConn: make(map[net.Conn]*clientConn),
		peersByName: make(map[string]*clientConn),
		shutdownCh:  make(chan struct{}),
	}
}

// serve 接收新连接并为每条连接启动独立处理协程。
// listener 关闭时 Accept 会返回错误，此时需要和异常退出区分开。
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

// handleConnection 处理一个连接从接入、认证到聊天或断开的完整生命周期。
// 登录前只接受认证命令，登录后才接受聊天相关命令。
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

// handleGuestCommand 路由未登录连接发来的命令。
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

// handleAuthedCommand 路由已登录用户的聊天命令。
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

// handleRegister 校验并注册用户。
// 注册成功只返回 OK，不会把当前连接切换为登录态。
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

// handleLogin 校验账号密码，并把登录成功的连接加入在线用户名表。
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

// handlePublicMessage 保存公聊消息后广播给所有在线用户。
// 广播前先取在线连接快照，避免持有全局锁时写网络。
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

// handlePrivateEnter 只判断目标用户是否可私聊。
// 真正进入私聊模式由客户端收到 ENTEROK 后本地切换。
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

// handlePrivateMessage 保存私聊消息，并发送给目标用户和发送者自己。
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

// handleUserList 返回当前在线用户名列表。
// 用户名排序在 snapshotOnlineUsernames 中完成，保证输出稳定。
func handleUserList(server *chatServer, peer *clientConn) {
	names := snapshotOnlineUsernames(server)
	_ = sendPacket(peer, protocol.MakePacket(protocol.CmdList, strings.Join(names, ",")))
}

// addPeer 把新连接放入连接表。
// 即使未登录连接也要登记，关服时才能统一通知和关闭。
func addPeer(server *chatServer, peer *clientConn) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.peersByConn[peer.conn] = peer
}

// disconnectPeer 从连接表和在线表中移除连接，并关闭底层 TCP 连接。
func disconnectPeer(server *chatServer, peer *clientConn) {
	server.mu.Lock()
	if peer.username != "" {
		delete(server.peersByName, peer.username)
	}
	delete(server.peersByConn, peer.conn)
	server.mu.Unlock()

	_ = peer.conn.Close()
}

// sendPacket 是服务端写客户端连接的唯一入口。
func sendPacket(peer *clientConn, payload string) error {
	peer.writeMu.Lock()
	defer peer.writeMu.Unlock()
	return pre.WritePacket(peer.conn, []byte(payload))
}

// sendErr 发送认证阶段的错误码响应。
func sendErr(peer *clientConn, code string) error {
	return sendPacket(peer, protocol.MakePacket(protocol.CmdErr, code))
}

// snapshotOnlinePeers 返回在线连接快照。
// 调用方可以在不持锁的情况下执行网络写入。
func snapshotOnlinePeers(server *chatServer) []*clientConn {
	server.mu.RLock()
	defer server.mu.RUnlock()

	peers := make([]*clientConn, 0, len(server.peersByName))
	for _, peer := range server.peersByName {
		peers = append(peers, peer)
	}
	return peers
}

// snapshotOnlineUsernames 返回排序后的在线用户名快照。
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

// snapshotOnlineUserSet 返回在线用户名集合，供私聊进入校验使用。
func snapshotOnlineUserSet(server *chatServer) map[string]struct{} {
	server.mu.RLock()
	defer server.mu.RUnlock()

	users := make(map[string]struct{}, len(server.peersByName))
	for name := range server.peersByName {
		users[name] = struct{}{}
	}
	return users
}

// watchConsoleExit 监听服务端控制台 /exit 命令。
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

// shutdownServer 执行一次性关服流程：通知客户端、关闭连接、关闭 listener。
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

// snapshotAllPeers 返回所有连接快照，包含尚未登录的连接。
func snapshotAllPeers(server *chatServer) []*clientConn {
	server.mu.RLock()
	defer server.mu.RUnlock()

	peers := make([]*clientConn, 0, len(server.peersByConn))
	for _, peer := range server.peersByConn {
		peers = append(peers, peer)
	}
	return peers
}

// isShuttingDown 非阻塞判断服务端是否已经进入关服流程。
func isShuttingDown(server *chatServer) bool {
	select {
	case <-server.shutdownCh:
		return true
	default:
		return false
	}
}

// mapLoginResultToCode 把数据库层登录结果映射为客户端协议错误码。
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

// canEnterPrivateMode 校验私聊目标，返回空字符串表示允许进入。
func canEnterPrivateMode(sender, target string, online map[string]struct{}) string {
	if sender == target {
		return "TARGET_SELF"
	}
	if _, ok := online[target]; !ok {
		return "TARGET_NOT_FOUND"
	}
	return ""
}
