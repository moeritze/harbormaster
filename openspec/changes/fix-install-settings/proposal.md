## Why

`hm install`/`uninstall` edit files the user owns. Uninstall deletes a user's own hook when it shares a hook entry with ours (#6), every rewrite turns `&`, `<`, `>` in the user's settings into `&`-style escapes (#19), and AGENTS.md is rewritten in place, so a crash mid-write can truncate it.

## What Changes

- #6: Claude uninstall removes only our commands from an entry's inner `hooks` array; the entry is dropped only when that array becomes empty. (Cursor's hooks.json is flat — one command per entry — so it is already correct; no change.)
- #19: settings.json is written with HTML escaping off, keeping user strings byte-for-byte.
- AGENTS.md install/uninstall write via temp file + rename, preserving the file's mode.
- Cleanup: inline `dirOf` (one-caller wrapper of `filepath.Dir`).

## Non-goals

- Preserving key order or formatting of settings.json (Go maps reorder; unchanged).
- Atomic skill-file writes.

## Capabilities

### New Capabilities
- `settings-install`: how harbormaster adds and removes its hooks and instructions in user-owned config files.

### Modified Capabilities

## Impact

- `internal/install/claude.go` (`ClaudeUninstall`), `internal/install/settings.go` (`write`, `dirOf`), `internal/install/agentsmd.go`; tests in `internal/install/claude_test.go`, `internal/install/cursor_test.go`.
