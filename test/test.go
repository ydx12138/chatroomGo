package main

import (
	"DEMO2/TCPIP/mysql"
	"errors"
	"fmt"
)

// main 是一个手工验证数据库 API 的小脚本。
// 它不参与服务端运行，只用于快速检查 mysql 包能否连接、建表、注册和登录。
func main() {
	// 初始化数据库连接。
	if err := mysql.InitMysql(); err != nil {
		// 初始化失败时打印错误并退出。
		fmt.Println("初始化数据库失败:", err)
		return
	}
	// 程序退出前关闭数据库连接。
	defer func() {
		// 如果关闭失败，打印错误。
		if err := mysql.CloseMysql(); err != nil {
			fmt.Println("关闭数据库失败:", err)
		}
	}()

	// 确保 user 和 news 表存在。
	if err := mysql.EnsureSchema(); err != nil {
		// 建表失败时打印错误并退出。
		fmt.Println("初始化表结构失败:", err)
		return
	}

	// name 是本次手工验证使用的测试用户名。
	name := "demoUser"
	// password 是本次手工验证使用的测试密码。
	password := "123456"
	// 尝试注册测试用户。
	if err := mysql.RegisterUser(name, password); err != nil && !errors.Is(err, mysql.ErrNameExists) {
		// 如果不是“用户已存在”，就说明注册发生了异常。
		fmt.Println("注册失败:", err)
		return
	}

	// 使用刚才的用户名和密码检查登录。
	result, err := mysql.CheckLogin(name, password)
	// 如果登录校验过程中发生数据库错误，打印并退出。
	if err != nil {
		fmt.Println("登录校验失败:", err)
		return
	}

	// 打印登录结果，LoginSuccess 对应成功。
	fmt.Println("登录结果:", result)
}
