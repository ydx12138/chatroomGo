# TCP Chat Simplified Design

Date: 2026-04-30

## Goal

Keep the chat room behavior unchanged while making the code easier to read and maintain.

The simplified version preserves:

- main-menu based login, registration, and exit
- public chat
- private-chat enter, send, and local exit
- `/list` online user lookup
- server-side `/exit` shutdown notification
- MySQL-backed user and message storage
- `pre.ReadPacket` and `pre.WritePacket` TCP framing
- the existing text wire protocol

## Current Package Responsibilities

### `pre`

Only handles TCP packet framing.

- `WritePacket` writes a 4-byte length header plus payload.
- `ReadPacket` reads exactly one framed payload.

It does not know about users, chat commands, or database behavior.

### `protocol`

Owns protocol constants, validation, packet construction, and packet parsing.

It keeps the client and server aligned on command names such as:

- `LOGIN`
- `REGISTER`
- `PUBLIC`
- `ENTER`
- `PRIVATE`
- `ACK`
- `LIST`
- `QUIT`
- `SHUTDOWN`

Messages may contain `|`, so message-bearing packets use `SplitN` parsing.

### `mysql`

Owns all database access.

Current public API:

- `InitMysql() error`
- `CloseMysql() error`
- `RegisterUser(name, password string) error`
- `CheckLogin(name, password string) (LoginResult, error)`
- `SavePublicMessage(sender, content string) error`
- `SavePrivateMessage(sender, receiver, content string) error`

Schema bootstrap is not part of the current code path. The server expects the `user` and `news` tables to already exist.

### `client`

Owns terminal interaction and local chat state.

The client keeps only local UI/session state:

- current username
- current private-chat target
- whether it is waiting for a private-enter response

The client no longer needs a separate `ChatMode` enum. `privateTarget == ""` means public mode, and a non-empty target means private mode.

### `server`

Owns connection lifecycle, authentication, online user tracking, message routing, and shutdown.

The server keeps:

- `peersByConn` for all connected clients, including unauthenticated clients
- `peersByName` for logged-in users only
- one global mutex for those maps
- one write mutex per client connection
- a shutdown channel and `sync.Once` for graceful stop

## Function Order Reference

The lists below follow the source files from top to bottom.

### `client/client.go`

1. `main()`: Client entry point; connects the menu flow to the chat flow.
2. `serverAddr() string`: Returns the server address used by the client.
3. `runMainMenu(reader *bufio.Reader) (string, net.Conn, error)`: Shows the menu and lets the user choose login, register, or exit.
4. `doAuthFlow(reader *bufio.Reader, action string) (string, net.Conn, error)`: Runs one login or registration attempt and reads the server response.
5. `promptCredentials(reader *bufio.Reader) (string, string, error)`: Reads the username and password from the terminal.
6. `runChat(conn net.Conn, reader *bufio.Reader, session *clientSession)`: Runs the chat screen and handles both user input and server messages.
7. `handleChatInput(conn net.Conn, session *clientSession, line string) (bool, error)`: Handles one line typed by the user in chat.
8. `readChatInput(reader *bufio.Reader, inputCh chan<- string)`: Keeps reading terminal input and sends it to the chat loop.
9. `receiveLoop(conn net.Conn, privateEnterCh chan<- privateEnterResult, doneCh chan<- struct{})`: Keeps reading server messages and forwards important results to the chat loop.
10. `sendPayload(conn net.Conn, payload string) error`: Sends one protocol message to the server.
11. `readLine(reader *bufio.Reader, prompt string) (string, error)`: Prints a prompt and reads one line.
12. `printMainMenu()`: Prints the pre-login menu.
13. `printChatGuide()`: Prints the available commands in chat.
14. `translateAuthError(code string) string`: Turns login and registration error codes into user-facing messages.
15. `translatePrivateEnterError(code string) string`: Turns private-chat entry error codes into user-facing messages.
16. `renderServerPacket(packet protocol.Packet) (string, bool)`: Turns a server packet into text that can be printed in the terminal.
17. `applyPrivateEnterResult(session *clientSession, result privateEnterResult) string`: Updates local private-chat state after the server confirms or rejects the request.

### `server/server.go`

1. `main()`: Server entry point; prepares the database, listens on the TCP port, and starts the server.
2. `newChatServer(listener net.Listener) *chatServer`: Creates the maps and shutdown signal used while the server is running.
3. `serve(server *chatServer) error`: Accepts new clients and starts one goroutine for each client.
4. `handleConnection(server *chatServer, peer *clientConn)`: Handles the full lifetime of one client connection.
5. `handleGuestCommand(server *chatServer, peer *clientConn, cmd protocol.Packet) bool`: Handles commands from a client that has not logged in yet.
6. `handleAuthedCommand(server *chatServer, peer *clientConn, cmd protocol.Packet) bool`: Handles chat commands from a logged-in user.
7. `handleRegister(peer *clientConn, cmd protocol.Packet)`: Checks registration data and saves the new user.
8. `handleLogin(server *chatServer, peer *clientConn, cmd protocol.Packet) bool`: Checks username and password, then marks the user as online.
9. `handlePublicMessage(server *chatServer, peer *clientConn, cmd protocol.Packet)`: Saves a public message and sends it to all online users.
10. `handlePrivateEnter(server *chatServer, peer *clientConn, cmd protocol.Packet)`: Checks whether the target user can be used for private chat.
11. `handlePrivateMessage(server *chatServer, peer *clientConn, cmd protocol.Packet)`: Saves a private message and sends it to both users.
12. `handleUserList(server *chatServer, peer *clientConn)`: Sends the current online user list to the client.
13. `addPeer(server *chatServer, peer *clientConn)`: Records a newly connected client.
14. `disconnectPeer(server *chatServer, peer *clientConn)`: Removes a disconnected client and closes its TCP connection.
15. `sendPacket(peer *clientConn, payload string) error`: Sends one complete protocol message to one client.
16. `sendErr(peer *clientConn, code string) error`: Sends an `ERR` packet with a specific error code.
17. `snapshotOnlinePeers(server *chatServer) []*clientConn`: Copies the current online connection list for later broadcasting.
18. `snapshotOnlineUsernames(server *chatServer) []string`: Copies and sorts the current online usernames.
19. `snapshotOnlineUserSet(server *chatServer) map[string]struct{}`: Builds a quick lookup set of online usernames.
20. `watchConsoleExit(server *chatServer)`: Watches server console input for `/exit`.
21. `shutdownServer(server *chatServer, message string)`: Notifies clients and closes connections and the listener.
22. `snapshotAllPeers(server *chatServer) []*clientConn`: Copies all connections, including clients that have not logged in.
23. `isShuttingDown(server *chatServer) bool`: Checks whether server shutdown has already started.
24. `mapLoginResultToCode(result mysql.LoginResult) string`: Converts the database login result into a client error code.
25. `canEnterPrivateMode(sender, target string, online map[string]struct{}) string`: Checks whether a private-chat target is valid.

## Preserved User Behavior

### Before Login

The client shows:

```text
1. Login
2. Register
3. Exit
```

Failed login or registration returns to the menu.

Registration success does not log the user in automatically.

### Public Chat

- Plain text sends a public message.
- `/chat <username>` asks the server to enter private chat.
- `/list` asks the server for online users.
- `/exit` exits the client.

### Private Chat

- Plain text sends a private message to the locked target.
- `/list` still works.
- `/exit` leaves private mode locally and returns to public mode.
- `/chat <username>` is rejected locally while already in private mode.

### Server Shutdown

When the server console receives `/exit`, it:

1. marks the server as shutting down
2. sends `SHUTDOWN|<message>` to all connected clients
3. closes client connections
4. closes the listener
5. lets connection goroutines exit without noisy expected errors

## Wire Protocol

Client to server:

```text
LOGIN|<username>|<password>
REGISTER|<username>|<password>
PUBLIC|<content>
ENTER|<target>
PRIVATE|<target>|<content>
LIST
QUIT
```

Server to client:

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
SYSTEM|<message>
PUBLIC|<sender>|<content>
PRIVATE|<sender>|<content>
ACK|<target>|<content>
ENTEROK|<target>
ENTERERR|<code>
LIST|<comma-separated-users>
SHUTDOWN|<message>
```

## Simplification Decisions

- Removed the client-side `ChatMode` enum and command parse struct.
- Removed protocol constants that were only wrapping fixed error strings.
- Replaced repeated authentication error packet construction with `sendErr`.
- Replaced long error-code switches in the client with small lookup maps.
- Simplified composite private-chat protocol prefixes to `ENTER`, `ENTEROK`, `ENTERERR`, and `ACK`.
- Removed commented-out dead code, including the unused schema bootstrap implementation.
- Removed redundant helper functions that no longer had call sites.
- Kept comments only where they explain function purpose or non-obvious concurrency/protocol decisions.

## Important Concurrency Rules

The server never writes to a client connection directly.

All writes go through `sendPacket`, which locks that connection's `writeMu`. This prevents concurrent goroutines from interleaving framed packet headers and payloads.

The server takes snapshots of online users before broadcasting or shutting down. This avoids holding the global map mutex while doing network IO.

## Verification

Run from `D:\go代码\DEMO2\TCPIP`:

```powershell
$env:GOCACHE='D:\codex_config\memories\go-build-cache'; go test ./...
```

Expected result:

```text
?    DEMO2/TCPIP/client    [no test files]
?    DEMO2/TCPIP/mysql     [no test files]
?    DEMO2/TCPIP/pre       [no test files]
?    DEMO2/TCPIP/protocol  [no test files]
?    DEMO2/TCPIP/server    [no test files]
```

Manual smoke testing should still cover:

- register success
- duplicate register failure
- wrong-password login failure
- login success
- public message broadcast
- `/list`
- private enter, send, and `/exit`
- server `/exit` notification and shutdown
