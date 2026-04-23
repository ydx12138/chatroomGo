# TCP Chat Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the TCP chat project into a lower-coupling client/server design with menu-based auth, private-chat mode, centralized MySQL access, and graceful server shutdown.

**Architecture:** Keep the existing `client`, `server`, `mysql`, and `pre` packages, but split behavior into smaller helpers and explicit state transitions. Use a small text command protocol over `pre` packets so client and server can coordinate auth, public chat, private chat, user listing, and shutdown consistently.

**Tech Stack:** Go, TCP, MySQL, `database/sql`, `github.com/go-sql-driver/mysql`, Go unit tests

---

## File Map

- Modify: `client/client.go`
- Create: `client/client_test.go`
- Modify: `mysql/mysql.go`
- Create: `mysql/mysql_test.go`
- Modify: `server/server.go`
- Create: `server/server_test.go`
- Modify: `test/test.go`
- Create: `docs/superpowers/specs/2026-04-23-tcp-chat-refactor-design-cn.md`

### Task 1: Build test coverage for shared validation and parsing rules

**Files:**
- Modify: `client/client.go`
- Test: `client/client_test.go`
- Modify: `server/server.go`
- Test: `server/server_test.go`

- [ ] **Step 1: Write failing validation tests**

```go
func TestValidateUsername(t *testing.T) {
    cases := []struct {
        name    string
        input   string
        wantOK  bool
    }{
        {name: "valid", input: "alice", wantOK: true},
        {name: "empty", input: "", wantOK: false},
        {name: "too long", input: "abcdefghijk", wantOK: false},
        {name: "space", input: "alice bob", wantOK: false},
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./client ./server`
Expected: FAIL because validation helpers do not exist yet.

- [ ] **Step 3: Implement minimal shared validation helpers in client/server**

```go
func validateUsername(name string) error
func validatePassword(password string) error
```

- [ ] **Step 4: Add parser tests for `/chat`, `/list`, `/exit`, and plain chat input**

```go
func TestParseChatInput(t *testing.T) {
    // cover public message, private enter, list, and exit cases
}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./client ./server`
Expected: PASS.

### Task 2: Refactor the MySQL access layer and schema bootstrap

**Files:**
- Modify: `mysql/mysql.go`
- Test: `mysql/mysql_test.go`

- [ ] **Step 1: Write failing tests for duplicate-key detection and credential validation result mapping**

```go
func TestIsDuplicateNameError(t *testing.T) {}
func TestMapLoginRowState(t *testing.T) {}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./mysql`
Expected: FAIL because helper functions are not defined.

- [ ] **Step 3: Implement focused DB API**

```go
func InitMysql() error
func CloseMysql() error
func EnsureSchema() error
func RegisterUser(name, password string) error
func CheckLogin(name, password string) (LoginResult, error)
func SavePublicMessage(sender, content string) error
func SavePrivateMessage(sender, receiver, content string) error
```

- [ ] **Step 4: Add comments to exported DB functions and schema SQL**

```go
// EnsureSchema creates the tables and unique index required by the chat service.
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./mysql`
Expected: PASS.

### Task 3: Refactor server protocol helpers and authenticated session model

**Files:**
- Modify: `server/server.go`
- Test: `server/server_test.go`

- [ ] **Step 1: Write failing tests for command parsing and auth response mapping**

```go
func TestParseClientPacket(t *testing.T) {}
func TestBuildAuthErrorMessage(t *testing.T) {}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./server`
Expected: FAIL because protocol helpers are missing.

- [ ] **Step 3: Implement protocol helpers and session structs**

```go
type clientCommand struct { /* kind, action, target, content */ }
type session struct { /* conn, username, writeMu */ }
func parseClientCommand(raw string) (clientCommand, error)
func sendPacket(sess *session, payload string) error
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./server`
Expected: PASS.

### Task 4: Implement server auth, chat routing, and `/list`

**Files:**
- Modify: `server/server.go`
- Test: `server/server_test.go`

- [ ] **Step 1: Write failing tests for private-enter validation and online-user rendering**

```go
func TestCanEnterPrivateMode(t *testing.T) {}
func TestFormatUserList(t *testing.T) {}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./server`
Expected: FAIL because routing helpers do not exist.

- [ ] **Step 3: Implement auth handlers and chat handlers**

```go
func handleRegister(conn net.Conn, cmd clientCommand) error
func handleLogin(conn net.Conn, cmd clientCommand) (*session, error)
func handlePublicMessage(sess *session, cmd clientCommand)
func handlePrivateEnter(sess *session, cmd clientCommand)
func handlePrivateMessage(sess *session, cmd clientCommand)
func handleUserList(sess *session)
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./server`
Expected: PASS.

### Task 5: Add graceful server shutdown from console `/exit`

**Files:**
- Modify: `server/server.go`
- Test: `server/server_test.go`

- [ ] **Step 1: Write failing tests for shutdown-message detection helpers**

```go
func TestIsExitCommand(t *testing.T) {}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./server`
Expected: FAIL because shutdown helpers are missing.

- [ ] **Step 3: Implement console loop and graceful shutdown**

```go
func watchConsoleExit()
func shutdownServer()
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./server`
Expected: PASS.

### Task 6: Refactor the client into main-menu and chat-mode loops

**Files:**
- Modify: `client/client.go`
- Test: `client/client_test.go`

- [ ] **Step 1: Write failing tests for menu and private-mode transitions**

```go
func TestMenuChoice(t *testing.T) {}
func TestPrivateModeExit(t *testing.T) {}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./client`
Expected: FAIL because transition helpers are missing.

- [ ] **Step 3: Implement client session state and loops**

```go
type chatMode int
type clientSession struct { /* username, mode, privateTarget */ }
func runMainMenu(conn net.Conn) error
func runChatLoop(conn net.Conn, session *clientSession) error
func handlePublicInput(input string, session *clientSession) (string, bool, error)
func handlePrivateInput(input string, session *clientSession) (string, bool, error)
```

- [ ] **Step 4: Implement receiver-side message rendering and shutdown auto-exit**

```go
func receiveLoop(conn net.Conn, done chan<- struct{})
func renderServerMessage(raw string) (string, bool)
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./client`
Expected: PASS.

### Task 7: Update helper script and add the final Chinese design document

**Files:**
- Modify: `test/test.go`
- Create: `docs/superpowers/specs/2026-04-23-tcp-chat-refactor-design-cn.md`

- [ ] **Step 1: Refresh the DB smoke script to use the new MySQL API**

```go
func main() {
    // initialize mysql and call the new helper functions
}
```

- [ ] **Step 2: Write the Chinese design document**

```md
# TCP 聊天室重构详细设计
## 使用流程
## 可能遇到的情况和处理方式
```

- [ ] **Step 3: Verify docs and helper script**

Run: `go test ./...`
Expected: PASS.

### Task 8: Full verification

**Files:**
- Modify: `client/client.go`
- Modify: `server/server.go`
- Modify: `mysql/mysql.go`

- [ ] **Step 1: Run the full automated suite**

Run: `go test ./...`
Expected: PASS for all packages.

- [ ] **Step 2: Manual smoke flow**

Run server: `go run ./TCPIP/server`
Run client: `go run ./TCPIP/client`
Expected:
- register success returns to menu
- duplicate register fails with prompt
- login wrong password fails with prompt
- login success enters chat
- `/chat <user>` enters private mode
- private `/exit` returns to public mode
- public `/exit` exits client
- server `/exit` notifies clients and exits

- [ ] **Step 3: Review comments and user-facing prompts**

Check:
- exported functions have concise comments
- non-obvious logic has short comments
- prompts are readable Chinese text
