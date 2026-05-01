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

简化后不再需要单独的 `ChatMode` 枚举。`privateTarget == ""` 表示公聊；`privateTarget != ""` 表示私聊。

### `server`

负责连接生命周期、认证、在线用户表、消息路由和关服。

服务端维护：

- `peersByConn`：所有连接，包括未登录连接
- `peersByName`：已登录用户
- 一个全局锁保护连接表和在线表
- 每个连接一个写锁，防止并发写同一个 TCP 连接
- `shutdownCh` 表示服务端已经开始关服

## 结构体字段说明

### `protocol.Packet`

`Packet` 是协议解析后的统一结果。不是每种消息都会用到所有字段。

- `Cmd`：消息命令，比如 `LOGIN`、`PUBLIC`、`PRIVATE`。
- `Username`：登录或注册时的用户名。
- `Password`：登录或注册时的密码。
- `Target`：消息目标。公聊和私聊里表示发送者或接收者，进入私聊时表示目标用户。
- `Content`：聊天正文，或部分命令里携带的普通内容。
- `Code`：错误码，比如 `NAME_EXISTS`、`TARGET_NOT_FOUND`。
- `Message`：系统提示文字，比如关服通知或普通系统消息。

### `client.clientSession`

`clientSession` 只保存客户端自己需要记住的聊天状态。

- `username`：当前登录成功的用户名。
- `privateTarget`：当前正在私聊的目标用户；为空表示现在是公聊。

### `client.privateEnterResult`

`privateEnterResult` 用来把接收消息 goroutine 里的私聊进入结果交回聊天主循环。

- `ok`：是否成功进入私聊。
- `target`：成功进入私聊时的目标用户名。
- `code`：进入私聊失败时的错误码。

### `server.clientConn`

`clientConn` 记录服务端眼里的一个客户端连接。

- `conn`：底层 TCP 连接，用来读写网络消息。
- `username`：这个连接登录成功后的用户名；没登录时为空。
- `writeMu`：这个连接自己的写锁，保证同一时刻只有一个 goroutine 往它写消息。

### `server.chatServer`

`chatServer` 保存服务端运行时共享的数据。

- `listener`：服务端监听端口的对象，用来接收新客户端连接。
- `peersByConn`：所有客户端连接，包括还没登录的连接。
- `peersByName`：已经登录成功的用户，按用户名查连接。
- `mu`：保护 `peersByConn` 和 `peersByName` 的锁。
- `shutdownCh`：关服信号，关闭后表示服务端已经开始关服。
- `shutdownWg`：等待所有连接处理 goroutine 退出。

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

## 主要流程说明

### 客户端启动和主菜单

1. `main` 创建终端输入 reader。
2. `main` 调用 `runMainMenu`。
3. `runMainMenu` 打印主菜单，让用户选择登录、注册或退出。
4. 用户选择退出时，`runMainMenu` 返回 `errClientExit`，`main` 认为这是正常退出。
5. 用户登录成功时，`runMainMenu` 返回用户名和保持打开的 TCP 连接。
6. `main` 创建 `clientSession`，然后调用 `runChat` 进入聊天。

### 注册流程

1. 用户在主菜单选择注册。
2. `runMainMenu` 调用 `doAuthFlow(reader, protocol.CmdRegister)`。
3. `doAuthFlow` 调用 `promptCredentials` 读取用户名和密码，并先在客户端做一次格式检查。
4. `doAuthFlow` 连接服务端，并通过 `sendPayload` 发送 `REGISTER|<用户名>|<密码>`。
5. 服务端的 `serve` 接收连接后，开 goroutine 调用 `handleConnection`。
6. `handleConnection` 读取 TCP 包，用 `protocol.ParseClientPacket` 解析出 `REGISTER`。
7. 因为连接还没登录，`handleConnection` 把命令交给 `handleGuestCommand`。
8. `handleGuestCommand` 调用 `handleRegister`。
9. `handleRegister` 再次检查用户名和密码，然后调用 `mysql.RegisterUser` 写入数据库。
10. 注册成功时，服务端返回 `OK|REGISTER`；用户名重复时返回 `ERR|NAME_EXISTS`；数据库错误时返回 `ERR|DB_ERROR`。
11. 客户端 `doAuthFlow` 收到结果后，成功则提示注册成功并回到主菜单；失败则用 `translateAuthError` 转成提示文字。
12. 注册成功不会自动登录，用户需要回到主菜单再选择登录。

### 登录流程

1. 用户在主菜单选择登录。
2. `runMainMenu` 调用 `doAuthFlow(reader, protocol.CmdLogin)`。
3. `doAuthFlow` 读取用户名和密码，连接服务端，发送 `LOGIN|<用户名>|<密码>`。
4. 服务端 `handleConnection` 读取并解析出 `LOGIN`。
5. 未登录连接的命令会交给 `handleGuestCommand`。
6. `handleGuestCommand` 调用 `handleLogin`。
7. `handleLogin` 检查用户名和密码格式，然后调用 `mysql.CheckLogin`。
8. 数据库返回用户不存在、密码错误或数据库错误时，`handleLogin` 用 `sendErr` 返回对应错误码。
9. 登录成功前，`handleLogin` 会检查 `peersByName`，防止同一个用户名重复在线。
10. 登录成功后，服务端把 `peer.username` 设置为用户名，并把连接放进 `peersByName`。
11. 服务端返回 `OK|LOGIN`。
12. 客户端收到 `OK|LOGIN` 后，打印聊天命令说明，并把连接交给 `runChat` 继续使用。

### 聊天主循环

1. `runChat` 创建三个 channel：输入 channel、私聊进入结果 channel、连接结束 channel。
2. `runChat` 启动 `readChatInput` goroutine，专门读取键盘输入。
3. `runChat` 启动 `receiveLoop` goroutine，专门接收服务端消息。
4. `runChat` 自己留在主循环里，统一处理用户输入、私聊进入结果和连接关闭。
5. 用户输入会交给 `handleChatInput`。
6. 服务端返回 `ENTEROK` 或 `ENTERERR` 时，`receiveLoop` 会把结果交给 `runChat`。
7. `runChat` 调用 `applyPrivateEnterResult` 更新本地私聊对象。
8. 普通服务端消息由 `receiveLoop` 调用 `renderServerPacket` 转成终端显示文字。

### 公聊消息流程

1. 用户在公聊状态下直接输入普通文字。
2. `handleChatInput` 发现 `privateTarget == ""`，把文字组装成 `PUBLIC|<内容>`。
3. 客户端通过 `sendPayload` 发给服务端。
4. 服务端 `handleConnection` 读取消息并解析成 `PUBLIC`。
5. 已登录用户的命令会交给 `handleAuthedCommand`。
6. `handleAuthedCommand` 调用 `handlePublicMessage`。
7. `handlePublicMessage` 检查消息不能为空，然后调用 `mysql.SavePublicMessage` 保存公聊消息。
8. 保存成功后，服务端用 `snapshotOnlinePeers` 复制当前在线连接列表。
9. 服务端给每个在线用户发送 `PUBLIC|<发送者>|<内容>`。
10. 客户端 `receiveLoop` 收到 `PUBLIC` 后，`renderServerPacket` 把它显示成公聊消息。

### 进入私聊流程

1. 用户在公聊状态下输入 `/chat <用户名>`。
2. `handleChatInput` 检查当前不在私聊状态，并检查目标用户名格式。
3. 客户端发送 `ENTER|<目标用户名>`。
4. 服务端 `handleConnection` 解析出 `ENTER`。
5. `handleAuthedCommand` 调用 `handlePrivateEnter`。
6. `handlePrivateEnter` 检查目标用户名格式。
7. `handlePrivateEnter` 用 `snapshotOnlineUserSet` 复制在线用户名集合。
8. `canEnterPrivateMode` 检查目标是不是自己、目标是否在线。
9. 可以进入时，服务端返回 `ENTEROK|<目标用户名>`。
10. 不能进入时，服务端返回 `ENTERERR|<错误码>`。
11. 客户端 `receiveLoop` 收到 `ENTEROK` 或 `ENTERERR` 后，把结果发给 `runChat`。
12. `runChat` 调用 `applyPrivateEnterResult`。成功时设置 `privateTarget`，失败时清空 `privateTarget` 并显示错误提示。

### 私聊消息流程

1. 客户端进入私聊后，`privateTarget` 保存当前私聊对象。
2. 用户直接输入普通文字。
3. `handleChatInput` 发现 `privateTarget != ""`，发送 `PRIVATE|<目标用户名>|<内容>`。
4. 服务端 `handleConnection` 解析出 `PRIVATE`。
5. `handleAuthedCommand` 调用 `handlePrivateMessage`。
6. `handlePrivateMessage` 检查目标用户名和消息正文。
7. 服务端从 `peersByName` 查找目标用户连接。
8. 目标不在线时，服务端给发送者返回系统提示。
9. 目标是自己时，服务端给发送者返回系统提示。
10. 检查通过后，服务端调用 `mysql.SavePrivateMessage` 保存私聊消息。
11. 保存成功后，服务端给目标用户发送 `PRIVATE|<发送者>|<内容>`。
12. 服务端给发送者发送 `ACK|<目标用户名>|<内容>`，让发送者也看到自己发出的私聊内容。
13. 客户端 `receiveLoop` 收到 `PRIVATE` 或 `ACK` 后，通过 `renderServerPacket` 显示出来。

### 查看在线用户流程

1. 用户输入 `/list`。
2. `handleChatInput` 发送 `LIST`。
3. 服务端 `handleAuthedCommand` 调用 `handleUserList`。
4. `handleUserList` 调用 `snapshotOnlineUsernames` 复制并排序在线用户名。
5. 服务端返回 `LIST|<逗号分隔的用户名>`。
6. 客户端 `renderServerPacket` 把它显示成在线用户列表。

### 退出私聊和退出客户端

1. 用户在私聊状态输入 `/exit`。
2. `handleChatInput` 只清空本地 `privateTarget`，不通知服务端。
3. 用户回到公聊状态。
4. 用户在公聊状态输入 `/exit`。
5. `handleChatInput` 发送 `QUIT` 给服务端，并告诉 `runChat` 退出。
6. 服务端 `handleAuthedCommand` 收到 `QUIT` 后返回 `true`。
7. `handleConnection` 退出，defer 调用 `disconnectPeer` 清理连接和在线表。

### 服务端收到消息后的通用处理

1. 每个客户端连接都会由一个 `handleConnection` goroutine 处理。
2. `handleConnection` 用 `pre.ReadPacket` 读取完整 TCP 包。
3. 读取失败时，如果不是关服造成的正常断开，就打印连接断开信息。
4. 读取成功后，`handleConnection` 用 `protocol.ParseClientPacket` 解析命令。
5. 解析失败时，服务端返回 `SYSTEM|无效命令`。
6. 如果 `peer.username == ""`，说明还没登录，只能走 `handleGuestCommand`。
7. 如果 `peer.username != ""`，说明已经登录，走 `handleAuthedCommand`。
8. 连接处理结束时，`disconnectPeer` 会从 `peersByConn` 和 `peersByName` 里清理该连接。

### 服务端关服流程

1. 服务端控制台输入 `/exit`。
2. `watchConsoleExit` 调用 `shutdownServer`。
3. `shutdownServer` 关闭 `shutdownCh`，表示服务端开始关服。
4. `shutdownServer` 用 `snapshotAllPeers` 复制所有连接，包括未登录连接。
5. 服务端给每个连接发送 `SHUTDOWN|<提示消息>`。
6. 服务端关闭每个客户端连接。
7. 服务端关闭 listener，`serve` 里的 `Accept` 会返回错误。
8. `serve` 发现 `isShuttingDown(server)` 为 true，于是正常返回。
9. `main` 等待 `shutdownWg`，让连接处理 goroutine 自然退出。

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
