package pre

import (
	"encoding/binary"
	"io"
	"net"
)

// WritePacket 写入一个带 4 字节长度头的完整业务包。
// TCP 是字节流，没有消息边界，所以发送端必须先写长度再写正文。
func WritePacket(conn net.Conn, data []byte) error {
	length := uint32(len(data)) //消息的长度
	header := make([]byte, 4)   //四字节刚好是int32的长度
	//BigEndian：高位在前，低位在后
	//PutUint32：把int32放到切片里
	binary.BigEndian.PutUint32(header, length)

	if _, err := conn.Write(header); err != nil { //先发头
		return err
	}
	_, err := conn.Write(data) //再发数据
	return err
}

// ReadPacket 按 WritePacket 的格式读取一个完整业务包。
// io.ReadFull 会等到长度头和正文都读满，避免半包被上层当成完整消息。
func ReadPacket(conn net.Conn) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil { //接收4字节的头
		return "", err
	}

	length := binary.BigEndian.Uint32(header) //解析出后面信息的长度
	data := make([]byte, length)              //对应长度的data来接收
	if _, err := io.ReadFull(conn, data); err != nil {
		return "", err
	}
	return string(data), nil
}
