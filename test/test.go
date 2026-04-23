package main

import (
	"DEMO2/TCPIP/mysql"
	"fmt"
)

func main() {
	mysql.InitMysql()
	mysql.InsertOne("光头强", "123456")

	re := mysql.QueryOneByNameOrPass("光头强", "")
	fmt.Println(re)
	//re = mysql.QueryOneByNameOrPass("光头强", "123456")
	//fmt.Println(re)
	//all := mysql.QueryAll()
	//fmt.Println(all[0].Name)
}
