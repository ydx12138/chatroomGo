package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
	driver "github.com/go-sql-driver/mysql"
)

// defaultDSN 是未配置 MYSQL_DSN 时使用的本机开发库连接。
const defaultDSN = "root:123456@tcp(127.0.0.1:3306)/sql_test?charset=utf8mb4&parseTime=True&loc=Local"

var (
	// ErrNameExists 把 MySQL 重复键错误转换成业务层能理解的错误。
	ErrNameExists = errors.New("name already exists")
	// ErrDBNotReady 表示数据库连接池尚未初始化。
	ErrDBNotReady = errors.New("database is not initialized")
)

var db *sql.DB

// LoginResult 描述登录校验结果，比 bool 更能区分失败原因。
type LoginResult int

const (
	LoginSuccess LoginResult = iota
	LoginUserNotFound
	LoginPasswordIncorrect
	LoginDBError
)

// InitMysql 初始化全局 MySQL 连接池。
// sql.Open 只创建连接池对象，Ping 才真正验证数据库可用。
func InitMysql() error {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	if err = db.Ping(); err != nil {
		_ = db.Close()
		db = nil
		return fmt.Errorf("ping mysql: %w", err)
	}

	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(20)
	return nil
}

// CloseMysql 关闭全局连接池，并把 db 置空避免重复使用。
func CloseMysql() error {
	if db == nil {
		return nil
	}
	err := db.Close()
	db = nil
	if err != nil {
		return fmt.Errorf("close mysql: %w", err)
	}
	return nil
}

// RegisterUser 插入新用户。
// 用户名和密码格式由 protocol 包校验，这里只处理数据库写入和重复名映射。
func RegisterUser(name, password string) error {
	if db == nil {
		return ErrDBNotReady
	}
	_, err := db.Exec("INSERT INTO `user`(`name`,`password`) VALUES(?,?)", name, password)
	if err == nil {
		return nil
	}
	if isDuplicateNameError(err) {
		return ErrNameExists
	}
	return fmt.Errorf("register user: %w", err)
}

// CheckLogin 查询用户密码，并转换为业务层登录结果。
func CheckLogin(name, password string) (LoginResult, error) {
	if db == nil {
		return LoginDBError, ErrDBNotReady
	}

	var storedPassword string
	err := db.QueryRow("SELECT `password` FROM `user` WHERE `name` = ?", name).Scan(&storedPassword)
	return evaluateLoginResult(storedPassword, password, err)
}

// SavePublicMessage 保存公聊消息，receive_name 使用空字符串。
func SavePublicMessage(sender, content string) error {
	return saveMessage(sender, "", content)
}

// SavePrivateMessage 保存私聊消息，receive_name 使用目标用户名。
func SavePrivateMessage(sender, receiver, content string) error {
	return saveMessage(sender, receiver, content)
}

// saveMessage 是公聊和私聊共用的落库函数。
func saveMessage(sender, receiver, content string) error {
	if db == nil {
		return ErrDBNotReady
	}
	_, err := db.Exec(
		"INSERT INTO `news`(`content`,`create_time`,`send_name`,`receive_name`) VALUES(?, NOW(), ?, ?)",
		content,
		sender,
		receiver,
	)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}

// evaluateLoginResult 把 sql 查询状态转换成 LoginResult。
func evaluateLoginResult(storedPassword, inputPassword string, queryErr error) (LoginResult, error) {
	switch {
	case queryErr == nil:
		if storedPassword != inputPassword {
			return LoginPasswordIncorrect, nil
		}
		return LoginSuccess, nil
	case errors.Is(queryErr, sql.ErrNoRows):
		return LoginUserNotFound, nil
	default:
		return LoginDBError, fmt.Errorf("query login: %w", queryErr)
	}
}

// isDuplicateNameError 判断错误链里是否包含 MySQL 1062 重复键错误。
func isDuplicateNameError(err error) bool {
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
