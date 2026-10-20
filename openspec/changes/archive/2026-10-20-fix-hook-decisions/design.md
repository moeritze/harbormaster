## Decisions

- **Liveness filter in preShell.** After `Peek`, keep only entries with `s.app.Prober.PidAlive(e.PID)` (one `kill(pid,0)` each; cheap). Applies to kill-deny, blind-kill ask and server-start deny. Session-start context keeps listing everything (informational).
- **Pure port hint.** Split `App.MyPort` into `App.PortAmong(entries []registry.Entry, listening func(int) bool) (int, error)` (the existing `taken` logic + `ports.Resolve`); `MyPort` = `Load` + `PortAmong(f.Entries, a.Prober.PortListening)`. Hooks call `PortAmong(peeked, func(int) bool { return false })`. `myPort(s)` becomes `myPort(s, entries)`; `wrapNudge`, `denyPort`, `strictReason`, `sessionContext` pass the peeked entries. Git discovery (`s.git()`) stays: the port key needs the worktree.
- **SessionEnd budget.** Add `LockTimeout time.Duration` to `registry.Store` (zero = 5 s), used by `Update` only. `sessionEnd` sets `a.Store.LockTimeout = 1s` and `a.Store.ProbeBudget = 300ms` before its `Update`. Alternative (raise the installed SessionEnd timeout) rejected: needs every user to reinstall and touches `internal/install` (owned by `fix-install-settings`).
- **hm port** uses `ports.Deterministic(a.WorktreeKey(), a.Ports)`; compares with `a.MyPort()` to warn.

## Risks / Trade-offs

- [PidAlive true for a reused pid] → still denies; conservative, same as today.
- **File overlap:** `internal/registry/registry.go` is also edited by `fix-registry-lock-and-history` (Peek/read/commit/prune plumbing). This change only adds one struct field and swaps `lockTimeout` for `s.lockWait()` in `Update`. Merge the later of the two by hand; no semantic conflict.
