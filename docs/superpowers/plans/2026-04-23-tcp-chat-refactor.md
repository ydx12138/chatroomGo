# TCP Chat Simplification Notes

Date: 2026-04-30

## Scope

This document records the current simplified implementation state. It replaces the old task-by-task refactor plan, which described work that is no longer the exact direction of the codebase.

The current goal is:

- keep behavior unchanged
- reduce unnecessary protocol/client state layers
- remove dead commented code
- keep useful comments before functions and complex logic
- verify compilation with `go test ./...`

## Files Updated By The Simplification

- `client/client.go`
- `server/server.go`
- `protocol/protocol.go`
- `mysql/mysql.go`
- `pre/pre.go`
- `docs/superpowers/specs/2026-04-23-tcp-chat-refactor-design.md`
- `docs/superpowers/specs/2026-04-23-tcp-chat-refactor-design-cn.md`
- `docs/superpowers/plans/2026-04-23-tcp-chat-refactor.md`

## Completed Changes

- Simplified client private/public mode detection.
- Removed unused `ChatMode`, `InputCommand`, and `ParseChatInput` protocol-side abstractions.
- Kept chat input behavior in `handleChatInput`.
- Simplified error-code translation in the client with maps.
- Added `sendErr` in the server to reduce repeated error-packet construction.
- Simplified private-chat protocol values to `ENTER`, `ENTEROK`, `ENTERERR`, and `ACK`.
- Restored useful comments before functions and non-obvious logic.
- Removed obsolete commented-out schema bootstrap code.
- Updated docs to describe the current implementation instead of the earlier planned refactor.

## Current Verification Command

Run from `D:\go代码\DEMO2\TCPIP`:

```powershell
$env:GOCACHE='D:\codex_config\memories\go-build-cache'; go test ./...
```

The custom `GOCACHE` path avoids permission issues with the default user-level Go build cache in this environment.

## Manual Checks Still Recommended

Automated tests currently only compile packages because there are no `_test.go` files.

Manual smoke checks should cover:

- register success returns to menu
- duplicate register shows a clear error
- wrong password shows a clear error
- login success enters chat
- public chat broadcasts to online users
- `/list` returns online users
- `/chat <user>` enters private mode
- private `/exit` returns to public chat
- public `/exit` exits the client
- server console `/exit` notifies clients and stops the listener

## Notes For Future Work

- Add focused unit tests for `protocol`, `client` rendering/state helpers, and `server` pure helpers.
- Decide whether schema bootstrap should be restored as a real feature or kept manual.
- Consider password hashing before treating this as a production-ready chat service.
