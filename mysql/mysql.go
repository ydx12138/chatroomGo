package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
	driver "github.com/go-sql-driver/mysql"
)

// defaultDSN 是默认数据库连接字符串。
// 如果没有配置 MYSQL_DSN 环境变量，程序会使用这个连接本机 MySQL。
const defaultDSN = "root:123456@tcp(127.0.0.1:3306)/sql_test?charset=utf8mb4&parseTime=True&loc=Local"

var (
	// ErrNameExists 表示注册用户名已经存在。
	ErrNameExists = errors.New("name already exists")
	// ErrDBNotReady 表示数据库还没有初始化成功。
	ErrDBNotReady = errors.New("database is not initialized")
)

// db 是 mysql 包内部维护的全局数据库连接池。
// 外部包不能直接访问它，只能通过本文件提供的函数操作数据库。
var db *sql.DB

// LoginResult 表示一次登录检查的结果。
// 使用枚举比 bool 更清楚，因为登录失败有多种原因。
type LoginResult int

const (
	// LoginSuccess 表示用户名存在，并且密码正确。
	LoginSuccess LoginResult = iota
	// LoginUserNotFound 表示没有查到这个用户名。
	LoginUserNotFound
	// LoginPasswordIncorrect 表示用户名存在，但是密码不匹配。
	LoginPasswordIncorrect
	// LoginDBError 表示查询数据库时发生异常。
	LoginDBError
)

// InitMysql 初始化 MySQL 连接池。
// 服务端启动时必须先调用它，否则后续数据库操作都会返回 ErrDBNotReady。
func InitMysql() error {
	// 先读取环境变量里的连接字符串。
	dsn := os.Getenv("MYSQL_DSN")
	// 如果环境变量为空，就使用默认本机连接。
	if dsn == "" {
		dsn = defaultDSN
	}

	// err 用来接收 sql.Open 或 Ping 的错误。
	var err error
	// sql.Open 不会立刻建立真实连接，它主要创建连接池对象。
	db, err = sql.Open("mysql", dsn)
	// 如果驱动名或 DSN 格式错误，这里会返回错误。
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	// Ping 会真正验证数据库是否可连接。
	if err = db.Ping(); err != nil {
		// Ping 失败时先关闭连接池，避免留下不可用对象。
		_ = db.Close()
		// 把全局 db 置空，避免后续误用。
		db = nil
		// 包装错误返回给服务端。
		return fmt.Errorf("ping mysql: %w", err)
	}

	// 设置最大空闲连接数。
	db.SetMaxIdleConns(10)
	// 设置最大打开连接数。
	db.SetMaxOpenConns(20)
	// 返回 nil 表示数据库初始化成功。
	return nil
}

// CloseMysql 关闭数据库连接池。
// 服务端退出时调用它释放数据库资源。
func CloseMysql() error {
	// 如果 db 是 nil，说明没有初始化或已经关闭，直接返回。
	if db == nil {
		return nil
	}
	// 调用连接池关闭方法。
	err := db.Close()
	// 不管 Close 是否报错，都把全局 db 清空，避免重复关闭。
	db = nil
	// 如果关闭出错，把错误包装后返回。
	if err != nil {
		return fmt.Errorf("close mysql: %w", err)
	}
	// 返回 nil 表示关闭成功。
	return nil
}

// EnsureSchema 确保数据库表结构存在。
// 这里会自动创建 user 表和 news 表，并确保 user.name 有唯一约束。
//func EnsureSchema() error {
//	// 数据库未初始化时不能执行建表。
//	if db == nil {
//		return ErrDBNotReady
//	}
//
//	// statements 保存需要依次执行的建表 SQL。
//	statements := []string{
//		// user 表保存用户名和密码。
//		"CREATE TABLE IF NOT EXISTS `user` (" +
//			// id 是自增主键。
//			"`id` BIGINT PRIMARY KEY AUTO_INCREMENT," +
//			// name 最长 10 位，对应业务层用户名长度限制。
//			"`name` VARCHAR(10) NOT NULL," +
//			// password 最长 10 位，对应业务层密码长度限制。
//			"`password` VARCHAR(10) NOT NULL," +
//			// 唯一索引保证数据库层面不能重名注册。
//			"UNIQUE KEY `uk_user_name` (`name`)" +
//			")",
//		// news 表保存公聊和私聊消息。
//		"CREATE TABLE IF NOT EXISTS `news` (" +
//			// id 是自增主键。
//			"`id` BIGINT PRIMARY KEY AUTO_INCREMENT," +
//			// content 保存消息正文。
//			"`content` TEXT NOT NULL," +
//			// create_time 保存消息创建时间，默认当前时间。
//			"`create_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP," +
//			// send_name 保存发送者用户名。
//			"`send_name` VARCHAR(10) NOT NULL," +
//			// receive_name 为空表示公聊，非空表示私聊接收者。
//			"`receive_name` VARCHAR(10) NOT NULL DEFAULT ''" +
//			")",
//	}
//
//	// 依次执行每条建表语句。
//	for _, statement := range statements {
//		// Exec 用于执行不返回行数据的 SQL。
//		if _, err := db.Exec(statement); err != nil {
//			// 任意一条失败都返回错误，服务端启动应该停止。
//			return fmt.Errorf("ensure schema: %w", err)
//		}
//	}
//	// 全部执行成功。
//	return nil
//}

// RegisterUser 注册新用户。
// 只负责插入数据库，不负责用户名和密码格式校验，格式校验由 protocol 包完成。
func RegisterUser(name, password string) error {
	// 数据库未初始化时直接返回。
	if db == nil {
		return ErrDBNotReady
	}
	// 使用占位符插入，避免手动拼 SQL。
	_, err := db.Exec("INSERT INTO `user`(`name`,`password`) VALUES(?,?)", name, password)
	// err 为 nil 表示插入成功。
	if err == nil {
		return nil
	}
	// 如果是 MySQL 重复键错误，转换成业务错误 ErrNameExists。
	if isDuplicateNameError(err) {
		return ErrNameExists
	}
	// 其他数据库错误保留原始信息并包装返回。
	return fmt.Errorf("register user: %w", err)
}

// CheckLogin 校验登录账号密码。
// 返回 LoginResult 用于区分：成功、用户不存在、密码错误、数据库错误。
func CheckLogin(name, password string) (LoginResult, error) {
	// 数据库未初始化时返回数据库错误。
	if db == nil {
		return LoginDBError, ErrDBNotReady
	}

	// storedPassword 用来保存数据库里查出的密码。
	var storedPassword string
	// 根据用户名查询密码。
	err := db.QueryRow("SELECT `password` FROM `user` WHERE `name` = ?", name).Scan(&storedPassword)
	// 把查询结果转换成业务层登录结果。
	return evaluateLoginResult(storedPassword, password, err)
}

// SavePublicMessage 保存公聊消息。
// 公聊消息在数据库里用 receive_name = "" 表示。
func SavePublicMessage(sender, content string) error {
	// 调用共用保存函数，接收者传空字符串。
	return saveMessage(sender, "", content)
}

// SavePrivateMessage 保存私聊消息。
// 私聊消息在数据库里会保存明确的 receive_name。
func SavePrivateMessage(sender, receiver, content string) error {
	// 调用共用保存函数，接收者传目标用户名。
	return saveMessage(sender, receiver, content)
}

// saveMessage 是公聊和私聊共用的消息保存函数。
// 它只负责落库，不负责消息内容校验。
func saveMessage(sender, receiver, content string) error {
	// 数据库未初始化时直接返回。
	if db == nil {
		return ErrDBNotReady
	}
	// 插入消息记录。
	_, err := db.Exec(
		// create_time 使用数据库的 NOW() 生成。
		"INSERT INTO `news`(`content`,`create_time`,`send_name`,`receive_name`) VALUES(?, NOW(), ?, ?)",
		// 第一个占位符：消息内容。
		content,
		// 第二个占位符：发送者。
		sender,
		// 第三个占位符：接收者，公聊时为空。
		receiver,
	)
	// 插入失败时返回包装错误。
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	// 返回 nil 表示保存成功。
	return nil
}

// evaluateLoginResult 把数据库查询结果转换成 LoginResult。
// 单独拆出来是为了让这段逻辑可以不用真实数据库也能单元测试。
func evaluateLoginResult(storedPassword, inputPassword string, queryErr error) (LoginResult, error) {
	// 根据查询错误状态判断登录结果。
	switch {
	case queryErr == nil:
		// 查询成功后，再比较数据库密码和用户输入密码。
		if storedPassword != inputPassword {
			return LoginPasswordIncorrect, nil
		}
		// 密码一致表示登录成功。
		return LoginSuccess, nil
	case errors.Is(queryErr, sql.ErrNoRows):
		// sql.ErrNoRows 表示用户名不存在。
		return LoginUserNotFound, nil
	default:
		// 其他错误表示数据库异常。
		return LoginDBError, fmt.Errorf("query login: %w", queryErr)
	}
}

// isDuplicateNameError 判断错误是否为 MySQL 重复键错误。
// MySQL 的重复唯一索引错误码是 1062。
func isDuplicateNameError(err error) bool {
	// mysqlErr 用来接收被 errors.As 提取出来的 MySQL 错误。
	var mysqlErr *driver.MySQLError
	// errors.As 可以从包装错误链中找到指定类型。
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
