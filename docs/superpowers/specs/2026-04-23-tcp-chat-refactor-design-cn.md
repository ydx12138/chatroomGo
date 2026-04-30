# TCP 聊天室简化设计说明

日期：2026-04-30

## 目标

在功能不变的前提下，把聊天室代码尽量简化，让代码结构更直接、注释更有用、旧的死代码更少。

保留的功能：

- 登录、注册、退出主菜单
- 公聊
- 进入私聊、发送私聊、退出私聊
- `/list` 查看在线用户
- 服务端控制台 `/exit` 关服并通知客户端
- MySQL 保存用户和聊天消息
- `pre.ReadPacket` / `pre.WritePacket` 处理 TCP 粘包拆包
- 原有文本协议格式

## 当前目录职责

### `pre`

只负责 TCP 分包：

- 写包时先写 4 字节长度头，再写消息体。
- 读包时先读长度头，再完整读消息体。

它不关心登录、聊天、数据库或命令类型。

### `protocol`

负责协议常量、输入校验、协议组包和协议解析。

主要命令包括：

- `LOGIN`
- `REGISTER`
- `PUBLIC`
- `ENTER`
- `PRIVATE`
- `ACK`
- `LIST`
- `QUIT`
- `SHUTDOWN`

聊天正文允许包含 `|`，所以带正文的协议包使用 `SplitN` 解析，避免误切正文。

### `mysql`

负责全部数据库访问。

当前对外函数：

- `InitMysql() error`
- `CloseMysql() error`
- `RegisterUser(name, password string) error`
- `CheckLogin(name, password string) (LoginResult, error)`
- `SavePublicMessage(sender, content string) error`
- `SavePrivateMessage(sender, receiver, content string) error`

当前代码不再自动建表。服务端启动时假设 `user` 和 `news` 表已经存在。

### `client`

负责终端交互和客户端本地聊天状态。

客户端只保存本地状态：

- 当前用户名
- 当前私聊对象
- 是否正在等待服务端确认进入私聊

简化后不再需要单独的 `ChatMode` 枚举。`privateTarget == ""` 表示公聊；`privateTarget != ""` 表示私聊。

### `server`

负责连接生命周期、认证、在线用户表、消息路由和关服。

服务端维护：

- `peersByConn`：所有连接，包括未登录连接
- `peersByName`：已登录用户
- 一个全局锁保护连接表和在线表
- 每个连接一个写锁，防止并发写同一个 TCP 连接
- `shutdownCh` 和 `sync.Once` 控制关服流程只执行一次

## 函数顺序清单

下面只按源码从上到下列出函数，方便读代码时快速对照。

### `client/client.go`

1. `main()`：客户端入口，连接菜单和聊天流程。
2. `serverAddr() string`：返回要连接的服务端地址。
3. `runMainMenu(reader *bufio.Reader) (string, net.Conn, error)`：显示主菜单，让用户选择登录、注册或退出。
4. `doAuthFlow(reader *bufio.Reader, action string) (string, net.Conn, error)`：执行一次登录或注册流程，并和服务端交换结果。
5. `promptCredentials(reader *bufio.Reader) (string, string, error)`：读取用户输入的用户名和密码。
6. `runChat(conn net.Conn, reader *bufio.Reader, session *clientSession)`：进入聊天界面，同时处理键盘输入和服务端消息。
7. `handleChatInput(conn net.Conn, session *clientSession, line string) (bool, error)`：处理用户在聊天里输入的一行内容。
8. `readChatInput(reader *bufio.Reader, inputCh chan<- string)`：持续读取键盘输入，并交给聊天主循环处理。
9. `receiveLoop(conn net.Conn, privateEnterCh chan<- privateEnterResult, doneCh chan<- struct{})`：持续接收服务端消息，并把需要主循环处理的结果送出去。
10. `sendPayload(conn net.Conn, payload string) error`：把一条协议消息发给服务端。
11. `readLine(reader *bufio.Reader, prompt string) (string, error)`：打印提示语并读取一行输入。
12. `printMainMenu()`：打印登录前的主菜单。
13. `printChatGuide()`：打印聊天界面的可用命令提示。
14. `translateAuthError(code string) string`：把登录、注册错误码转成用户能看懂的提示。
15. `translatePrivateEnterError(code string) string`：把进入私聊失败的错误码转成提示。
16. `renderServerPacket(packet protocol.Packet) (string, bool)`：把服务端消息转成要显示在终端上的文字。
17. `applyPrivateEnterResult(session *clientSession, result privateEnterResult) string`：根据服务端确认结果，更新本地私聊状态。

### `server/server.go`

1. `main()`：服务端入口，准备数据库、监听端口并启动服务。
2. `newChatServer(listener net.Listener) *chatServer`：创建服务端运行时需要的连接表、用户表和关服信号。
3. `serve(server *chatServer) error`：不断接收新客户端，并为每个客户端启动单独的处理 goroutine。
4. `handleConnection(server *chatServer, peer *clientConn)`：负责一个客户端连接的完整处理过程。
5. `handleGuestCommand(server *chatServer, peer *clientConn, cmd protocol.Packet) bool`：处理还没登录的客户端命令。
6. `handleAuthedCommand(server *chatServer, peer *clientConn, cmd protocol.Packet) bool`：处理已经登录用户的聊天命令。
7. `handleRegister(peer *clientConn, cmd protocol.Packet)`：检查注册信息并写入数据库。
8. `handleLogin(server *chatServer, peer *clientConn, cmd protocol.Packet) bool`：检查账号密码，成功后把用户标记为在线。
9. `handlePublicMessage(server *chatServer, peer *clientConn, cmd protocol.Packet)`：保存公聊消息，并发给所有在线用户。
10. `handlePrivateEnter(server *chatServer, peer *clientConn, cmd protocol.Packet)`：检查目标用户是否可以进入私聊。
11. `handlePrivateMessage(server *chatServer, peer *clientConn, cmd protocol.Packet)`：保存私聊消息，并发给对方和自己。
12. `handleUserList(server *chatServer, peer *clientConn)`：把当前在线用户名列表发给客户端。
13. `addPeer(server *chatServer, peer *clientConn)`：把新连接记录到连接表里。
14. `disconnectPeer(server *chatServer, peer *clientConn)`：清理断开的连接，并从在线表里移除用户。
15. `sendPacket(peer *clientConn, payload string) error`：给某个客户端发送一条完整协议消息。
16. `sendErr(peer *clientConn, code string) error`：给客户端发送一条 `ERR` 错误码消息。
17. `snapshotOnlinePeers(server *chatServer) []*clientConn`：复制当前在线连接列表，方便后面群发消息。
18. `snapshotOnlineUsernames(server *chatServer) []string`：复制并排序当前在线用户名。
19. `snapshotOnlineUserSet(server *chatServer) map[string]struct{}`：整理在线用户名集合，方便快速判断某人是否在线。
20. `watchConsoleExit(server *chatServer)`：监听服务端控制台输入的 `/exit`。
21. `shutdownServer(server *chatServer, message string)`：通知所有客户端并关闭连接和监听端口。
22. `snapshotAllPeers(server *chatServer) []*clientConn`：复制所有连接，包括还没登录的连接。
23. `isShuttingDown(server *chatServer) bool`：判断服务端是否已经开始关服。
24. `mapLoginResultToCode(result mysql.LoginResult) string`：把数据库登录结果转成客户端错误码。
25. `canEnterPrivateMode(sender, target string, online map[string]struct{}) string`：检查私聊目标是否有效。

## 用户行为保持不变

### 登录前

客户端显示主菜单：

```text
1. 登录
2. 注册
3. 退出
```

登录或注册失败后回到主菜单。

注册成功后不会自动登录，用户需要回到主菜单重新选择登录。

### 公聊状态

- 直接输入文字：发送公聊消息。
- `/chat <用户名>`：请求进入和该用户的私聊。
- `/list`：查看在线用户。
- `/exit`：退出客户端。

### 私聊状态

- 直接输入文字：发送给当前私聊对象。
- `/list`：仍然可以查看在线用户。
- `/exit`：退出私聊，回到公聊。
- `/chat <用户名>`：本地拒绝，不允许在私聊中再次进入私聊。

### 服务端关服

服务端控制台输入 `/exit` 后：

1. 标记服务端正在关闭。
2. 给所有连接发送 `SHUTDOWN|<提示消息>`。
3. 关闭客户端连接。
4. 关闭监听器。
5. 让连接协程自然退出，不把主动关服产生的网络错误当异常打印。

## 协议格式

客户端发给服务端：

```text
LOGIN|<用户名>|<密码>
REGISTER|<用户名>|<密码>
PUBLIC|<内容>
ENTER|<目标用户名>
PRIVATE|<目标用户名>|<内容>
LIST
QUIT
```

服务端发给客户端：

```text
OK|LOGIN
OK|REGISTER
ERR|NAME_EXISTS
ERR|USER_NOT_FOUND
ERR|PASSWORD_INCORRECT
ERR|ALREADY_ONLINE
ERR|INVALID_USERNAME
ERR|INVALID_PASSWORD
ERR|DB_ERROR
SYSTEM|<提示消息>
PUBLIC|<发送者>|<内容>
PRIVATE|<发送者>|<内容>
ACK|<目标用户名>|<内容>
ENTEROK|<目标用户名>
ENTERERR|<错误码>
LIST|<逗号分隔的在线用户>
SHUTDOWN|<提示消息>
```

## 本次简化点

- 删除客户端 `ChatMode` 枚举，用 `privateTarget` 是否为空判断公聊/私聊。
- 删除客户端输入解析结构体，把简单输入分支直接放回 `handleChatInput`。
- 删除只包装固定字符串的协议错误码常量。
- 服务端新增 `sendErr`，减少重复的 `ERR|code` 组包代码。
- 客户端错误码翻译从长 `switch` 改成小 map。
- 私聊相关协议前缀简化为 `ENTER`、`ENTEROK`、`ENTERERR`、`ACK`。
- 删除注释掉的旧实现和未使用 helper。
- 注释改为函数级和复杂逻辑说明，避免逐行重复代码含义。

## 并发规则

服务端不能直接写 `net.Conn`。

所有发送都走 `sendPacket`，由它持有当前连接的 `writeMu` 后再调用 `pre.WritePacket`。这样可以避免多个 goroutine 同时写同一个连接时，把长度头和消息体写乱。

广播和关服前先取连接快照。这样网络写入时不需要持有全局在线表锁，避免一个慢连接阻塞其他用户登录、退出或查询列表。

## 验证方式

在 `D:\go代码\DEMO2\TCPIP` 下执行：

```powershell
$env:GOCACHE='D:\codex_config\memories\go-build-cache'; go test ./...
```

期望结果：

```text
?    DEMO2/TCPIP/client    [no test files]
?    DEMO2/TCPIP/mysql     [no test files]
?    DEMO2/TCPIP/pre       [no test files]
?    DEMO2/TCPIP/protocol  [no test files]
?    DEMO2/TCPIP/server    [no test files]
```

还需要手动冒烟验证：

- 注册成功
- 重复注册失败
- 密码错误登录失败
- 登录成功进入聊天
- 公聊广播
- `/list`
- 进入私聊、发送私聊、退出私聊
- 服务端 `/exit` 通知客户端并关闭
