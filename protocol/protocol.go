package protocol

import (
	"errors"
	"fmt"
	"strings"
)

// 协议使用竖线分隔字段，命令常量集中在这里避免客户端和服务端写散。
const (
	CmdLogin           = "LOGIN"
	CmdRegister        = "REGISTER"
	CmdOK              = "OK"
	CmdErr             = "ERR"
	CmdPublic          = "PUBLIC"
	CmdPrivateEnter    = "PRIVATE_ENTER"
	CmdPrivateEnterOK  = "PRIVATE_ENTER_OK"
	CmdPrivateEnterErr = "PRIVATE_ENTER_ERR"
	CmdPrivate         = "PRIVATE"
	CmdPrivateAck      = "PRIVATE_ACK"
	CmdList            = "LIST"
	CmdQuit            = "QUIT"
	CmdSystem          = "SYSTEM"
	CmdShutdown        = "SHUTDOWN"
)

// Packet 是客户端和服务端协议解析后的统一结构。
// 不同命令只使用其中一部分字段，避免为每种包类型创建一组小结构。
type Packet struct {
	Cmd      string
	Username string
	Password string
	Target   string
	Content  string
	Code     string
	Message  string
}

// ValidateUsername 校验用户名是否能安全放入文本协议字段。
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

// ValidatePassword 校验密码是否能安全放入文本协议字段。
func ValidatePassword(password string) error {
	if len(password) < 6 || len(password) > 10 {
		return errors.New("密码长度必须为6到10位")
	}
	if strings.Contains(password, " ") || strings.Contains(password, "|") {
		return errors.New("密码不能包含空格或竖线")
	}
	return nil
}

// ValidateMessage 校验聊天正文。
// 正文可以包含竖线，所以解析消息包时必须使用 SplitN。
func ValidateMessage(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("消息不能为空")
	}
	return nil
}

// ParseClientPacket 解析客户端发给服务端的协议包。
// 聊天正文允许包含竖线，因此 PUBLIC/PRIVATE 使用限制次数的拆分。
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
	case CmdList, CmdQuit:
		if raw != cmd {
			return Packet{}, invalid(raw)
		}
		return Packet{Cmd: cmd}, nil
	default:
		return Packet{}, invalid(raw)
	}
}

// ParseServerPacket 解析服务端发给客户端的协议包。
// 展示类消息和聊天正文同样允许包含竖线。
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

// MakePacket 用命令名和字段组装协议字符串。
func MakePacket(cmd string, fields ...string) string {
	parts := append([]string{cmd}, fields...)
	return strings.Join(parts, "|")
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
	return fmt.Errorf("INVALID_COMMAND: %q", raw)
}
