package mysql

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

type User struct {
	Id       int
	Name     string
	Password string
}

type New struct {
	Id          int
	content     string
	createTime  string
	sendName    string
	receiveName string
}

// InitMysql 初始化连接池
func InitMysql() {
	//创建连接池
	str := "root:123456@tcp(127.0.0.1:3306)/sql_test"
	var err error
	db, err = sql.Open("mysql", str)
	if err != nil {
		fmt.Println(err)
		return
	}
	//主动ping创建一个连接
	err = db.Ping()
	if err != nil {
		fmt.Println(err)
		return
	}
	//最大连接数
	db.SetMaxIdleConns(200)
	//最大空闲连接数
	db.SetMaxOpenConns(10)
}

func CloseMysql() {
	err := db.Close()
	if err != nil {
		fmt.Println(err)
		return
	}
}
func InsertOne(name, password string) {
	//插入
	result, err := db.Exec("insert into user(name,password) values(?,?)", name, password)
	if err != nil {
		fmt.Println("insert err:", err)
	}
	//本条数据的id
	id, err := result.LastInsertId()
	if err != nil {
		fmt.Println("get id failed, err:", err)
	}
	fmt.Println("insert success, the id is ", id)
}

// QueryOneByNameOrPass 找到true。找不到false
func QueryOneByNameOrPass(name string, password string) bool {
	var user User
	var row *sql.Row
	if password == "" {
		row = db.QueryRow("select * from user where name=?", name)
	} else {
		row = db.QueryRow("select * from user where name=? and password=?", name, password)
	}
	err := row.Scan(&user.Id, &user.Name, &user.Password)
	if err != nil {
		fmt.Println("QueryOneByNameAndPass err:", err)
		return false
	}
	return true
}

// insertOneNew 找到true。找不到false
func InsertOneNew(message, sendName, receiveName string) {
	result, err := db.Exec("insert into news(content,create_time,send_name,receive_name) values(?,now(),?,?)", message, sendName, receiveName)
	if err != nil {
		fmt.Println("insert err:", err)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		fmt.Println("get id failed, err:", err)
		return
	}
	fmt.Println("insert success, the id is ", id)
}

// QueryAll 查所有用户
//func QueryAll() []User {
//	rows, err := db.Query("select * from user")
//	if err != nil {
//		fmt.Println("query err:", err)
//	}
//	defer func(rows *sql.Rows) {
//		err := rows.Close()
//		if err != nil {
//			fmt.Println("close err:", err)
//		}
//	}(rows)
//	//取数据
//	var users = make([]User, 0, 100)
//	for rows.Next() {
//		var user = User{}
//		err = rows.Scan(&user.Id, &user.Name, &user.Password)
//		if err != nil {
//			fmt.Println("scan err:", err)
//		}
//		users = append(users, user)
//	}
//	return users
//}
