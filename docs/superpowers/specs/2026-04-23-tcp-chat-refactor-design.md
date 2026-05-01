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

The client no longer needs a separate `ChatMode` enum. `privateTarget == ""` means public mode, and a non-empty target means private mode.

### `server`

Owns connection lifecycle, authentication, online user tracking, message routing, and shutdown.

The server keeps:

- `peersByConn` for all connected clients, including unauthenticated clients
- `peersByName` for logged-in users only
- one global mutex for those maps
- one write mutex per client connection
- a shutdown channel for marking server shutdown

## Struct Field Reference

### `protocol.Packet`

`Packet` is the shared parsed result for protocol messages. Not every message uses every field.

- `Cmd`: The message command, such as `LOGIN`, `PUBLIC`, or `PRIVATE`.
- `Username`: The username used by login and registration messages.
- `Password`: The password used by login and registration messages.
- `Target`: The message target. For chat messages it is the sender or receiver; for private-chat entry it is the target user.
- `Content`: Chat text or regular content carried by a command.
- `Code`: An error code, such as `NAME_EXISTS` or `TARGET_NOT_FOUND`.
- `Message`: System text, such as shutdown notices or other server messages.

### `client.clientSession`

`clientSession` keeps only the local chat state the client needs.

- `username`: The currently logged-in username.
- `privateTarget`: The current private-chat target; empty means the client is in public chat.

### `client.privateEnterResult`

`privateEnterResult` passes private-chat entry results from the receiving goroutine back to the chat loop.

- `ok`: Whether private-chat entry succeeded.
- `target`: The target username when entry succeeds.
- `code`: The error code when entry fails.

### `server.clientConn`

`clientConn` records one client connection from the server's point of view.

- `conn`: The underlying TCP connection used for network reads and writes.
- `username`: The username after this connection logs in; empty before login.
- `writeMu`: A per-connection write lock so only one goroutine writes to this client at a time.

### `server.chatServer`

`chatServer` stores shared server runtime data.

- `listener`: The TCP listener that accepts new client connections.
- `peersByConn`: All client connections, including clients that have not logged in.
- `peersByName`: Logged-in users, indexed by username.
- `mu`: Protects `peersByConn` and `peersByName`.
- `shutdownCh`: Shutdown signal; closed once the server starts shutting down.
- `shutdownWg`: Waits for all connection-handling goroutines to exit.

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

## Main Flow Reference

### Client Startup And Main Menu

1. `main` creates the terminal reader.
2. `main` calls `runMainMenu`.
3. `runMainMenu` prints the main menu and lets the user choose login, registration, or exit.
4. If the user exits, `runMainMenu` returns `errClientExit`, and `main` treats it as a normal exit.
5. If login succeeds, `runMainMenu` returns the username and the still-open TCP connection.
6. `main` creates `clientSession`, then calls `runChat`.

### Registration Flow

1. The user chooses registration in the main menu.
2. `runMainMenu` calls `doAuthFlow(reader, protocol.CmdRegister)`.
3. `doAuthFlow` calls `promptCredentials` to read username and password, and the client checks their format first.
4. `doAuthFlow` connects to the server and uses `sendPayload` to send `REGISTER|<username>|<password>`.
5. The server `serve` function accepts the connection and starts `handleConnection` in a goroutine.
6. `handleConnection` reads the TCP packet and parses `REGISTER` with `protocol.ParseClientPacket`.
7. Because the connection is not logged in yet, `handleConnection` passes the command to `handleGuestCommand`.
8. `handleGuestCommand` calls `handleRegister`.
9. `handleRegister` checks username and password again, then calls `mysql.RegisterUser`.
10. On success, the server sends `OK|REGISTER`; duplicate names return `ERR|NAME_EXISTS`; database failures return `ERR|DB_ERROR`.
11. The client `doAuthFlow` reads the result. Success prints a registration message and returns to the menu; failure uses `translateAuthError`.
12. Registration does not log the user in automatically.

### Login Flow

1. The user chooses login in the main menu.
2. `runMainMenu` calls `doAuthFlow(reader, protocol.CmdLogin)`.
3. `doAuthFlow` reads username and password, connects to the server, and sends `LOGIN|<username>|<password>`.
4. Server `handleConnection` reads and parses `LOGIN`.
5. Commands from not-yet-logged-in connections go to `handleGuestCommand`.
6. `handleGuestCommand` calls `handleLogin`.
7. `handleLogin` checks username and password format, then calls `mysql.CheckLogin`.
8. User-not-found, wrong-password, and database failures are converted into error codes and sent with `sendErr`.
9. Before completing login, `handleLogin` checks `peersByName` so the same username cannot be online twice.
10. On success, the server sets `peer.username` and stores the connection in `peersByName`.
11. The server returns `OK|LOGIN`.
12. The client prints the chat command guide and passes the connection to `runChat`.

### Chat Loop

1. `runChat` creates three channels: input, private-entry result, and done.
2. `runChat` starts `readChatInput` in a goroutine to read keyboard input.
3. `runChat` starts `receiveLoop` in a goroutine to read server messages.
4. `runChat` keeps the main loop responsible for user input, private-entry results, and connection close events.
5. User input is handled by `handleChatInput`.
6. Server `ENTEROK` and `ENTERERR` packets are forwarded by `receiveLoop` to `runChat`.
7. `runChat` calls `applyPrivateEnterResult` to update local private-chat state.
8. Normal server packets are rendered by `receiveLoop` through `renderServerPacket`.

### Public Message Flow

1. In public chat, the user types regular text.
2. `handleChatInput` sees `privateTarget == ""` and builds `PUBLIC|<content>`.
3. The client sends it with `sendPayload`.
4. Server `handleConnection` reads and parses `PUBLIC`.
5. Logged-in user commands go to `handleAuthedCommand`.
6. `handleAuthedCommand` calls `handlePublicMessage`.
7. `handlePublicMessage` checks that the message is not empty, then calls `mysql.SavePublicMessage`.
8. On success, the server calls `snapshotOnlinePeers` to copy the current online connections.
9. The server sends `PUBLIC|<sender>|<content>` to every online user.
10. Client `receiveLoop` receives `PUBLIC`, and `renderServerPacket` prints it as a public chat message.

### Private Chat Entry Flow

1. In public chat, the user types `/chat <username>`.
2. `handleChatInput` checks that the client is not already in private mode and validates the target username.
3. The client sends `ENTER|<target>`.
4. Server `handleConnection` parses `ENTER`.
5. `handleAuthedCommand` calls `handlePrivateEnter`.
6. `handlePrivateEnter` validates the target username.
7. `handlePrivateEnter` copies online usernames with `snapshotOnlineUserSet`.
8. `canEnterPrivateMode` checks whether the target is the sender or is offline.
9. If entry is allowed, the server sends `ENTEROK|<target>`.
10. If entry fails, the server sends `ENTERERR|<code>`.
11. Client `receiveLoop` forwards `ENTEROK` or `ENTERERR` to `runChat`.
12. `runChat` calls `applyPrivateEnterResult`. Success sets `privateTarget`; failure clears it and prints an error.

### Private Message Flow

1. After private chat entry, `privateTarget` stores the current private-chat target.
2. The user types regular text.
3. `handleChatInput` sees `privateTarget != ""` and sends `PRIVATE|<target>|<content>`.
4. Server `handleConnection` parses `PRIVATE`.
5. `handleAuthedCommand` calls `handlePrivateMessage`.
6. `handlePrivateMessage` validates the target username and message content.
7. The server looks up the target connection in `peersByName`.
8. If the target is offline, the server sends a system message back to the sender.
9. If the target is the sender, the server sends a system message back to the sender.
10. If checks pass, the server calls `mysql.SavePrivateMessage`.
11. On success, the server sends `PRIVATE|<sender>|<content>` to the target user.
12. The server sends `ACK|<target>|<content>` to the sender so the sender sees their own private message.
13. Client `receiveLoop` renders `PRIVATE` and `ACK` through `renderServerPacket`.

### Online User List Flow

1. The user types `/list`.
2. `handleChatInput` sends `LIST`.
3. Server `handleAuthedCommand` calls `handleUserList`.
4. `handleUserList` calls `snapshotOnlineUsernames` to copy and sort online usernames.
5. The server returns `LIST|<comma-separated-users>`.
6. Client `renderServerPacket` prints the online user list.

### Leaving Private Chat And Exiting The Client

1. In private mode, the user types `/exit`.
2. `handleChatInput` clears local `privateTarget` and does not notify the server.
3. The user returns to public chat.
4. In public mode, the user types `/exit`.
5. `handleChatInput` sends `QUIT` to the server and tells `runChat` to exit.
6. Server `handleAuthedCommand` returns `true` for `QUIT`.
7. `handleConnection` exits, and its deferred `disconnectPeer` cleans up the connection and online map.

### Common Server Message Handling

1. Each client connection is handled by one `handleConnection` goroutine.
2. `handleConnection` reads complete TCP frames through `pre.ReadPacket`.
3. If reading fails and the server is not shutting down, it prints the disconnect error.
4. If reading succeeds, `handleConnection` parses the command with `protocol.ParseClientPacket`.
5. If parsing fails, the server sends `SYSTEM|无效命令`.
6. If `peer.username == ""`, the command goes to `handleGuestCommand`.
7. If `peer.username != ""`, the command goes to `handleAuthedCommand`.
8. When connection handling ends, `disconnectPeer` removes the connection from `peersByConn` and `peersByName`.

### Server Shutdown Flow

1. The server console receives `/exit`.
2. `watchConsoleExit` calls `shutdownServer`.
3. `shutdownServer` closes `shutdownCh`, marking the server as shutting down.
4. `shutdownServer` calls `snapshotAllPeers` to copy all connections, including not-yet-logged-in clients.
5. The server sends `SHUTDOWN|<message>` to every connection.
6. The server closes every client connection.
7. The server closes the listener, so `Accept` in `serve` returns an error.
8. `serve` sees `isShuttingDown(server) == true` and returns normally.
9. `main` waits on `shutdownWg` so connection goroutines can exit.

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
