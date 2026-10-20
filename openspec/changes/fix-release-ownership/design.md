## Context

`release` removes matched entries inside one `Store.Update`, then terminates them outside the lock. `kill` (kill.go) already solves the same ordering: guard first, remove, signal, `restoreAfterFailedKill` on failure.

## Decisions

- **#12: guard inside the Update callback, before removing.** With `--kill`, call `guardEntry(a, e)` for each matched, owned entry; on error keep the entry and record the failure. After the Update, terminate each removed entry; on error call `restoreAfterFailedKill(a, e, true)`. Exit `ExitDenied` if any entry failed. Alternative "just exit non-zero" leaves an unregistered live server, which is the bug.
- **Guard runs under the registry lock.** It forks `ps` per entry (start-time lookup). Acceptable: only with `--kill`, a handful of entries.
- **#1: delete the `session == a.Ident.Session` clause.** `ident.Owns` already returns true for `e.Session == a.Ident.Session`, so `release --session <own>` keeps working.

## Risks / Trade-offs

- [Partial release: some entries released, one refused] → all failures are printed, exit code non-zero; history records only actually-released entries.
