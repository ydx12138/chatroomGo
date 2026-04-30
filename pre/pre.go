package pre

import (
	"encoding/binary"
	"io"
	"net"
)

// WritePacket 写入一个带 4 字节长度头的完整业务包。
// TCP 是字节流，没有消息边界，所以发送端必须先写长度再写正文。
func WritePacket(conn net.Conn, data []byte) error {
	length := uint32(len(data))
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, length)

	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

// ReadPacket 按 WritePacket 的格式读取一个完整业务包。
// io.ReadFull 会等到长度头和正文都读满，避免半包被上层当成完整消息。
func ReadPacket(conn net.Conn) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}

	length := binary.BigEndian.Uint32(header)
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return "", err
	}
	return string(data), nil
}
