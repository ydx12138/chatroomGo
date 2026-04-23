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

// defaultServerAddr 是客户端默认连接的服务端地址。
// 如果你在本机运行服务端，就保持这个值不变。
const defaultServerAddr = "localhost:8888"

// errClientExit 是一个内部哨兵错误。
// 它不表示程序异常，只表示用户在主菜单选择了“退出”。
var errClientExit = errors.New("client exit")

// clientSession 保存“登录成功以后”的客户端本地状态。
// 这里的状态只影响客户端如何解释用户输入，不会直接写进数据库。
type clientSession struct {
	// username 是当前登录成功的用户名。
	username string
	// mode 表示当前聊天模式：公聊模式或私聊模式。
	mode protocol.ChatMode
	// privateTarget 表示当前私聊锁定的对象。
	// 只有 mode == protocol.ChatModePrivate 时它才有意义。
	privateTarget string
	// waitingPrivate 表示客户端已经发出“进入私聊”请求，但还没有收到服务端确认。
	// 这个字段可以防止用户连续输入多个 /chat 命令导致状态混乱。
	waitingPrivate bool
}

// privateEnterResult 表示服务端对“进入私聊请求”的返回结果。
// 接收协程收到服务端响应后，会把结果封装成这个结构交给主聊天循环处理。
type privateEnterResult struct {
	// ok 为 true 表示允许进入私聊。
	ok bool
	// target 是私聊目标用户名。
	target string
	// code 是进入私聊失败时的错误码。
	code string
}

// main 是客户端入口函数。
// 它先进入主菜单，登录成功后再进入聊天流程。
func main() {
	// reader 统一负责读取命令行输入。
	reader := bufio.NewReader(os.Stdin)
	// runMainMenu 会一直运行到登录成功或用户退出。
	username, conn, err := runMainMenu(reader)
	// 如果主菜单返回错误，需要判断是用户主动退出还是异常错误。
	if err != nil {
		// errClientExit 表示正常退出，不需要打印异常信息。
		//如果err是errClientExit，则是正常的退出，否则就就是异常退出
		if !errors.Is(err, errClientExit) {
			// 其他错误才说明客户端异常。
			fmt.Println("[系统] 客户端异常退出:", err)
		}
		// 认证阶段没有成功进入聊天，直接结束程序。
		return
	}
	// 登录成功后，这条连接会继续用于聊天；程序结束前要关闭。
	defer func() {
		// 关闭连接失败不影响客户端退出，所以这里忽略错误。
		_ = conn.Close()
	}()

	// 登录成功后创建本地会话状态。
	session := &clientSession{
		// 记录当前用户，方便后续扩展使用。
		username: username,
		// 登录后默认处于公聊模式。
		mode: protocol.ChatModePublic,
	}
	// 进入真正的聊天循环。
	runChat(conn, reader, session)
}

// serverAddr 返回客户端要连接的服务端地址。
// 默认连接 localhost:8888；如果设置了 CHAT_SERVER_ADDR，就使用环境变量。
func serverAddr() string {
	// 先读取环境变量，方便跨机器测试。
	//if addr := os.Getenv("CHAT_SERVER_ADDR"); addr != "" {
	//	// 环境变量非空就直接使用。
	//	return addr
	//}
	// 没有配置环境变量时使用默认地址。
	return defaultServerAddr
}

// runMainMenu 负责认证前的主菜单循环。
// 返回值说明：
// - string：登录成功的用户名
// - net.Conn：登录成功后要继续聊天的连接
// - error：退出或异常
func runMainMenu(reader *bufio.Reader) (string, net.Conn, error) {
	// 主菜单必须反复显示，直到用户登录成功或主动退出。
	for {
		// 每一轮都先打印菜单。
		printMainMenu()
		// 读取
		choice, err := readLine(reader, "请选择功能: ")
		// 读取命令行失败时，直接把错误交给 main。
		if err != nil {
			return "", nil, err
		}

		// TrimSpace 用来忽略用户输入前后的空白字符。
		switch strings.TrimSpace(choice) {
		case "1":
			// 选择 1 表示登录。
			username, conn, err := doAuthFlow(reader, protocol.CmdLogin)
			// 登录成功时 username 非空，并且 conn 是后续聊天连接。
			if err == nil && username != "" {
				return username, conn, nil
			}
			// 登录过程中出现异常，打印后回到主菜单。
			if err != nil {
				fmt.Println("[系统]", err)
			}
		case "2":
			// 选择 2 表示注册。
			_, conn, err := doAuthFlow(reader, protocol.CmdRegister)
			// 注册失败时打印错误，然后回到主菜单。
			if err != nil {
				fmt.Println("[系统]", err)
				continue
			}
			// 注册成功不自动登录，所以本次注册连接要关闭。
			if conn != nil {
				_ = conn.Close()
			}
		case "3":
			// 选择 3 表示退出客户端。
			fmt.Println("[系统] 客户端已退出")
			// 用 errClientExit 通知 main：这是正常退出。
			return "", nil, errClientExit
		default:
			// 其他输入都视为无效菜单项。
			fmt.Println("[系统] 无效选项，请重新输入")
		}
	}
}

// doAuthFlow 负责执行“一次登录”或“一次注册”。
// 它会完成：采集用户名密码、连接服务端、发送认证包、读取认证响应。
func doAuthFlow(reader *bufio.Reader, action string) (string, net.Conn, error) {
	// 先读取并校验用户名和密码。
	username, password, err := promptCredentials(reader)
	// 如果本地校验失败，就不需要连接服务端。
	if err != nil {
		return "", nil, err
	}

	// 每一次登录/注册都单独建立 TCP 连接。
	conn, err := net.Dial("tcp", serverAddr())
	// 如果连不上服务端，直接返回错误给主菜单。
	if err != nil {
		return "", nil, fmt.Errorf("连接服务端失败: %w", err)
	}

	// 构造认证请求，并通过 pre.WritePacket 发送。
	if err := sendPayload(conn, protocol.MakePacket(action, username, password)); err != nil {
		// 发送失败时，这条连接已经不能继续用，必须关闭。
		_ = conn.Close()
		return "", nil, fmt.Errorf("发送认证请求失败: %w", err)
	}

	// 读取服务端认证响应。
	raw, err := pre.ReadPacket(conn)
	// 读响应失败时关闭连接，回到主菜单。
	if err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("读取认证响应失败: %w", err)
	}
	// 把服务端返回的字符串解析成统一结构。
	packet, err := protocol.ParseServerPacket(raw)
	// 解析失败说明服务端响应格式不符合协议。
	if err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("解析认证响应失败: %w", err)
	}
	// 认证阶段只能收到 AUTH 类型响应。
	// ERR|xxx 表示登录或注册失败。
	if packet.Cmd == protocol.CmdErr {
		// 把协议错误码翻译成用户能看懂的中文。
		fmt.Println("[系统]", translateAuthError(packet.Code))
		// 认证失败后关闭连接，因为用户要回主菜单重新选择。
		_ = conn.Close()
		// 这里返回 nil 错误，表示失败原因已经提示过，不需要外层再打印异常。
		return "", nil, nil
	}

	// 除了 OK 和 ERR，其他认证响应都属于异常协议。
	if packet.Cmd != protocol.CmdOK {
		_ = conn.Close()
		return "", nil, errors.New("收到未知认证响应")
	}

	// 根据当前请求类型决定成功后的行为。
	switch action {
	case protocol.CmdRegister:
		// 注册成功后只提示成功，不进入聊天。
		fmt.Println("[系统] 注册成功，请返回主菜单登录")
		// 返回 conn 让调用方关闭，保持连接生命周期清晰。
		return "", conn, nil
	case protocol.CmdLogin:
		// 登录成功后打印欢迎语。
		fmt.Printf("[系统] 登录成功，欢迎 %s\n", username)
		// 打印聊天命令说明。
		printChatGuide()
		// 返回用户名和连接，进入聊天阶段。
		return username, conn, nil
	default:
		// 理论上不会走到这里，除非调用方传了未知 action。
		_ = conn.Close()
		return "", nil, errors.New("未知认证动作")
	}
}

// promptCredentials 负责读取用户名和密码，并做客户端本地校验。
// 服务端仍会再次校验，所以这里主要是为了更快给用户提示。
func promptCredentials(reader *bufio.Reader) (string, string, error) {
	// 读取用户名。
	username, err := readLine(reader, "请输入用户名: ")
	// 输入读取失败时直接返回。
	if err != nil {
		return "", "", err
	}
	// 校验用户名长度、空格、协议分隔符等规则。
	if err := protocol.ValidateUsername(username); err != nil {
		return "", "", err
	}

	// 读取密码。
	password, err := readLine(reader, "请输入密码: ")
	// 输入读取失败时直接返回。
	if err != nil {
		return "", "", err
	}
	// 校验密码长度、空格、协议分隔符等规则。
	if err := protocol.ValidatePassword(password); err != nil {
		return "", "", err
	}

	// 校验全部通过，返回用户名和密码。
	return username, password, nil
}

// runChat 是登录后的主聊天循环。
// 它需要同时处理两种事件：用户键盘输入、服务端推送消息。
func runChat(conn net.Conn, reader *bufio.Reader, session *clientSession) {
	// inputCh 用来接收键盘输入协程读到的内容。
	inputCh := make(chan string)
	// privateEnterCh 用来接收“进入私聊”的异步结果。
	privateEnterCh := make(chan privateEnterResult, 1)
	// doneCh 用来通知主循环：服务端连接已经结束。
	doneCh := make(chan struct{})

	// 单独启动一个协程读取键盘输入，避免阻塞接收服务端消息。
	go readChatInput(reader, inputCh)
	// 单独启动一个协程读取服务端消息，避免阻塞用户输入。
	go receiveLoop(conn, privateEnterCh, doneCh)

	// select 循环让客户端可以同时响应多个来源的事件。
	for {
		select {
		case line, ok := <-inputCh:
			// inputCh 关闭说明标准输入读取失败或结束。
			if !ok {
				return
			}
			// 根据当前聊天状态处理这行输入。
			shouldExit, err := handleChatInput(conn, session, line)
			// 输入格式错误时只提示，不退出聊天。
			if err != nil {
				fmt.Println("[系统]", err)
				continue
			}
			// 公聊状态输入 /exit 时 shouldExit 为 true。
			if shouldExit {
				fmt.Println("[系统] 客户端已退出")
				return
			}
		case result := <-privateEnterCh:
			// 进入私聊的结果必须回到主循环统一修改 session。
			fmt.Println("[系统]", applyPrivateEnterResult(session, result))
		case <-doneCh:
			// 服务端断开或关服时，接收协程会关闭 doneCh。
			return
		}
	}
}

// handleChatInput 把用户输入的一行文本转成具体动作。
// 返回值 shouldExit 表示是否需要退出整个客户端。
func handleChatInput(conn net.Conn, session *clientSession, line string) (bool, error) {
	// 如果正在等待进入私聊结果，不允许继续输入新的聊天命令。
	if session.waitingPrivate {
		return false, errors.New("正在等待私聊确认，请稍候")
	}

	// 根据当前模式解析输入。
	cmd, err := protocol.ParseChatInput(session.mode, line)
	// 解析失败表示输入为空、未知命令、私聊嵌套等情况。
	if err != nil {
		return false, err
	}

	// 根据解析后的动作执行网络发送或本地状态修改。
	switch cmd.Action {
	case protocol.CmdPublic:
		// 公聊普通消息：发给服务端，由服务端广播。
		return false, sendPayload(conn, protocol.MakePacket(protocol.CmdPublic, cmd.Text))
	case protocol.CmdPrivate:
		// 私聊普通消息：目标固定为当前 privateTarget。
		return false, sendPayload(conn, protocol.MakePacket(protocol.CmdPrivate, session.privateTarget, cmd.Text))
	case protocol.CmdList:
		// /list：请求服务端返回在线用户列表。
		return false, sendPayload(conn, protocol.CmdList)
	case protocol.CmdPrivateEnter:
		// /chat 用户名：先标记等待，再请求服务端校验目标是否在线。
		session.waitingPrivate = true
		return false, sendPayload(conn, protocol.MakePacket(protocol.CmdPrivateEnter, cmd.Target))
	case protocol.InputLeavePrivate:
		// 私聊状态下 /exit：只退出私聊，不退出客户端。
		session.mode = protocol.ChatModePublic
		// 清空私聊对象，防止后续误用旧目标。
		session.privateTarget = ""
		// 退出私聊后也不再等待任何私聊确认。
		session.waitingPrivate = false
		// 给用户明确提示当前已经回到公聊。
		fmt.Println("[系统] 已退出私聊，回到公聊")
		return false, nil
	case protocol.InputExitClient:
		// 公聊状态下 /exit：通知服务端退出会话。
		_ = sendPayload(conn, protocol.CmdQuit)
		// 返回 true 让 runChat 结束客户端。
		return true, nil
	default:
		// 理论上不会走到这里，除非协议层新增动作但这里没处理。
		return false, errors.New("未知输入动作")
	}
}

// readChatInput 持续读取用户在终端输入的每一行内容。
// 这个函数只负责读取，不负责解析，也不负责网络发送。
func readChatInput(reader *bufio.Reader, inputCh chan<- string) {
	// 一直读取，直到标准输入出错。
	for {
		// 空 prompt 表示聊天阶段不额外打印输入提示符。
		line, err := readLine(reader, "")
		// 如果读取失败，就关闭 channel 通知主循环退出。
		if err != nil {
			close(inputCh)
			return
		}
		// 把用户输入交给 runChat 主循环处理。
		inputCh <- line
	}
}

// receiveLoop 持续读取服务端发来的消息。
// 注意：这个协程不直接修改 clientSession，避免多个 goroutine 同时改状态。
func receiveLoop(conn net.Conn, privateEnterCh chan<- privateEnterResult, doneCh chan<- struct{}) {
	// 函数退出时关闭 doneCh，通知 runChat 服务端连接已经结束。
	defer close(doneCh)

	// 只要连接不断，就持续读取服务端包。
	for {
		// 从 TCP 连接中读取一个完整业务包。
		raw, err := pre.ReadPacket(conn)
		// 读取失败通常表示服务端关闭或网络断开。
		if err != nil {
			fmt.Println("[系统] 与服务端的连接已关闭")
			return
		}

		// 把服务端字符串包解析成统一结构。
		packet, err := protocol.ParseServerPacket(raw)
		// 如果解析失败，说明服务端消息格式不符合协议。
		if err != nil {
			fmt.Println("[系统] 收到无法识别的服务端消息")
			continue
		}

		// 根据服务端消息类型决定怎么处理。
		switch {
		case packet.Cmd == protocol.CmdPrivateEnterOK:
			// 进入私聊成功：把结果交给主循环更新 session。
			privateEnterCh <- privateEnterResult{ok: true, target: packet.Target}
		case packet.Cmd == protocol.CmdPrivateEnterErr:
			// 进入私聊失败：把错误码交给主循环翻译并提示。
			privateEnterCh <- privateEnterResult{ok: false, code: packet.Code}
		default:
			// 其他消息都是展示类消息或关服消息。
			line, shouldExit := renderServerPacket(packet)
			// line 非空时打印给用户看。
			if line != "" {
				fmt.Println(line)
			}
			// shouldExit 为 true 表示服务端要求客户端退出。
			if shouldExit {
				return
			}
		}
	}
}

// sendPayload 是客户端统一发包函数。
// 所有网络发送都通过这里，底层仍然使用 pre.WritePacket 解决粘包问题。
func sendPayload(conn net.Conn, payload string) error {
	// 把字符串转成字节切片后交给 pre 包封包发送。
	return pre.WritePacket(conn, []byte(payload))
}

// readLine 负责读取一行命令行输入。
// prompt 非空时会先打印提示语。
// 返回从键盘读取的，处理好的字串
func readLine(reader *bufio.Reader, prompt string) (string, error) {
	// 只有需要提示时才打印 prompt。
	if prompt != "" {
		fmt.Print(prompt)
	}
	// ReadString('\n') 会一直读到换行符。
	line, err := reader.ReadString('\n')
	// 如果读取失败，把错误交给调用方。
	if err != nil {
		return "", err
	}
	// 去掉 Windows 和 Linux 换行符\r和\n，保留用户真正输入的内容。
	return strings.TrimRight(line, "\r\n"), nil
}

// printMainMenu 打印登录前主菜单。
func printMainMenu() {
	// 菜单头部。
	fmt.Println("========== Go 聊天室 ==========")
	// 选项 1：登录。
	fmt.Println("1. 登录")
	// 选项 2：注册。
	fmt.Println("2. 注册")
	// 选项 3：退出。
	fmt.Println("3. 退出")
	// 菜单底部。
	fmt.Println("================================")
}

// printChatGuide 打印登录成功后的聊天命令说明。
func printChatGuide() {
	// 标明下面是公聊状态命令。
	fmt.Println("[系统] 公聊命令:")
	// 说明普通文本如何发送。
	fmt.Println("[系统] 直接输入内容发送公聊消息")
	// 说明如何进入私聊。
	fmt.Println("[系统] /chat 用户名 进入私聊")
	// 说明如何查看在线用户。
	fmt.Println("[系统] /list 查看在线用户")
	// 说明公聊状态下如何退出客户端。
	fmt.Println("[系统] /exit 退出客户端")
}

// translateAuthError 把服务端认证错误码转换成中文提示。
// 这样用户不会看到 NAME_EXISTS 这种协议内部字符串。
func translateAuthError(code string) string {
	// 按错误码逐一映射。
	switch code {
	case protocol.CodeNameExists:
		return "用户名已存在"
	case protocol.CodeUserNotFound:
		return "用户不存在"
	case protocol.CodePasswordIncorrect:
		return "密码错误"
	case protocol.CodeAlreadyOnline:
		return "该用户已经在线"
	case protocol.CodeInvalidUsername:
		return "用户名格式不正确"
	case protocol.CodeInvalidPassword:
		return "密码格式不正确"
	case protocol.CodeDBError:
		return "数据库操作失败"
	default:
		return "未知认证错误"
	}
}

// translatePrivateEnterError 把进入私聊失败的错误码转换成中文提示。
func translatePrivateEnterError(code string) string {
	// 根据服务端返回的具体错误码提示原因。
	switch code {
	case protocol.CodeTargetNotFound:
		return "目标用户不在线或不存在"
	case protocol.CodeTargetSelf:
		return "不能和自己进入私聊"
	case protocol.CodeInvalidUsername:
		return "私聊用户名格式不正确"
	default:
		return "进入私聊失败"
	}
}

// renderServerPacket 把服务端消息渲染成用户能直接看的文本。
// 第二个返回值表示收到该消息后是否应该退出客户端。
func renderServerPacket(packet protocol.Packet) (string, bool) {
	// 根据消息种类和动作决定展示格式。
	switch {
	case packet.Cmd == protocol.CmdPublic:
		// 公聊消息显示为：[公聊] 用户: 内容。
		return fmt.Sprintf("[公聊] %s: %s", packet.Target, packet.Content), false
	case packet.Cmd == protocol.CmdPrivate:
		// 收到别人发来的私聊消息。
		return fmt.Sprintf("[私聊] %s: %s", packet.Target, packet.Content), false
	case packet.Cmd == protocol.CmdPrivateAck:
		// 自己发送私聊成功后的回执。
		return fmt.Sprintf("[私聊] 你对 %s 说: %s", packet.Target, packet.Content), false
	case packet.Cmd == protocol.CmdList:
		// 在线列表为空时单独提示。
		if strings.TrimSpace(packet.Content) == "" {
			return "[系统] 当前没有在线用户", false
		}
		// 服务端用逗号拼接在线用户，这里拆开后加空格提升可读性。
		users := strings.Split(packet.Content, ",")
		return fmt.Sprintf("[系统] 在线用户: %s", strings.Join(users, ", ")), false
	case packet.Cmd == protocol.CmdSystem:
		// 普通系统提示直接显示 message。
		return fmt.Sprintf("[系统] %s", packet.Message), false
	case packet.Cmd == protocol.CmdShutdown:
		// 服务端关闭时，客户端显示提示并退出。
		return fmt.Sprintf("[系统] %s", packet.Message), true
	default:
		// 未覆盖的协议包给一个兜底提示，避免静默无响应。
		return "[系统] 收到未知消息", false
	}
}

// applyPrivateEnterResult 根据服务端响应修改本地私聊状态。
// 这里是唯一真正改变 mode/privateTarget 的地方。
func applyPrivateEnterResult(session *clientSession, result privateEnterResult) string {
	// 不管成功失败，收到响应后都不再处于等待状态。
	session.waitingPrivate = false
	// 成功时切换到私聊模式。
	if result.ok {
		// 记录当前模式为私聊。
		session.mode = protocol.ChatModePrivate
		// 锁定私聊目标，后续普通输入都发给这个人。
		session.privateTarget = result.target
		// 返回给用户看的提示语。
		return fmt.Sprintf("已进入与 %s 的私聊，输入 /exit 退出私聊", result.target)
	}

	// 失败时强制保持公聊状态。
	session.mode = protocol.ChatModePublic
	// 失败时不能留下旧的私聊目标。
	session.privateTarget = ""
	// 把失败错误码翻译成中文提示。
	return translatePrivateEnterError(result.code)
}
