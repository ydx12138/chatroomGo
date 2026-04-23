package protocol

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// CmdLogin 表示登录请求。
	CmdLogin = "LOGIN"
	// CmdRegister 表示注册请求。
	CmdRegister = "REGISTER"
	// CmdOK 表示认证成功响应。
	CmdOK = "OK"
	// CmdErr 表示认证失败响应。
	CmdErr = "ERR"
	// CmdPublic 表示公聊消息。
	CmdPublic = "PUBLIC"
	// CmdPrivateEnter 表示请求进入私聊。
	CmdPrivateEnter = "PRIVATE_ENTER"
	// CmdPrivateEnterOK 表示允许进入私聊。
	CmdPrivateEnterOK = "PRIVATE_ENTER_OK"
	// CmdPrivateEnterErr 表示拒绝进入私聊。
	CmdPrivateEnterErr = "PRIVATE_ENTER_ERR"
	// CmdPrivate 表示私聊消息。
	CmdPrivate = "PRIVATE"
	// CmdPrivateAck 表示私聊发送方回执。
	CmdPrivateAck = "PRIVATE_ACK"
	// CmdList 表示在线用户列表请求或响应。
	CmdList = "LIST"
	// CmdQuit 表示客户端退出会话。
	CmdQuit = "QUIT"
	// CmdSystem 表示普通系统提示。
	CmdSystem = "SYSTEM"
	// CmdShutdown 表示服务端关闭通知。
	CmdShutdown = "SHUTDOWN"
)

const (
	// CodeNameExists 表示注册时用户名已经存在。
	CodeNameExists = "NAME_EXISTS"
	// CodeUserNotFound 表示登录时用户不存在。
	CodeUserNotFound = "USER_NOT_FOUND"
	// CodePasswordIncorrect 表示登录时密码错误。
	CodePasswordIncorrect = "PASSWORD_INCORRECT"
	// CodeAlreadyOnline 表示同一账号已经在线。
	CodeAlreadyOnline = "ALREADY_ONLINE"
	// CodeInvalidUsername 表示用户名不满足格式要求。
	CodeInvalidUsername = "INVALID_USERNAME"
	// CodeInvalidPassword 表示密码不满足格式要求。
	CodeInvalidPassword = "INVALID_PASSWORD"
	// CodeDBError 表示数据库操作失败。
	CodeDBError = "DB_ERROR"
	// CodeTargetNotFound 表示私聊目标不存在或不在线。
	CodeTargetNotFound = "TARGET_NOT_FOUND"
	// CodeTargetSelf 表示用户试图和自己私聊。
	CodeTargetSelf = "TARGET_SELF"
	// CodeInvalidCommand 表示报文格式不是协议支持的格式。
	CodeInvalidCommand = "INVALID_COMMAND"
)

const (
	// InputLeavePrivate 是客户端本地动作：退出私聊但不退出程序。
	InputLeavePrivate = "LEAVE_PRIVATE"
	// InputExitClient 是客户端本地动作：退出整个客户端。
	InputExitClient = "EXIT_CLIENT"
)

// ChatMode 表示客户端当前所处的聊天模式。
type ChatMode string

const (
	// ChatModePublic 表示普通输入默认发送到公聊。
	ChatModePublic ChatMode = "PUBLIC"
	// ChatModePrivate 表示普通输入默认发送给当前私聊目标。
	ChatModePrivate ChatMode = "PRIVATE"
)

// Packet 是解析后的统一协议结构。
// 现在只保留一个 Cmd 字段，不再拆成 Kind + Action 两层，协议判断更轻。
type Packet struct {
	// Cmd 是命令名，例如 LOGIN、PUBLIC、PRIVATE。
	Cmd string
	// Username 保存登录/注册请求里的用户名。
	Username string
	// Password 保存登录/注册请求里的密码。
	Password string
	// Target 保存私聊目标、消息发送者或回执目标。
	Target string
	// Content 保存聊天内容或在线列表内容。
	Content string
	// Code 保存错误码。
	Code string
	// Message 保存系统提示文本。
	Message string
}

// InputCommand 是客户端输入解析后的统一结构。
type InputCommand struct {
	// Action 表示输入动作，可以直接复用 CmdPublic、CmdPrivate 等命令名。
	Action string
	// Target 表示私聊目标。
	Target string
	// Text 表示要发送的消息正文。
	Text string
}

// ValidateUsername 校验用户名规则。
func ValidateUsername(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 10 {
		return errors.New("用户名长度必须为1到10位")
	}
	if strings.Contains(name, " ") || strings.Contains(name, "|") {
		return errors.New("用户名不能包含空格或竖线")
	}
	return nil
}

// ValidatePassword 校验密码规则。
func ValidatePassword(password string) error {
	if len(password) < 6 || len(password) > 10 {
		return errors.New("密码长度必须为6到10位")
	}
	if strings.Contains(password, " ") || strings.Contains(password, "|") {
		return errors.New("密码不能包含空格或竖线")
	}
	return nil
}

// ValidateMessage 校验聊天消息。
func ValidateMessage(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("消息不能为空")
	}
	return nil
}

// ParseClientPacket 解析客户端发给服务端的轻量协议。
func ParseClientPacket(raw string) (Packet, error) {
	cmd := firstField(raw)

	switch cmd {
	case CmdLogin, CmdRegister:
		parts := strings.Split(raw, "|")
		if len(parts) != 3 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: cmd, Username: parts[1], Password: parts[2]}, nil
	case CmdPublic:
		content, err := splitContent(raw, 2)
		if err != nil {
			return Packet{}, err
		}
		return Packet{Cmd: CmdPublic, Content: content}, nil
	case CmdPrivateEnter:
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdPrivateEnter, Target: parts[1]}, nil
	case CmdPrivate:
		parts := strings.SplitN(raw, "|", 3)
		if len(parts) != 3 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdPrivate, Target: parts[1], Content: parts[2]}, nil
	case CmdList:
		if raw != CmdList {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdList}, nil
	case CmdQuit:
		if raw != CmdQuit {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdQuit}, nil
	default:
		return Packet{}, invalid(raw)
	}
}

// ParseServerPacket 解析服务端发给客户端的轻量协议。
func ParseServerPacket(raw string) (Packet, error) {
	cmd := firstField(raw)

	switch cmd {
	case CmdOK:
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdOK, Content: parts[1]}, nil
	case CmdErr:
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdErr, Code: parts[1]}, nil
	case CmdSystem, CmdShutdown:
		message, err := splitContent(raw, 2)
		if err != nil {
			return Packet{}, err
		}
		return Packet{Cmd: cmd, Message: message}, nil
	case CmdPublic, CmdPrivate, CmdPrivateAck:
		parts := strings.SplitN(raw, "|", 3)
		if len(parts) != 3 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: cmd, Target: parts[1], Content: parts[2]}, nil
	case CmdPrivateEnterOK:
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdPrivateEnterOK, Target: parts[1]}, nil
	case CmdPrivateEnterErr:
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: CmdPrivateEnterErr, Code: parts[1]}, nil
	case CmdList:
		content, err := splitContent(raw, 2)
		if err != nil {
			return Packet{}, err
		}
		return Packet{Cmd: CmdList, Content: content}, nil
	default:
		return Packet{}, invalid(raw)
	}
}

// ParseChatInput 把用户在客户端输入的一行文本解析成动作。
func ParseChatInput(mode ChatMode, input string) (InputCommand, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return InputCommand{}, errors.New("输入不能为空")
	}

	switch input {
	case "/list":
		return InputCommand{Action: CmdList}, nil
	case "/exit":
		if mode == ChatModePrivate {
			return InputCommand{Action: InputLeavePrivate}, nil
		}
		return InputCommand{Action: InputExitClient}, nil
	}

	if strings.HasPrefix(input, "/chat") {
		if mode == ChatModePrivate {
			return InputCommand{}, errors.New("私聊状态下不能再次进入私聊")
		}
		target := strings.TrimSpace(strings.TrimPrefix(input, "/chat"))
		if err := ValidateUsername(target); err != nil {
			return InputCommand{}, err
		}
		return InputCommand{Action: CmdPrivateEnter, Target: target}, nil
	}

	if strings.HasPrefix(input, "/") {
		return InputCommand{}, errors.New("未知命令")
	}
	if err := ValidateMessage(input); err != nil {
		return InputCommand{}, err
	}
	if mode == ChatModePrivate {
		return InputCommand{Action: CmdPrivate, Text: input}, nil
	}
	return InputCommand{Action: CmdPublic, Text: input}, nil
}

// BuildAuthRequest 构造登录或注册请求。
func BuildAuthRequest(cmd, username, password string) string {
	return strings.Join([]string{cmd, username, password}, "|")
}

// BuildPublicMessage 构造客户端公聊消息。
func BuildPublicMessage(content string) string {
	return strings.Join([]string{CmdPublic, content}, "|")
}

// BuildPrivateEnterRequest 构造进入私聊请求。
func BuildPrivateEnterRequest(target string) string {
	return strings.Join([]string{CmdPrivateEnter, target}, "|")
}

// BuildPrivateMessage 构造客户端私聊消息。
func BuildPrivateMessage(target, content string) string {
	return strings.Join([]string{CmdPrivate, target, content}, "|")
}

// BuildListRequest 构造在线列表请求。
func BuildListRequest() string {
	return CmdList
}

// BuildQuitRequest 构造客户端退出请求。
func BuildQuitRequest() string {
	return CmdQuit
}

// BuildAuthOK 构造认证成功响应。
func BuildAuthOK(action string) string {
	return strings.Join([]string{CmdOK, action}, "|")
}

// BuildAuthErr 构造认证失败响应。
func BuildAuthErr(code string) string {
	return strings.Join([]string{CmdErr, code}, "|")
}

// BuildSystemInfo 构造系统提示消息。
func BuildSystemInfo(message string) string {
	return strings.Join([]string{CmdSystem, message}, "|")
}

// BuildSystemErr 构造系统错误消息。
func BuildSystemErr(message string) string {
	return strings.Join([]string{CmdSystem, message}, "|")
}

// BuildPublicBroadcast 构造服务端公聊广播消息。
func BuildPublicBroadcast(sender, content string) string {
	return strings.Join([]string{CmdPublic, sender, content}, "|")
}

// BuildPrivateInbound 构造发给私聊接收者的消息。
func BuildPrivateInbound(sender, content string) string {
	return strings.Join([]string{CmdPrivate, sender, content}, "|")
}

// BuildPrivateAck 构造发给私聊发送者的回执。
func BuildPrivateAck(target, content string) string {
	return strings.Join([]string{CmdPrivateAck, target, content}, "|")
}

// BuildPrivateEnterOK 构造进入私聊成功响应。
func BuildPrivateEnterOK(target string) string {
	return strings.Join([]string{CmdPrivateEnterOK, target}, "|")
}

// BuildPrivateEnterErr 构造进入私聊失败响应。
func BuildPrivateEnterErr(code string) string {
	return strings.Join([]string{CmdPrivateEnterErr, code}, "|")
}

// BuildUserList 构造在线用户列表响应。
func BuildUserList(users []string) string {
	return strings.Join([]string{CmdList, strings.Join(users, ",")}, "|")
}

// BuildShutdown 构造服务端关闭通知。
func BuildShutdown(message string) string {
	return strings.Join([]string{CmdShutdown, message}, "|")
}

func firstField(raw string) string {
	if i := strings.Index(raw, "|"); i >= 0 {
		return raw[:i]
	}
	return raw
}

func splitContent(raw string, parts int) (string, error) {
	fields := strings.SplitN(raw, "|", parts)
	if len(fields) != parts {
		return "", invalid(raw)
	}
	return fields[len(fields)-1], nil
}

func invalid(raw string) error {
	return fmt.Errorf("%s: %q", CodeInvalidCommand, raw)
}
