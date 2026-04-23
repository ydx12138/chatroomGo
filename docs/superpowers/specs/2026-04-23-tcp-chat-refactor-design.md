# TCP Chat Refactor Design

Date: 2026-04-23

## 1. Goal

Refactor the existing Go TCP chat project so that:

- the client starts from a main menu with `login`, `register`, and `exit`
- registration and login failures always show a clear reason and return to the main menu
- username and password validation is enforced consistently
- private chat becomes an explicit mode with enter/exit behavior
- `/list` continues to show online users
- all packet IO continues to use `pre.ReadPacket` and `pre.WritePacket`
- all MySQL access is centralized in `mysql/mysql.go`
- the server can shut down from console input `/exit`, notify all clients, and close cleanly

## 2. User Rules To Preserve

- Username must be unique.
- Username length: 1-10.
- Password length: 6-10.
- Username and password cannot contain spaces.
- A failed login or registration must return the client to the main menu.
- Register success must show a success message and return to the main menu.
- Login success enters the chat page.
- Public chat:
  - normal input sends a public message
  - `/chat <username>` enters private-chat mode
  - `/list` shows online users
  - `/exit` exits the client
- Private-chat mode:
  - normal input sends only to the locked target user
  - entering another private chat is not allowed while already in private-chat mode
  - `/exit` leaves private-chat mode and returns to public chat
  - `/list` still works
- When the server shuts down, clients must receive a system message and then exit automatically.

## 3. Current Problems

- Client flow mixes authentication and chat behavior in a single procedural path.
- Server authentication, routing, broadcast, and connection lifecycle are tightly coupled.
- Multiple goroutines can write to one connection at the same time.
- MySQL calls are spread through business logic instead of going through focused database functions.
- There is no explicit private-chat session state on the client.
- Server shutdown does not currently support console-triggered graceful exit.

## 4. Refactor Strategy

Use a structured refactor, not a full rewrite.

- Keep the existing top-level packages: `client`, `server`, `mysql`, `pre`.
- Keep `pre` as the only packet framing layer.
- Split client and server logic into small functions and multiple files if needed.
- Move all SQL into `mysql/mysql.go`.
- Introduce explicit session state on both client and server.
- Introduce a simple command protocol over `pre` packets.

This keeps the project recognizable while reducing coupling and making each behavior testable.

## 5. Package Responsibilities

### 5.1 `pre`

Responsibility:

- send one framed packet
- receive one framed packet

Non-goals:

- no business parsing
- no authentication logic
- no chat routing

### 5.2 `mysql`

Responsibility:

- initialize and close the DB connection
- ensure required tables and indexes exist
- register users
- validate login credentials
- persist public messages
- persist private messages

Business code outside this package must not write SQL directly.

### 5.3 `client`

Responsibility:

- render the main menu
- read menu choice
- validate login/register input locally
- send auth or chat commands
- maintain chat mode (`public` vs `private`)
- print server messages with clear prefixes
- exit automatically on server shutdown

### 5.4 `server`

Responsibility:

- accept TCP connections
- handle unauthenticated auth requests
- create and remove online sessions
- route commands
- broadcast public messages
- send private messages
- serve online user list
- monitor console input for `/exit`
- notify clients and shut down gracefully

## 6. Client Design

### 6.1 Main Menu State

The client starts in a loop:

1. show the main menu
2. read one menu choice
3. branch to login, register, or exit
4. after any failed login/register attempt, return to step 1

Main menu options:

- `1` login
- `2` register
- `3` exit

Invalid menu input shows an error and redisplays the menu.

### 6.2 Local Validation

The client validates before sending auth requests:

- username length: `1 <= len <= 10`
- password length: `6 <= len <= 10`
- no spaces in username
- no spaces in password

Implementation note:

- the command protocol uses `|` as a delimiter
- to keep parsing deterministic, username and password will also reject `|`

Validation is implemented in focused helper functions so both login and register can reuse the same rules.

### 6.3 Chat States

The client has exactly two chat states after login:

- `public`
- `private`

Client session fields:

- current username
- current chat mode
- current private target, empty when in public mode

#### Public mode

- normal input sends public chat
- `/chat <username>` requests entry into private mode
- `/list` requests online users
- `/exit` exits the client

#### Private mode

- normal input sends a private message to the locked target
- `/chat <username>` is rejected locally with a system prompt
- `/list` still requests online users
- `/exit` leaves private mode and returns to public mode

### 6.4 Receiving Messages

The receiver goroutine only does these jobs:

- read one packet through `pre.ReadPacket`
- parse the server response
- print a user-facing line
- detect server shutdown and trigger client exit

Displayed prefixes:

- `[SYSTEM]`
- `[PUBLIC]`
- `[PRIVATE]`

If the server sends a shutdown event, the client prints the shutdown message and exits.

## 7. Server Design

### 7.1 Session Model

Each authenticated connection becomes one online session.

Session fields:

- `conn`
- `username`
- `writeMu`

The server stores:

- a connection-to-session map
- a username-to-session map
- one mutex protecting online session maps
- a shutdown signal

### 7.2 Single-Connection Write Safety

Every write to a client connection must go through one helper function:

- lock session `writeMu`
- call `pre.WritePacket`
- unlock `writeMu`

This avoids interleaved packet headers and payloads when multiple goroutines write to the same connection.

### 7.3 Connection Lifecycle

For each accepted connection:

1. create an unauthenticated handler loop
2. accept only auth commands until login succeeds
3. after login success, create an online session
4. enter the authenticated command loop
5. on disconnect or explicit quit, remove the session and close the connection

### 7.4 Auth Behavior

#### Register

Server checks:

- validation rules again on the server side
- username uniqueness through `mysql`

Possible register responses:

- success
- username already exists
- invalid username
- invalid password
- database error

Register success does not log the user in.

#### Login

Server checks:

- validation rules again on the server side
- user existence and password match through `mysql`
- online session map to prevent duplicate login

Possible login responses:

- success
- user does not exist
- password incorrect
- user already online
- invalid username
- invalid password
- database error

Login success adds the session to the online map.

### 7.5 Command Routing

After login, the server handles these command groups:

- public message
- private chat enter
- private message
- online user list
- quit

Each command has its own handler function.

### 7.6 Public Chat

For a public message:

1. validate non-empty content
2. save the message through `mysql.SavePublicMessage`
3. broadcast to all online users

The sender also receives the broadcast like everyone else.

### 7.7 Private Chat

Private chat is a client-side mode with server-side validation.

#### Enter private mode

Client sends an enter-private request with a target username.

Server checks:

- target exists online
- target is not the sender

Responses:

- success, client enters private mode
- target offline or missing
- target is self

#### Send private message

Client sends target username plus message content.

Server:

- validates target still online
- saves through `mysql.SavePrivateMessage`
- sends the message only to the target
- sends an acknowledgement to the sender

The message is not broadcast.

### 7.8 Online User List

`/list` remains a lightweight command.

Server returns one response containing all online usernames.

### 7.9 Server Console Shutdown

The server starts a goroutine that reads console input.

If the console input is `/exit`:

1. mark the server as shutting down
2. send a shutdown system message to all connected clients
3. close all client connections
4. close the TCP listener
5. stop the process cleanly

During shutdown:

- accept loop should stop
- client handlers should stop without printing misleading errors

## 8. Wire Protocol

The project will keep framed packets from `pre`, but the payload will use a text command protocol.

General rule:

- fields are separated by `|`
- parsing uses `strings.SplitN`
- the last field is allowed to contain `|`

Client-to-server payloads:

- `LOGIN|<username>|<password>`
- `REGISTER|<username>|<password>`
- `PUBLIC|<content>`
- `PRIVATE_ENTER|<target>`
- `PRIVATE|<target>|<content>`
- `LIST`
- `QUIT`

Server-to-client payloads:

- `OK|LOGIN`
- `OK|REGISTER`
- `ERR|USER_NOT_FOUND`
- `ERR|NAME_EXISTS`
- `ERR|PASSWORD_INCORRECT`
- `ERR|ALREADY_ONLINE`
- `ERR|INVALID_USERNAME`
- `ERR|INVALID_PASSWORD`
- `ERR|DB_ERROR`
- `SYSTEM|<message>`
- `PUBLIC|<sender>|<content>`
- `PRIVATE|<sender>|<content>`
- `PRIVATE_ACK|<target>|<content>`
- `PRIVATE_ENTER_OK|<target>`
- `PRIVATE_ENTER_ERR|<code>`
- `LIST|<comma-separated-users>`
- `SHUTDOWN|Server is shutting down`

## 9. Database Design

The existing DB package will be turned into a focused access layer.

Planned functions:

- `InitMysql() error`
- `CloseMysql() error`
- `EnsureSchema() error`
- `RegisterUser(name, password string) error`
- `CheckLogin(name, password string) (LoginResult, error)`
- `SavePublicMessage(sender, content string) error`
- `SavePrivateMessage(sender, receiver, content string) error`

The design will preserve the current tables conceptually, but make schema enforcement explicit.

Planned schema behavior:

- ensure `user` table exists
- ensure `news` table exists
- ensure a unique index exists on `user.name`

Suggested SQL shape:

```sql
CREATE TABLE IF NOT EXISTS user (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(10) NOT NULL,
    password VARCHAR(10) NOT NULL,
    UNIQUE KEY uk_user_name (name)
);

CREATE TABLE IF NOT EXISTS news (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    content TEXT NOT NULL,
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    send_name VARCHAR(10) NOT NULL,
    receive_name VARCHAR(10) NOT NULL DEFAULT ''
);
```

`receive_name = ''` means a public message.

## 10. Testing Strategy

The refactor should follow TDD where practical by extracting pure helpers first.

Planned unit-test targets:

- username validation
- password validation
- menu choice validation
- public/private command parsing
- private-mode transition rules
- auth result to user-message mapping

Integration verification:

- `go test ./...`
- build or run the client and server successfully
- manual smoke test for:
  - register success
  - duplicate register failure
  - login wrong password failure
  - duplicate online login failure
  - `/list`
  - private mode enter/send/exit
  - server `/exit` shutdown behavior

Database-heavy behavior will still be exercised manually because the project depends on a local MySQL instance.

## 11. Non-Goals

- no GUI client
- no history replay feature
- no message encryption
- no password hashing in this refactor
- no protocol compatibility with the old client/server pair

## 12. Risks And Mitigations

Risk: protocol parsing bugs in string commands.

Mitigation:

- use small parsing helpers
- test each command shape
- keep the command vocabulary small

Risk: shutdown races while handlers are still running.

Mitigation:

- add a shutdown flag/channel
- centralize connection close behavior
- make accept-loop shutdown expected instead of noisy

Risk: DB uniqueness only checked in code.

Mitigation:

- enforce unique index in MySQL
- map duplicate-key DB errors to the correct user-facing message

## 13. Implementation Outline

1. Add tests for validation and parsing helpers.
2. Refactor `mysql` into a focused access layer plus schema setup.
3. Refactor server session and command-routing logic.
4. Add server console shutdown support.
5. Refactor client into menu/auth/chat state loops.
6. Verify with tests and a manual smoke pass.
