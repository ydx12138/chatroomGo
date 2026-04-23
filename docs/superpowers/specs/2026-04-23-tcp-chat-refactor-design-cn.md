# TCP 聊天室重构详细设计文档

日期：2026-04-23

## 1. 项目目标

本次重构的目标是把原有的 Go TCP 聊天室项目整理成一个职责更清晰、耦合更低、交互更稳定的版本，重点解决以下问题：

1. 客户端启动后必须先进入主菜单，而不是直接输入用户名和密码。
2. 登录、注册失败时要有明确提示，并且统一回到主菜单重新选择。
3. 私聊从“单条消息语法”改为“显式私聊状态”，让公聊和私聊的行为更清晰。
4. 所有消息收发仍然使用 `pre` 包中的粘包处理方法。
5. 所有 MySQL 操作集中在 `mysql` 包中，不允许业务层直接拼 SQL。
6. 服务端支持在控制台输入 `/exit` 后主动关闭，并通知所有客户端自动退出。

## 2. 重构后的目录职责

### 2.1 `client`

负责客户端交互逻辑：

- 主菜单显示
- 登录和注册流程
- 公聊/私聊状态切换
- 用户输入处理
- 服务端消息显示
- 服务端关闭后的自动退出

### 2.2 `server`

负责服务端会话和路由逻辑：

- 监听 TCP 连接
- 处理登录和注册
- 维护在线用户表
- 处理公聊、私聊、在线列表
- 统一给连接发包
- 监听控制台 `/exit` 并优雅关闭

### 2.3 `mysql`

负责全部数据库操作：

- 初始化数据库连接
- 建表和唯一索引
- 注册用户
- 登录校验
- 保存公聊消息
- 保存私聊消息

### 2.4 `pre`

只负责粘包拆包：

- `WritePacket`
- `ReadPacket`

业务层不直接处理长度头。

### 2.5 `protocol`

这是本次重构新增的协议辅助层，负责：

- 用户名、密码、消息内容校验
- 客户端输入解析
- 客户端与服务端之间的文本协议格式化
- 客户端与服务端之间的文本协议解析

这样可以避免客户端和服务端各自维护一套字符串常量，降低重复和出错概率。

## 3. 核心设计

## 3.1 客户端状态设计

客户端包含两个大状态：

1. 认证前状态
2. 登录后聊天状态

认证前状态下，客户端循环显示主菜单：

- `1. 登录`
- `2. 注册`
- `3. 退出`

登录后聊天状态下，客户端又分成两个子状态：

- 公聊状态
- 私聊状态

### 公聊状态行为

- 直接输入文本：发送公聊消息
- 输入 `/chat 用户名`：申请进入与该用户的私聊
- 输入 `/list`：查看在线用户
- 输入 `/exit`：退出客户端

### 私聊状态行为

- 直接输入文本：默认发送给当前私聊目标
- 输入 `/list`：查看在线用户
- 输入 `/exit`：退出私聊，回到公聊
- 再次输入 `/chat 用户名`：不允许，客户端本地直接提示

## 3.2 服务端状态设计

服务端对每个连接分成两个阶段处理：

1. 未认证阶段
2. 已认证阶段

### 未认证阶段

只允许两类请求：

- 登录
- 注册

如果客户端在未登录前发送聊天类命令，服务端会返回系统提示，要求先登录或注册。

### 已认证阶段

允许处理以下请求：

- 公聊消息
- 进入私聊请求
- 私聊消息
- 在线用户列表
- 会话退出

## 3.3 连接写入安全设计

原项目里存在一个重要风险：同一个连接可能被多个 goroutine 同时写入，导致消息头和消息体交叉，虽然 `pre` 包解决了粘包问题，但无法解决“并发写一个连接”的问题。

本次重构后，服务端给每个连接维护一个单独的写锁：

- 每次写连接前先加锁
- 写完后释放锁
- 所有写操作统一走一个发送函数

这样可以保证一个连接上的数据包永远按完整顺序写出。

## 4. 协议设计

## 4.1 协议层说明

底层仍然使用 `pre.WritePacket` 和 `pre.ReadPacket` 发送完整字符串包。

字符串包内部采用 `|` 分隔字段，例如：

```text
LOGIN|alice|123456
PUBLIC|hello world
PRIVATE|bob|你好
```

协议解析统一放在 `protocol` 包中。

## 4.2 客户端到服务端的协议

### 认证类

- `LOGIN|用户名|密码`
- `REGISTER|用户名|密码`

### 聊天类

- `PUBLIC|消息内容`
- `PRIVATE_ENTER|目标用户名`
- `PRIVATE|目标用户名|消息内容`

### 用户列表

- `LIST`

### 退出会话

- `QUIT`

## 4.3 服务端到客户端的协议

### 认证响应

- `OK|LOGIN` 登陆成功
- `OK|REGISTER` 注册成功
- `ERR|NAME_EXISTS` 名字已经存在
- `ERR|USER_NOT_FOUND` 用户不存在
- `ERR|PASSWORD_INCORRECT` 密码错误
- `ERR|ALREADY_ONLINE` 已经在线
- `ERR|INVALID_USERNAME` 用户名无效
- `ERR|INVALID_PASSWORD` 密码无效
- `ERR|DB_ERROR` 数据库错误

### 系统消息

- `SYSTEM|提示内容`

### 聊天消息

- `PUBLIC|发送者|内容`
- `PRIVATE|发送者|内容`
- `PRIVATE_ACK|目标用户|内容`
- `PRIVATE_ENTER_OK|目标用户`
- `PRIVATE_ENTER_ERR|错误码`

### 在线列表

- `LIST|alice,bob,lucy`

### 服务端关闭

- `SHUTDOWN|服务器已关闭`

## 5. 数据库设计

## 5.1 用户表

用户表保存账号信息，并通过唯一索引保证不能重名。

```sql
CREATE TABLE IF NOT EXISTS `user` (
    `id` BIGINT PRIMARY KEY AUTO_INCREMENT,
    `name` VARCHAR(10) NOT NULL,
    `password` VARCHAR(10) NOT NULL,
    UNIQUE KEY `uk_user_name` (`name`)
);
```

## 5.2 消息表

消息表保存公聊和私聊记录：

- `receive_name = ''` 表示公聊
- `receive_name != ''` 表示私聊

```sql
CREATE TABLE IF NOT EXISTS `news` (
    `id` BIGINT PRIMARY KEY AUTO_INCREMENT,
    `content` TEXT NOT NULL,
    `create_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `send_name` VARCHAR(10) NOT NULL,
    `receive_name` VARCHAR(10) NOT NULL DEFAULT ''
);
```

## 5.3 数据库访问函数

MySQL 包对外提供以下接口：

- `InitMysql() error`
- `CloseMysql() error`
- `EnsureSchema() error`
- `RegisterUser(name, password string) error`
- `CheckLogin(name, password string) (LoginResult, error)`
- `SavePublicMessage(sender, content string) error`
- `SavePrivateMessage(sender, receiver, content string) error`

业务层只调用这些函数，不再出现散落在服务端中的 SQL 语句。

## 6. 输入校验设计

用户名规则：

- 长度 1 到 10
- 不能包含空格
- 不能包含 `|`

密码规则：

- 长度 6 到 10
- 不能包含空格
- 不能包含 `|`

消息规则：

- 不能是空消息

说明：

之所以额外禁止 `|`，是因为它被协议层当作字段分隔符使用。如果允许用户名或密码带 `|`，会导致协议解析歧义。

## 7. 使用流程

## 7.1 服务端启动流程

1. 启动服务端程序。
2. 初始化 MySQL 连接。
3. 自动检查并创建所需表结构与唯一索引。
4. 监听 `0.0.0.0:8888`。
5. 启动控制台命令监听。
6. 等待客户端连接。

## 7.2 客户端启动流程

1. 启动客户端程序。
2. 连接服务端。
3. 进入主菜单。
4. 选择登录、注册或退出。

## 7.3 注册流程

1. 用户在主菜单选择 `2. 注册`。
2. 客户端输入用户名和密码。
3. 客户端先做本地校验。
4. 校验通过后向服务端发送注册请求。
5. 服务端再次校验，并调用 `mysql.RegisterUser`。
6. 若注册成功，客户端提示“注册成功，请返回主菜单登录”。
7. 无论成功还是失败，流程都回到主菜单。

## 7.4 登录流程

1. 用户在主菜单选择 `1. 登录`。
2. 客户端输入用户名和密码。
3. 客户端先做本地校验。
4. 校验通过后向服务端发送登录请求。
5. 服务端调用 `mysql.CheckLogin` 校验账号和密码。
6. 服务端额外检查该用户是否已经在线。
7. 登录成功后进入聊天状态。
8. 登录失败后提示原因，并回到主菜单。

## 7.5 公聊流程

1. 用户登录成功后，默认处于公聊状态。
2. 直接输入内容，客户端发送公聊请求。
3. 服务端保存消息到数据库。
4. 服务端广播给所有在线用户。
5. 客户端收到后显示为：

```text
[公聊] 用户名: 内容
```

## 7.6 进入私聊流程

1. 在公聊状态输入 `/chat 用户名`。
2. 客户端向服务端发送进入私聊请求。
3. 服务端检查：
   - 目标用户是否在线
   - 是否试图和自己私聊
4. 通过后返回私聊成功响应。
5. 客户端将当前模式切换为私聊，并记录私聊目标。
6. 后续普通输入都默认发给该目标用户。

## 7.7 私聊消息流程

1. 客户端处于私聊状态时，直接输入文字。
2. 客户端把消息封装成私聊请求并发送到服务端。
3. 服务端只把消息投递给目标用户，同时给发送者回执。
4. 接收方显示：

```text
[私聊] 发送者: 内容
```

5. 发送方显示：

```text
[私聊] 你对 目标用户 说: 内容
```

## 7.8 退出私聊流程

1. 客户端处于私聊状态时输入 `/exit`。
2. 客户端本地退出私聊状态。
3. 客户端提示“已退出私聊，回到公聊”。
4. 之后再次输入普通文字，默认发送公聊。

## 7.9 查看在线列表流程

1. 在公聊或私聊状态输入 `/list`。
2. 客户端向服务端发送在线列表请求。
3. 服务端从在线用户表中取出用户名列表并排序。
4. 客户端显示在线用户列表。

## 7.10 服务端关闭流程

1. 管理员在服务端控制台输入 `/exit`。
2. 服务端进入关闭流程。
3. 服务端向所有客户端发送：

```text
SHUTDOWN|服务器已关闭
```

4. 客户端收到后显示：

```text
[系统] 服务器已关闭
```

5. 客户端自动退出。
6. 服务端关闭所有连接并关闭监听器。

## 8. 可能遇到的情况和对应处理

## 8.1 注册时用户名重复

现象：

- 客户端收到“用户名已存在”提示。

处理：

- 服务端通过数据库唯一索引和重复键错误识别该情况。
- 客户端提示后回到主菜单重新选择。

## 8.2 登录时用户不存在

现象：

- 客户端提示“用户不存在”。

处理：

- 服务端查询不到该用户名时返回 `USER_NOT_FOUND`。
- 客户端回到主菜单。

## 8.3 登录时密码错误

现象：

- 客户端提示“密码错误”。

处理：

- 服务端返回 `PASSWORD_INCORRECT`。
- 客户端回到主菜单。

## 8.4 同一账号重复登录

现象：

- 第二个客户端登录同一账号时提示“该用户已经在线”。

处理：

- 服务端在在线用户表中检查该用户名是否已经存在。
- 如果已存在，则拒绝新的登录请求。

## 8.5 用户名或密码格式不合法

可能情况：

- 用户名为空
- 用户名长度超过 10
- 密码长度小于 6 或大于 10
- 用户名或密码中包含空格
- 用户名或密码中包含 `|`

处理：

- 客户端本地优先拦截，直接提示并回到菜单流程。
- 服务端也会再次校验，避免客户端绕过校验。

## 8.6 私聊目标不存在或不在线

现象：

- 输入 `/chat 某用户` 后进入私聊失败。

处理：

- 服务端返回 `TARGET_NOT_FOUND`。
- 客户端提示“目标用户不在线或不存在”。
- 客户端保持在公聊状态。

## 8.7 试图和自己私聊

现象：

- 输入 `/chat 自己的用户名` 后进入私聊失败。

处理：

- 服务端返回 `TARGET_SELF`。
- 客户端提示“不能和自己进入私聊”。

## 8.8 私聊状态下再次输入 `/chat 用户名`

现象：

- 客户端本地直接提示，不会向服务端发送请求。

处理：

- 客户端保持当前私聊对象不变。
- 用户如果需要切换对象，必须先输入 `/exit` 退出当前私聊，再重新输入 `/chat 用户名`。

## 8.9 私聊对象中途下线

现象：

- 当前已进入私聊，但对方后来下线。
- 再发送私聊消息时提示失败。

处理：

- 服务端发现目标不在线时返回系统错误消息。
- 客户端显示“私聊对象已离线，请输入 /exit 退出私聊”。
- 是否退出私聊由用户决定。

## 8.10 输入空消息

现象：

- 客户端提示“消息不能为空”。

处理：

- 客户端本地拒绝发送。
- 服务端也保留二次校验。

## 8.11 输入未知命令

现象：

- 例如输入 `/abc`。

处理：

- 客户端本地提示“未知命令”。
- 不会发送到服务端。

## 8.12 数据库连接失败

现象：

- 服务端启动时提示数据库初始化失败。

处理：

- 服务端直接终止启动。
- 避免在数据库不可用的情况下进入半运行状态。

## 8.13 数据库写入消息失败

现象：

- 发送消息时服务端返回“保存消息失败”。

处理：

- 服务端不会静默吞错。
- 客户端会收到系统错误提示。

## 8.14 收到无法识别的协议包

现象：

- 客户端或服务端收到格式不对的消息。

处理：

- 使用 `protocol` 包解析失败后，返回“无效命令”或“收到无法识别的服务端消息”。
- 避免程序直接崩溃。

## 8.15 服务端主动关闭

现象：

- 客户端收到“服务器已关闭”。

处理：

- 客户端立即输出提示。
- 自动退出程序，不要求用户再手动确认。

## 9. 测试与验证

本次重构加入了以下自动化测试：

- 协议和输入校验测试：`protocol`
- 数据库纯逻辑测试：`mysql`
- 服务端纯逻辑测试：`server`
- 客户端提示和状态变更测试：`client`

已执行验证命令：

```bash
go test ./...
```

验证结果：

- 当前所有包已通过测试
- `pre` 和 `test` 包没有单元测试，但能正常参与编译

## 10. 后续可扩展方向

如果后续继续迭代，建议优先考虑：

1. 对密码进行哈希存储，而不是明文保存。
2. 增加聊天记录查询功能。
3. 增加更稳定的集成测试，自动覆盖多客户端联调。
4. 进一步把客户端和服务端拆成多文件，便于后续继续扩展。

## 11. 函数清单

本节列出本次重构后新增或重写的主要函数，按文件分组说明用途。

## 11.1 `client/client.go`

- `main()`：客户端入口，先进入主菜单，登录成功后进入聊天循环。
- `serverAddr() string`：返回服务端地址，优先读取 `CHAT_SERVER_ADDR` 环境变量，否则使用 `localhost:8888`。
- `runMainMenu(reader *bufio.Reader) (string, net.Conn, error)`：运行登录前主菜单，直到登录成功或用户退出。
- `doAuthFlow(reader *bufio.Reader, action string) (string, net.Conn, error)`：执行一次登录或注册流程，包括读取账号密码、连接服务端、发送认证请求和处理认证响应。
- `promptCredentials(reader *bufio.Reader) (string, string, error)`：读取用户名和密码，并在客户端本地做格式校验。
- `runChat(conn net.Conn, reader *bufio.Reader, session *clientSession)`：登录后的聊天主循环，同时处理键盘输入、服务端消息和私聊进入结果。
- `handleChatInput(conn net.Conn, session *clientSession, line string) (bool, error)`：根据当前聊天模式解析用户输入，并决定发送公聊、私聊、列表请求、退出私聊或退出客户端。
- `readChatInput(reader *bufio.Reader, inputCh chan<- string)`：单独读取用户终端输入，并把输入内容送入聊天主循环。
- `receiveLoop(conn net.Conn, privateEnterCh chan<- privateEnterResult, doneCh chan<- struct{})`：持续接收服务端消息，处理私聊进入结果、普通消息展示和服务端关闭通知。
- `sendPayload(conn net.Conn, payload string) error`：客户端统一发包入口，内部调用 `pre.WritePacket`。
- `readLine(reader *bufio.Reader, prompt string) (string, error)`：读取一行命令行输入，并去掉换行符。
- `printMainMenu()`：打印客户端登录前主菜单。
- `printChatGuide()`：登录成功后打印聊天命令说明。
- `translateAuthError(code string) string`：把服务端认证错误码转换成中文提示。
- `translatePrivateEnterError(code string) string`：把进入私聊失败的错误码转换成中文提示。
- `renderServerPacket(packet protocol.Packet) (string, bool)`：把服务端协议包转换成客户端显示文本，并判断是否需要退出客户端。
- `applyPrivateEnterResult(session *clientSession, result privateEnterResult) string`：根据服务端返回的私聊进入结果更新客户端本地状态。

## 11.2 `server/server.go`

- `main()`：服务端入口，初始化数据库、建表、监听端口、启动控制台 `/exit` 监听。
- `newChatServer(listener net.Listener) *chatServer`：创建服务端对象并初始化连接表、在线表和关闭信号。
- `(s *chatServer) serve() error`：循环接收新 TCP 连接，并为每个连接启动独立 goroutine。
- `(s *chatServer) handleConnection(peer *clientConn)`：处理一个客户端连接从接入、认证、聊天到断开的完整生命周期。
- `(s *chatServer) handleGuestCommand(peer *clientConn, cmd protocol.Packet) bool`：处理未登录连接的命令，只允许登录和注册。
- `(s *chatServer) handleAuthedCommand(peer *clientConn, cmd protocol.Packet) bool`：处理已登录用户的公聊、私聊、列表和退出命令。
- `(s *chatServer) handleRegister(peer *clientConn, cmd protocol.Packet)`：处理注册请求，校验格式并调用数据库注册函数。
- `(s *chatServer) handleLogin(peer *clientConn, cmd protocol.Packet) bool`：处理登录请求，校验账号密码并登记在线用户。
- `(s *chatServer) handlePublicMessage(peer *clientConn, cmd protocol.Packet)`：处理公聊消息，落库后广播给所有在线用户。
- `(s *chatServer) handlePrivateEnter(peer *clientConn, cmd protocol.Packet)`：处理进入私聊请求，校验目标是否在线以及是否为自己。
- `(s *chatServer) handlePrivateMessage(peer *clientConn, cmd protocol.Packet)`：处理私聊消息，只发送给目标用户和发送者自己。
- `(s *chatServer) handleUserList(peer *clientConn)`：返回当前在线用户列表。
- `(s *chatServer) addPeer(peer *clientConn)`：把新接入连接加入连接表。
- `(s *chatServer) disconnectPeer(peer *clientConn)`：连接断开后从连接表和在线表中清理用户。
- `(s *chatServer) sendPacket(peer *clientConn, payload string) error`：服务端统一发包入口，负责加连接写锁并调用 `pre.WritePacket`。
- `(s *chatServer) snapshotOnlinePeers() []*clientConn`：返回当前在线连接快照，用于广播。
- `(s *chatServer) snapshotOnlineUsernames() []string`：返回排序后的在线用户名列表。
- `(s *chatServer) snapshotOnlineUserSet() map[string]struct{}`：返回在线用户名集合，用于快速判断私聊目标是否在线。
- `(s *chatServer) watchConsoleExit()`：监听服务端控制台输入，读到 `/exit` 后触发关服。
- `(s *chatServer) shutdownServer(message string)`：执行服务端优雅关闭，先通知客户端，再关闭连接和监听器。
- `(s *chatServer) snapshotAllPeers() []*clientConn`：返回所有连接快照，包括未登录连接。
- `(s *chatServer) isShuttingDown() bool`：判断服务端是否正在关闭，用于区分正常关服和异常错误。
- `mapLoginResultToCode(result mysql.LoginResult) string`：把数据库登录结果转换成协议错误码。
- `canEnterPrivateMode(sender, target string, online map[string]struct{}) string`：判断是否允许进入私聊。
- `formatUserList(users []string) string`：排序并拼接用户名列表。
- `isExitCommand(input string) bool`：判断服务端控制台输入是否为 `/exit`。

## 11.3 `protocol/protocol.go`

- `ValidateUsername(name string) error`：校验用户名长度、空格和协议分隔符。
- `ValidatePassword(password string) error`：校验密码长度、空格和协议分隔符。
- `ValidateMessage(text string) error`：校验消息内容不能为空。
- `ParseClientPacket(raw string) (Packet, error)`：解析客户端发给服务端的轻量协议报文。
- `ParseServerPacket(raw string) (Packet, error)`：解析服务端发给客户端的轻量协议报文。
- `ParseChatInput(mode ChatMode, input string) (InputCommand, error)`：解析客户端终端输入，转换成公聊、私聊、列表或退出动作。
- `MakePacket(cmd string, fields ...string) string`：统一拼接协议字符串，代替原来大量重复的 `BuildXXX` 函数。
- `firstField(raw string) string`：取协议字符串的第一个字段，用于判断命令名。
- `splitContent(raw string, parts int) (string, error)`：按指定段数切分协议，并保留正文里的 `|`。
- `invalid(raw string) error`：构造统一的无效协议错误。

## 11.4 `mysql/mysql.go`

- `InitMysql() error`：初始化 MySQL 连接池并验证连接。
- `CloseMysql() error`：关闭全局数据库连接池。
- `EnsureSchema() error`：创建 `user` 表、`news` 表和用户名唯一索引。
- `RegisterUser(name, password string) error`：注册新用户，并把重复用户名转换成 `ErrNameExists`。
- `CheckLogin(name, password string) (LoginResult, error)`：校验用户名和密码，并返回具体登录结果。
- `SavePublicMessage(sender, content string) error`：保存公聊消息。
- `SavePrivateMessage(sender, receiver, content string) error`：保存私聊消息。
- `saveMessage(sender, receiver, content string) error`：公聊和私聊共用的消息保存实现。
- `evaluateLoginResult(storedPassword, inputPassword string, queryErr error) (LoginResult, error)`：把数据库查询结果转换成登录业务结果。
- `isDuplicateNameError(err error) bool`：判断 MySQL 错误是否为用户名唯一索引冲突。

## 11.5 `pre/pre.go`

- `WritePacket(conn net.Conn, data []byte) error`：按“4 字节长度头 + 消息体”的格式发送完整 TCP 包。
- `ReadPacket(conn net.Conn) (string, error)`：按长度头读取一个完整 TCP 包。

## 11.6 `test/test.go`

- `main()`：手工验证数据库 API 的小脚本，用于检查连接、建表、注册和登录。

## 11.7 测试函数

- `TestTranslateAuthError(t *testing.T)`：测试认证错误码到中文提示的转换。
- `TestRenderServerPacket(t *testing.T)`：测试客户端如何渲染服务端消息。
- `TestApplyPrivateEnterResult(t *testing.T)`：测试客户端进入私聊后的本地状态更新。
- `TestCanEnterPrivateMode(t *testing.T)`：测试服务端私聊入口校验规则。
- `TestFormatUserList(t *testing.T)`：测试在线用户列表排序和拼接。
- `TestIsExitCommand(t *testing.T)`：测试服务端控制台退出命令识别。
- `TestValidateUsername(t *testing.T)`：测试用户名校验规则。
- `TestValidatePassword(t *testing.T)`：测试密码校验规则。
- `TestParseClientPacket(t *testing.T)`：测试客户端到服务端协议解析。
- `TestParseServerPacket(t *testing.T)`：测试服务端到客户端协议解析。
- `TestParseChatInput(t *testing.T)`：测试客户端聊天输入解析。
- `TestIsDuplicateNameError(t *testing.T)`：测试 MySQL 重复用户名错误识别。
- `TestEvaluateLoginResult(t *testing.T)`：测试数据库登录结果映射。
