## Why

`hm release` is a write path that can terminate processes, yet it lets one session release and kill another session's server without `--force` (#1), silently orphans a server whose termination failed (#12), and `hm run` accepts ports outside 0–65535 that can then never be released by port (#14).

## What Changes

- #1: `release` treats an entry as the caller's only when `ident.Owns` says so. Naming your own id with `--session` no longer makes a port-matched foreign entry "mine".
- #12: `release --kill` runs the kill safety guard before removing an entry; an entry that fails the guard or whose termination fails stays (or is restored) in the registry, and the command exits non-zero.
- #14: `run --port N` rejects N outside 1–65535 with a usage error (0 keeps meaning "default").

## Non-goals

- Changing `kill`'s behavior or the ownership rule itself (`ident.Owns`).
- Batch/partial-success semantics beyond "report every failure, exit non-zero".

## Capabilities

### New Capabilities
- `session-ownership`: which session may release or stop which registered server.
- `registry-storage`: what a registry entry may contain and how it is persisted.

### Modified Capabilities

## Impact

- `internal/cli/release.go`, `internal/cli/run.go` (`resolveRunPort`); tests in `internal/cli/commands_test.go`, `internal/cli/run_internal_test.go`.
- Reuses `guardEntry`, `restoreAfterFailedKill`, `terminateFn` from `internal/cli/kill.go` unchanged.
- README.md `release` section: `--kill` failure exit code.
