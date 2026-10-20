## 1. Regression tests (write first, must fail)

- [x] 1.1 `internal/hooks/core_test.go`: `TestPreShellIgnoresDeadForeignEntries` — foreign entry port 3000 pid 4242, prober alive[4242]=false; `npm run dev -- --port 3000` and `kill 4242` are both Allow
- [x] 1.2 Update existing deny/ask tests in `core_test.go` (`TestPreShellDeniesKillOfForeignPort`, `...ForeignPid`, `TestPreShellBlindKillAsks`, `TestPreShellServerStartOnForeignPortDenied`, cursor ownership tests) to mark their entries' pids alive in `fakeProber`
- [x] 1.3 `core_test.go`: `TestHookPortHintDoesNotLoad` — hold the registry lock (or use a Store whose dir makes `Load` fail) and assert a strict-mode deny reason still contains the deterministic port, not "unknown", and returns in < 500 ms
- [x] 1.4 `core_test.go`: `TestSessionEndGivesUpOnHeldLock` — hold the lock; `sessionEnd` returns an error in < 2 s
- [x] 1.5 `internal/cli/commands_test.go`-style test in a new `internal/cli/port_test.go`: deterministic port registered to another session → stdout is the deterministic port, stderr names the fallback

## 2. Fix

- [x] 2.1 `internal/app/app.go`: extract `PortAmong`; `MyPort` calls it
- [x] 2.2 `internal/hooks/core.go` `preShell`: filter peeked entries by `Prober.PidAlive`; `myPort(s, entries)` uses `PortAmong` with no-op listening; pass entries through `denyPort`, `strictReason`
- [x] 2.3 `internal/hooks/context.go`: `sessionContext`, `wrapNudge` take entries and call `myPort(s, entries)`
- [x] 2.4 `internal/registry/registry.go`: `Store.LockTimeout` field; `Update` uses it when non-zero
- [x] 2.5 `core.go` `sessionEnd`: set `LockTimeout = time.Second`, `ProbeBudget = 300*time.Millisecond` before `Update`; update the `defaultKillTimeout` comment if the budget math changes
- [x] 2.6 `internal/cli/port.go`: print `ports.Deterministic(a.WorktreeKey(), a.Ports)`; if `a.MyPort()` differs, warn on stderr
- [x] 2.7 Delete `Events` in `internal/hooks/adapters/claude/claude.go:17` and `internal/hooks/adapters/cursor/cursor.go:28`
- [x] 2.8 `cursor.go` `FormatSession`: drop `Env` from `sessionOutput` and the env assignment; fix the doc comment. Update `cursor_test.go:91` (no env assertion) and `internal/cli/hook_test.go:134` (assert the attribution line in `additional_context` instead of the env key)
- [x] 2.9 `go test -race ./internal/hooks/... ./internal/app ./internal/cli ./internal/registry` green

## 3. Docs

- [x] 3.1 README.md "Ports": `port` always prints the deterministic port and warns when it is busy
