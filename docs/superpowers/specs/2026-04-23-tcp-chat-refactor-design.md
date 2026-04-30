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
