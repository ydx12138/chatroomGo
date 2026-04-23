package pre

import (
	"encoding/binary"
	"io"
	"net"
)

func WritePacket(conn net.Conn, data []byte) error {
	//fmt.Println("aaa")
	length := uint32(len(data)) //消息的长度
	header := make([]byte, 4)   //四字节刚好是int32的长度
	//把长度以十六进制，存到头里
	//BigEndian：高位在前，低位在后
	//PutUint32：把int32放到切片里
	binary.BigEndian.PutUint32(header, length)

	if _, err := conn.Write(header); err != nil { //先发头
		return err
	}
	_, err := conn.Write(data) //再发消息
	return err
}

func ReadPacket(conn net.Conn) (string, error) {
	//fmt.Println("aaa")
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil { //先接收头
		return "", err
	}

	length := binary.BigEndian.Uint32(header) //获得消息长度
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil { //再接收length长度的消息
		return "", err
	}
	return string(data), nil
}
