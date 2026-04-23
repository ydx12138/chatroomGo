package pre

import (
	"encoding/binary"
	"io"
	"net"
)

// WritePacket 发送一个完整的数据包。
// TCP 本身是字节流，没有“消息边界”，所以这里手动加一个长度头来解决粘包/拆包问题。
func WritePacket(conn net.Conn, data []byte) error {
	// length 表示消息体的字节长度。
	length := uint32(len(data))
	// header 固定 4 个字节，用来存储消息体长度。
	header := make([]byte, 4)
	// BigEndian 表示高位字节在前，接收方也用同样规则读取即可。
	binary.BigEndian.PutUint32(header, length)

	// 先发送 4 字节长度头。
	if _, err := conn.Write(header); err != nil {
		// 头部发送失败，直接返回错误。
		return err
	}
	// 再发送真实消息体。
	_, err := conn.Write(data)
	// 返回消息体发送结果。
	return err
}

// ReadPacket 读取一个完整的数据包。
// 它和 WritePacket 配套使用：先读 4 字节长度头，再按长度读消息体。
func ReadPacket(conn net.Conn) (string, error) {
	// header 用来接收 4 字节长度头。
	header := make([]byte, 4)
	// io.ReadFull 会一直读，直到读满 header 或发生错误。
	if _, err := io.ReadFull(conn, header); err != nil {
		// 长度头没读完整，说明连接断开或发生网络错误。
		return "", err
	}

	// 从 4 字节头部解析出消息体长度。
	length := binary.BigEndian.Uint32(header)
	// data 按消息体长度分配空间。
	data := make([]byte, length)
	// 按刚才解析出的长度完整读取消息体。
	if _, err := io.ReadFull(conn, data); err != nil {
		// 消息体没读完整，也说明连接断开或发生网络错误。
		return "", err
	}
	// 把消息体字节转成字符串返回给业务层。
	return string(data), nil
}
