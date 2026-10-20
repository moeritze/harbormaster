## Why

`hm run` promises that the server it starts never outlives it unregistered. Two gaps break that: a Ctrl-C/SIGHUP that arrives while `run` is still registering (which can wait 5 s for the lock) kills harbormaster but not the child in its own process group (#9), and group termination only watches the group leader, so a worker that ignores SIGTERM keeps the port after its `sh -c` parent exits (#10).

## What Changes

- #9: `runner.Run` subscribes to SIGINT/SIGTERM/SIGHUP before starting the child; a signal received during registration terminates the child's group.
- #10: `Terminate` with `group=true` waits until the whole process group is gone (not just the leader) before declaring success, and sends SIGKILL to the group when the timeout passes.

## Non-goals

- Windows process-tree termination (no process groups there; behavior unchanged).
- Changing timeouts.

## Capabilities

### New Capabilities
- `process-termination`: how harbormaster stops the processes it started or registered.

### Modified Capabilities

## Impact

- `internal/runner/runner.go` (`Run`), `internal/runner/terminate.go` (`Terminate`), `internal/runner/terminate_unix.go`, `internal/runner/terminate_windows.go` (new `gone` helper), `internal/runner/runner_test.go`.
- `fix-registry-lock-and-history` edits `ErrNoStartTime` in `terminate.go` lines 28–32 (different hunk).
