## Decisions

- **Poll the group, not the leader.** Add `gone(pid int, group bool) bool` per platform. Unix: `syscall.Kill(target(pid, group), 0)`; `ESRCH` = gone; for a group target also treat `EPERM` as gone (the darwin zombie-leader quirk `sendKill` already documents). Non-group keeps today's `alive` semantics (`EPERM` = alive). Windows: `!alive(pid)`. `Terminate` loops on `!gone(pid, group)`.
- **Notify before Start.** Move `sigs`/`signal.Notify`/`defer signal.Stop` above `cmd.Start()`. After a failed `register`, still terminate as today. After a successful one, the existing `select` sees a signal that arrived during registration because `sigs` is buffered (cap 2).
- Alternative for #9 (register before start) rejected: the entry needs the child's pid.

## Risks / Trade-offs

- [A group with a TERM-ignoring worker now costs the full KillTimeout (10 s) on natural leader exit, inside the reaper goroutine] → intended: that is the case that leaked before.
- [kill(-pid,0) after the leader is reaped could name a reused group id] → same exposure as the existing post-exit `Terminate(pid, true)` sweep, which already runs before the reap is published (runner.go comment).
