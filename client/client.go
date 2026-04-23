package main

import (
	"DEMO2/TCPIP/pre"
	"bufio"
	"fmt"
	"os"
	//"github.com/go-sql-driver/utils"
	"net"
	"strings"
)

// 键盘输入控制器
var reader = bufio.NewReader(os.Stdin)
var ch = make(chan string)

func main() {
	//连接服务器
	conn, err := net.Dial("tcp", "localhost:8888")
	if err != nil {
		fmt.Println("net.Dial客户端连接失败=", err)
		return
	}
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(conn)
	fmt.Println("═══════════════════════════════")
	fmt.Println("        ☁️Go 聊天室           ")
	fmt.Println("═══════════════════════════════")
	fmt.Println("  欢迎使用！                    ")
	fmt.Println("  直接输入消息      → 发送群聊    ")
	fmt.Println("  @用户名 消息内容  → 发送私聊    ")
	fmt.Println("  /list            → 查看在线用户")
	fmt.Println("  exit             → 退出聊天室 ")
	fmt.Println("  输入用户名和密码即可进入聊天    ")
	fmt.Println("═══════════════════════════════")
	fmt.Println()
	//接收信息,必须在前,不然关闭太快,read读不到EOF
	go ReceiveRadio(conn)
	//名字和密码
	nameORPasswrd(0, conn)
	nameORPasswrd(1, conn)
	//发送信息
	go send(reader, conn)
	<-ch
}

// 名字和密码
func nameORPasswrd(i int, conn net.Conn) {
	for {
		//用户输入名字
		if i == 0 {
			fmt.Print("输入你在群聊中的名称:")
		} else {
			fmt.Print("输入密码:")
		}
		name, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("键盘读取名字失败=", err)
			continue
		}
		//去除回车符
		name = strings.TrimSpace(name)
		//检查名字格式：不为空，无空格，不超过128字符
		if name == "" || strings.Contains(name, " ") || len(name) > 128 {
			fmt.Println("名字格式不对,不能为空，不能有空格，不能超过128字节")
			continue
		}
		//发送名字
		err = pre.WritePacket(conn, []byte(name))
		if err != nil {
			fmt.Println("发送名字失败=", err)
			ch <- "1"
			return
		}
		break
	}
}

// 发送消息
func send(reader *bufio.Reader, conn net.Conn) {
	for {
		//1.从键盘获取消息
		line, err := reader.ReadString('\n') //阻塞
		if err != nil {
			fmt.Println("键盘读取消息失败=", err)
			break
		}
		//去除空白符  \n\t\r ""
		line = strings.TrimSpace(line)
		//exit退出
		if line == "exit" {
			ch <- "1"
			break
		}
		//不能为空，不能超过4096个字节
		if line == "" || len(line) > 4096 {
			fmt.Println("输入不能为空，也不能太长")
			continue
		}
		//2.发送
		err = pre.WritePacket(conn, []byte(line))
		if err != nil {
			fmt.Println("conn.Write发送失败=", err)
			ch <- "1"
			break
		}
	}
}

// ReceiveRadio 接收广播
func ReceiveRadio(conn net.Conn) {
	for {
		var buf string
		buf, err := pre.ReadPacket(conn)
		if err != nil {
			fmt.Println("连接关闭")
			ch <- "1"
			return
		}
		fmt.Println(buf)
	}

}
