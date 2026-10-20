## 1. Regression tests (write first, must fail)

- [x] 1.1 `internal/cli/commands_test.go`: `TestReleaseOwnSessionFlagDoesNotOwnForeignPort` — seed port 3001 owned by session s2, caller s1 runs `release 3001 --session s1 --kill` with a fake `a.Terminate`; want exit 1, entry kept, Terminate not called
- [x] 1.2 `commands_test.go`: `TestReleaseKillKeepsEntryWhenGuardRefuses` — own spawned entry with empty StartTime and `PidStartTime` returning a value (guard returns `runner.ErrNoStartTime`); want non-zero exit, entry still registered, no "released" history record
- [x] 1.3 `commands_test.go`: `TestReleaseKillRestoresEntryWhenTerminateFails` — fake `a.Terminate` returns error; want non-zero exit and entry restored
- [x] 1.4 `internal/cli/run_internal_test.go`: `run --port 70000` and `--port -5` exit `ExitUsage` and register nothing

## 2. Fix

- [x] 2.1 `internal/cli/release.go`: `mine := ident.Owns(a.Ident, e, a.Git.Worktree)` (drop the `--session` clause; update the comment)
- [x] 2.2 `release.go`: when `kill`, call `guardEntry(a, e)` inside the Update callback before appending to `removed`; on error keep the entry and collect the error
- [x] 2.3 `release.go`: after Update, on `terminateEntry` failure call `restoreAfterFailedKill(a, e, true)` and collect the error (use `terminateFn(a)` directly since the guard already ran); skip the "released" history record for that entry
- [x] 2.4 `release.go`: return `exitf(ExitDenied, ...)` naming each failed port when any guard/terminate failed, after printing output
- [x] 2.5 `internal/cli/run.go` `resolveRunPort`: `if flag < 0 || flag > 65535 { return 0, exitf(ExitUsage, "invalid port %d", flag) }` at the top
- [x] 2.6 `go test -race ./internal/cli` green

## 3. Docs

- [x] 3.1 README.md `release` section: `--kill` leaves an entry registered and exits non-zero when it cannot stop the process
