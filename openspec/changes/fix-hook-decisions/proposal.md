## Why

Hooks run before every agent shell command and must answer fast and correctly. Today they deny commands because of entries whose process is already dead (#7), do a full locked `Store.Load` (prune, probes, `ps`) just to print a port hint (#8), and the SessionEnd cleanup can wait 5 s for the lock inside a 3 s Claude hook budget (#8). Separately, `hm port` is documented as stable but returns a different port whenever the deterministic one is busy (#15). The Cursor adapter also carries dead code.

## What Changes

- #7: PreShell ignores registry entries whose pid is not alive before deciding deny/ask.
- #8: the hook's port hint is computed from the entries it already peeked (no `Load`, no lock, no probes). SessionEnd bounds its registry lock wait and probe budget below the hook timeout.
- #15: `hm port` always prints the deterministic port; if it is taken it says so on stderr and names the port `run` would use.
- Cleanup: delete unused `claude.Events`, `cursor.Events`, and Cursor's sessionStart `env` block (hooks take identity from the payload, so it has no effect).

## Non-goals

- Making `Peek` lock-free (done in `fix-registry-lock-and-history`; this change works with either).
- Changing `run`'s port fallback.

## Capabilities

### New Capabilities
- `agent-hooks`: what the Claude Code / Cursor hooks decide, say, and clean up.
- `session-ownership`: which session may release or stop which registered server, and each worktree's own port.

### Modified Capabilities

## Impact

- `internal/hooks/core.go`, `internal/hooks/context.go`, `internal/hooks/core_test.go`
- `internal/app/app.go` (`MyPort` split), `internal/cli/port.go`, `internal/cli/hook_test.go`
- `internal/hooks/adapters/claude/claude.go`, `internal/hooks/adapters/cursor/cursor.go`, `cursor_test.go`
- `internal/registry/registry.go`: new `Store.LockTimeout` field used by `Update` — overlaps `fix-registry-lock-and-history` (see design.md)
- README.md "Ports" section
