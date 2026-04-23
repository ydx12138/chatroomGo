package main

import (
	"DEMO2/TCPIP/mysql"
	"DEMO2/TCPIP/pre"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

var (
	connUser = make(map[net.Conn]string, 100) //连接和用户名一一对应
	lock     sync.RWMutex                     //操作connUser时加读写锁
	listener net.Listener
	ch       chan string
)

func main() {

	//监听一个端口8888
	fmt.Println("开始监听")
	var err1 error
	listener, err1 = net.Listen("tcp", "0.0.0.0:8888")
	if err1 != nil {
		fmt.Println("net.Listen监听失败:", err1)
		return
	}
	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(listener)

	ch = make(chan string, 10000)
	defer close(ch)
	go Radio(ch)
	mysql.InitMysql()
	defer mysql.CloseMysql()
	for {
		//阻塞，等待连接
		conn, err2 := listener.Accept()
		if err2 != nil {
			fmt.Println("listener.Accept连接出错:", err2)
			continue
		} else {
			fmt.Printf("%v连接到服务端\n", conn.RemoteAddr().String())
		}
		//对每一个访问，单独开一个协程处理
		go Process(conn, ch)
	}
}

// 单个断开连接的提示
func tip(conn net.Conn, str string) {
	err := pre.WritePacket(conn, []byte(str))
	if err != nil {
		fmt.Println(err)
		return
	}
}

// 超时时间
func outTime(conn net.Conn) {
	err := conn.SetDeadline(time.Now().Add(3600 * time.Second))
	if err != nil {
		return
	}
}

// Process 接收
func Process(conn net.Conn, ch chan<- string) {
	//关闭conn 删除conn
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			fmt.Println(err)
		}
		v, ok := connUser[conn]
		if ok == true {
			ch <- "<" + v + ">" + "退出聊天室"
		}
		delete(connUser, conn)
	}(conn)
	var nameStr, passwordStr string
	//超时时间
	outTime(conn)
	//接收名字
	nameStr, err := pre.ReadPacket(conn)
	if err != nil {
		fmt.Println("name读取失败:", err)
		return
	}
	//接收用户密码
	outTime(conn)
	passwordStr, err = pre.ReadPacket(conn)
	if err != nil {
		fmt.Println("password读取失败:", err)
		return
	}

	//1名字是否在数据库--注册并登录--加到map
	//2在数据库，但密码错--登陆失败
	//3在数据库，密码对，但map在线--重复登陆
	//3在数据库，密码对，map不在线--正常登录--加到map

	if mysql.QueryOneByNameOrPass(nameStr, "") == false {
		mysql.InsertOne(nameStr, passwordStr)
		lock.Lock()
		connUser[conn] = nameStr
		lock.Unlock()
	} else {
		if mysql.QueryOneByNameOrPass(nameStr, passwordStr) == false {
			tip(conn, "登录失败")
			return
		}
		if existOfName(nameStr) == true {
			tip(conn, "不可重复登录")
			return
		}
		lock.Lock()
		connUser[conn] = nameStr
		lock.Unlock()
	}

	//接收消息
	for {
		//超时时间
		outTime(conn)
		var buf string
		//阻塞，等待客户端通过conn发送信息，如果客户端没有conn.write[信息]，就会在这里一直阻塞
		buf, err = pre.ReadPacket(conn)
		//客户端正常断开连接，会读到EOF
		if err != nil {
			if err.Error() == "EOF" {
				fmt.Printf("%v正常关闭连接\n", conn.RemoteAddr().String())
			} else {
				fmt.Printf("%v异常断开连接:%v\n", conn.RemoteAddr().String(), err)
			}
			return
		}
		//去除消息的空白
		text := strings.TrimSpace(buf)

		if text == "/list" {
			// 查看在线用户
			SendUserList(conn)
			continue
		}
		if strings.HasPrefix(text, "@") {
			// 私聊格式 :@用户名 消息内容 --- parts[0]是用户名---parts[1]是消息
			parts := strings.SplitN(text[1:], " ", 2)
			//如果格式正确则SendPrivate，否则提醒正确的私聊格式
			if len(parts) == 2 && parts[1] != "" {
				SendPrivate(conn, nameStr, parts[0], parts[1])
			} else {
				err = pre.WritePacket(conn, []byte("[系统] 私聊格式: @用户名 消息内容\n"))
				if err != nil {
					fmt.Println(err)
					return
				}
			}
			continue
		}
		mysql.InsertOneNew(text, nameStr, "")
		var meaasge = "<" + nameStr + ">" + ":" + text
		ch <- meaasge
		fmt.Printf("%v\n", meaasge)
	}
}

// Radio 广播
func Radio(ch <-chan string) {
	//遍历管道，将消息发送给所有用户
	for message := range ch {
		lock.RLock()
		for conn := range connUser {
			err := pre.WritePacket(conn, []byte(message))
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		lock.RUnlock()
	}
}

// SendPrivate 私聊：向目标用户发送消息，
func SendPrivate(senderConn net.Conn, senderName, targetName, msg string) {
	var targetConn net.Conn
	lock.RLock()
	//获取接收方的连接
	for conn, name := range connUser {
		if name == targetName {
			targetConn = conn
			break
		}
	}
	lock.RUnlock()
	//接收方不存在
	if targetConn == nil {
		err := pre.WritePacket(senderConn, []byte("用户<"+targetName+"> 不在线或不存在"))
		if err != nil {
			fmt.Println(err)
			return
		}
		return
	}
	//不能给自己发私聊
	if targetConn == senderConn {
		err := pre.WritePacket(senderConn, []byte("不能给自己发私聊\n"))
		if err != nil {
			fmt.Println(err)
			return
		}
		return
	}
	//向接收方和发送方都写一份
	mysql.InsertOneNew(msg, senderName, targetName)
	err := pre.WritePacket(targetConn, []byte("[私聊] <"+senderName+"> 悄悄对你说: "+msg))
	if err != nil {
		fmt.Println(err)
		return
	}
	err = pre.WritePacket(senderConn, []byte("[私聊] 你对 <"+targetName+"> 说: "+msg))
	if err != nil {
		fmt.Println(err)
		return
	}
}

// SendUserList 发送在线用户列表给指定连接，把所有人的名字以逗号分隔发送
func SendUserList(conn net.Conn) {
	var users []string
	lock.RLock()
	for _, name := range connUser {
		users = append(users, name)
	}
	lock.RUnlock()
	err := pre.WritePacket(conn, []byte("[在线用户] "+strings.Join(users, ", ")))
	if err != nil {
		return
	}
}

// 这个名字是否存在
func existOfName(name string) (re bool) {
	lock.Lock()
	for _, v := range connUser {
		if name == v {
			re = true
		}
	}
	lock.Unlock()
	return
}
