## Why

The registry is on every hook's and command's path. Under contention it is slow (#4: one full history rewrite + fsync per pruned record while holding the lock), hooks fail open after a 1 s lock wait (#5), a reused pid's start time can be persisted and later trusted for a process-group signal (#11), and a registry.json of `{}` or `null` bricks every command (#18).

## What Changes

- #4: `commit` appends all pruned records to history.jsonl in one rewrite.
- #5: `Peek` reads registry.json without taking the lock (writes are temp file + rename, so a read sees a whole file). A parse or version error is returned, never quarantined from `Peek`.
- #11: **BREAKING (behavioral)** — delete start-time backfill during prune. An entry without a recorded start time stays unverifiable: it can be signalled as a single pid, never as a group. The `ErrNoStartTime` message stops telling users to run `hm ls`.
- #18: a registry.json with version 0 (`{}`, `null`, missing `version`) is quarantined like unparsable JSON.
- Cleanup: `Peek` returns the freshly read document without copying it.

## Non-goals

- Changing the lock timeouts or the lock implementation.
- Making `History` lock-free.

## Capabilities

### New Capabilities
- `registry-storage`: what a registry entry may contain and how it is persisted.

### Modified Capabilities

## Impact

- `internal/registry/registry.go`, `history.go`, `prune.go`; delete `starttime_unix.go`, `starttime_windows.go`; tests in `internal/registry/*_test.go`.
- `internal/runner/terminate.go` lines 28–32 (`ErrNoStartTime` text + comment) — overlaps with `fix-process-termination`, see design.md.
- `internal/cli/run_kill_test.go` (`TestKillRefusesGroupSignalForEntryWithoutStartTime`), `internal/cli/kill_internal_test.go` (`st.StartTimeOf = nil`).
