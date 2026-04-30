package main

import (
	"DEMO2/TCPIP/pre"
	"DEMO2/TCPIP/protocol"
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

const defaultServerAddr = "localhost:8888"

// errClientExit 表示用户在主菜单主动退出，不属于异常。
var errClientExit = errors.New("client exit")

// clientSession 只保存客户端本地聊天状态。
// 服务端仍然是登录态、在线列表和消息落库的权威来源。
type clientSession struct {
	username       string
	privateTarget  string
	waitingPrivate bool
}

// privateEnterResult 把接收协程里的私聊进入响应交回主循环处理。
// 这样只有主循环会修改 clientSession，避免多个 goroutine 同时改状态。
type privateEnterResult struct {
	ok     bool
	target string
	code   string
}

var authErrors = map[string]string{
	"NAME_EXISTS":        "用户名已存在",
	"USER_NOT_FOUND":     "用户不存在",
	"PASSWORD_INCORRECT": "密码错误",
	"ALREADY_ONLINE":     "该用户已经在线",
	"INVALID_USERNAME":   "用户名格式不正确",
	"INVALID_PASSWORD":   "密码格式不正确",
	"DB_ERROR":           "数据库操作失败",
}

var privateEnterErrors = map[string]string{
	"TARGET_NOT_FOUND": "目标用户不在线或不存在",
	"TARGET_SELF":      "不能和自己进入私聊",
	"INVALID_USERNAME": "私聊用户名格式不正确",
}

// main 先处理登录/注册菜单，登录成功后进入聊天循环。
func main() {
	reader := bufio.NewReader(os.Stdin)
	username, conn, err := runMainMenu(reader)
	if err != nil {
		if !errors.Is(err, errClientExit) {
			fmt.Println("[系统] 客户端异常退出:", err)
		}
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	session := &clientSession{
		username: username,
	}
	runChat(conn, reader, session)
}

// serverAddr 返回客户端默认连接的服务端地址。
func serverAddr() string {
	return defaultServerAddr
}

// runMainMenu 循环处理登录前菜单。
// 登录成功时返回仍然保持打开的 TCP 连接，注册成功则回到菜单不自动登录。
func runMainMenu(reader *bufio.Reader) (string, net.Conn, error) {
	for {
		printMainMenu()
		choice, err := readLine(reader, "请选择功能: ")
		if err != nil {
			return "", nil, err
		}

		switch strings.TrimSpace(choice) {
		case "1":
			username, conn, err := doAuthFlow(reader, protocol.CmdLogin)
			if err == nil && username != "" {
				return username, conn, nil
			}
			if err != nil {
				fmt.Println("[系统]", err)
			}
		case "2":
			_, conn, err := doAuthFlow(reader, protocol.CmdRegister)
			if err != nil {
				fmt.Println("[系统]", err)
				continue
			}
			if conn != nil {
				_ = conn.Close()
			}
		case "3":
			fmt.Println("[系统] 客户端已退出")
			return "", nil, errClientExit
		default:
			fmt.Println("[系统] 无效选项，请重新输入")
		}
	}
}

// doAuthFlow 执行一次登录或注册请求。
// 认证失败会在这里关闭连接并返回主菜单；登录成功的连接交给聊天循环继续使用。
func doAuthFlow(reader *bufio.Reader, action string) (string, net.Conn, error) {
	username, password, err := promptCredentials(reader)
	if err != nil {
		return "", nil, err
	}

	conn, err := net.Dial("tcp", serverAddr())
	if err != nil {
		return "", nil, fmt.Errorf("连接服务端失败: %w", err)
	}

	if err := sendPayload(conn, protocol.MakePacket(action, username, password)); err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("发送认证请求失败: %w", err)
	}

	raw, err := pre.ReadPacket(conn)
	if err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("读取认证响应失败: %w", err)
	}
	packet, err := protocol.ParseServerPacket(raw)
	if err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("解析认证响应失败: %w", err)
	}
	if packet.Cmd == protocol.CmdErr {
		fmt.Println("[系统]", translateAuthError(packet.Code))
		_ = conn.Close()
		return "", nil, nil
	}

	if packet.Cmd != protocol.CmdOK {
		_ = conn.Close()
		return "", nil, errors.New("收到未知认证响应")
	}

	switch action {
	case protocol.CmdRegister:
		fmt.Println("[系统] 注册成功，请返回主菜单登录")
		return "", conn, nil
	case protocol.CmdLogin:
		fmt.Printf("[系统] 登录成功，欢迎 %s\n", username)
		printChatGuide()
		return username, conn, nil
	default:
		_ = conn.Close()
		return "", nil, errors.New("未知认证动作")
	}
}

// promptCredentials 先做客户端本地格式校验，让用户更快看到输入错误。
// 服务端仍会再次校验，不能依赖客户端校验保证安全。
func promptCredentials(reader *bufio.Reader) (string, string, error) {
	username, err := readLine(reader, "请输入用户名: ")
	if err != nil {
		return "", "", err
	}
	if err := protocol.ValidateUsername(username); err != nil {
		return "", "", err
	}

	password, err := readLine(reader, "请输入密码: ")
	if err != nil {
		return "", "", err
	}
	if err := protocol.ValidatePassword(password); err != nil {
		return "", "", err
	}

	return username, password, nil
}

// runChat 同时监听键盘输入和服务端推送。
// 输入解析、私聊状态变更都留在主循环里完成，接收协程只负责传递事件。
func runChat(conn net.Conn, reader *bufio.Reader, session *clientSession) {
	inputCh := make(chan string)
	privateEnterCh := make(chan privateEnterResult, 1)
	doneCh := make(chan struct{})

	go readChatInput(reader, inputCh)
	go receiveLoop(conn, privateEnterCh, doneCh)

	for {
		select {
		case line, ok := <-inputCh:
			if !ok {
				return
			}
			shouldExit, err := handleChatInput(conn, session, line)
			if err != nil {
				fmt.Println("[系统]", err)
				continue
			}
			if shouldExit {
				fmt.Println("[系统] 客户端已退出")
				return
			}
		case result := <-privateEnterCh:
			fmt.Println("[系统]", applyPrivateEnterResult(session, result))
		case <-doneCh:
			return
		}
	}
}

// handleChatInput 把一行用户输入转成协议包或本地状态变化。
// privateTarget 为空表示公聊；非空表示普通文本会发给当前私聊对象。
func handleChatInput(conn net.Conn, session *clientSession, line string) (bool, error) {
	if session.waitingPrivate {
		return false, errors.New("正在等待私聊确认，请稍候")
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return false, errors.New("输入不能为空")
	}

	inPrivate := session.privateTarget != ""
	switch {
	case line == "/list":
		return false, sendPayload(conn, protocol.CmdList)
	case line == "/exit" && inPrivate:
		session.privateTarget = ""
		session.waitingPrivate = false
		fmt.Println("[系统] 已退出私聊，回到公聊")
		return false, nil
	case line == "/exit":
		_ = sendPayload(conn, protocol.CmdQuit)
		return true, nil
	case strings.HasPrefix(line, "/chat"):
		if inPrivate {
			return false, errors.New("私聊状态下不能再次进入私聊")
		}
		target := strings.TrimSpace(strings.TrimPrefix(line, "/chat"))
		if err := protocol.ValidateUsername(target); err != nil {
			return false, err
		}
		session.waitingPrivate = true
		return false, sendPayload(conn, protocol.MakePacket(protocol.CmdPrivateEnter, target))
	case strings.HasPrefix(line, "/"):
		return false, errors.New("未知命令")
	}

	if err := protocol.ValidateMessage(line); err != nil {
		return false, err
	}
	if inPrivate {
		return false, sendPayload(conn, protocol.MakePacket(protocol.CmdPrivate, session.privateTarget, line))
	}
	return false, sendPayload(conn, protocol.MakePacket(protocol.CmdPublic, line))
}

// readChatInput 只负责把标准输入转成行事件。
// 解析和网络发送放在主循环，避免输入协程直接修改会话状态。
func readChatInput(reader *bufio.Reader, inputCh chan<- string) {
	for {
		line, err := readLine(reader, "")
		if err != nil {
			close(inputCh)
			return
		}
		inputCh <- line
	}
}

// receiveLoop 持续读取服务端包。
// 私聊进入结果通过 privateEnterCh 交给主循环，其他消息直接渲染输出。
func receiveLoop(conn net.Conn, privateEnterCh chan<- privateEnterResult, doneCh chan<- struct{}) {
	defer close(doneCh)

	for {
		raw, err := pre.ReadPacket(conn)
		if err != nil {
			fmt.Println("[系统] 与服务端的连接已关闭")
			return
		}

		packet, err := protocol.ParseServerPacket(raw)
		if err != nil {
			fmt.Println("[系统] 收到无法识别的服务端消息")
			continue
		}

		switch {
		case packet.Cmd == protocol.CmdPrivateEnterOK:
			privateEnterCh <- privateEnterResult{ok: true, target: packet.Target}
		case packet.Cmd == protocol.CmdPrivateEnterErr:
			privateEnterCh <- privateEnterResult{ok: false, code: packet.Code}
		default:
			line, shouldExit := renderServerPacket(packet)
			if line != "" {
				fmt.Println(line)
			}
			if shouldExit {
				return
			}
		}
	}
}

// sendPayload 统一使用 pre 包的长度头协议发送数据。
func sendPayload(conn net.Conn, payload string) error {
	return pre.WritePacket(conn, []byte(payload))
}

// readLine 读取一行终端输入，并统一去掉 Windows/Linux 换行符。
func readLine(reader *bufio.Reader, prompt string) (string, error) {
	if prompt != "" {
		fmt.Print(prompt)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// printMainMenu 打印登录前菜单。
func printMainMenu() {
	fmt.Println("========== Go 聊天室 ==========")
	fmt.Println("1. 登录")
	fmt.Println("2. 注册")
	fmt.Println("3. 退出")
	fmt.Println("================================")
}

// printChatGuide 打印登录后的聊天命令说明。
func printChatGuide() {
	fmt.Println("[系统] 公聊命令:")
	fmt.Println("[系统] 直接输入内容发送公聊消息")
	fmt.Println("[系统] /chat 用户名 进入私聊")
	fmt.Println("[系统] /list 查看在线用户")
	fmt.Println("[系统] /exit 退出客户端")
}

// translateAuthError 把认证错误码转换成用户可读提示。
func translateAuthError(code string) string {
	if text, ok := authErrors[code]; ok {
		return text
	}
	return "未知认证错误"
}

// translatePrivateEnterError 把进入私聊失败的错误码转换成用户可读提示。
func translatePrivateEnterError(code string) string {
	if text, ok := privateEnterErrors[code]; ok {
		return text
	}
	return "进入私聊失败"
}

// renderServerPacket 把服务端协议包转换成用户可读文本。
// 第二个返回值表示收到该包后客户端是否应该退出。
func renderServerPacket(packet protocol.Packet) (string, bool) {
	switch {
	case packet.Cmd == protocol.CmdPublic:
		return fmt.Sprintf("[公聊] %s: %s", packet.Target, packet.Content), false
	case packet.Cmd == protocol.CmdPrivate:
		return fmt.Sprintf("[私聊] %s: %s", packet.Target, packet.Content), false
	case packet.Cmd == protocol.CmdPrivateAck:
		return fmt.Sprintf("[私聊] 你对 %s 说: %s", packet.Target, packet.Content), false
	case packet.Cmd == protocol.CmdList:
		if strings.TrimSpace(packet.Content) == "" {
			return "[系统] 当前没有在线用户", false
		}
		users := strings.Split(packet.Content, ",")
		return fmt.Sprintf("[系统] 在线用户: %s", strings.Join(users, ", ")), false
	case packet.Cmd == protocol.CmdSystem:
		return fmt.Sprintf("[系统] %s", packet.Message), false
	case packet.Cmd == protocol.CmdShutdown:
		return fmt.Sprintf("[系统] %s", packet.Message), true
	default:
		return "[系统] 收到未知消息", false
	}
}

// applyPrivateEnterResult 是唯一真正改变私聊目标的地方。
// 失败时清空 privateTarget，防止继续向旧目标发送私聊消息。
func applyPrivateEnterResult(session *clientSession, result privateEnterResult) string {
	session.waitingPrivate = false
	if result.ok {
		session.privateTarget = result.target
		return fmt.Sprintf("已进入与 %s 的私聊，输入 /exit 退出私聊", result.target)
	}

	session.privateTarget = ""
	return translatePrivateEnterError(result.code)
}
