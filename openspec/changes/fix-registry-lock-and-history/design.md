## Context

All writers hold the flock and write via `atomicWrite` (temp + rename). `Peek` (hooks) and `History` use `readLockTimeout` = 1 s; hooks fail open on error.

## Decisions

- **Lock-free Peek.** Rename is atomic on the same filesystem, so an unlocked `os.ReadFile` sees either the old or new file. `Peek` must not call `quarantine` (moving a file needs the lock), so split `read()` into `readFile() (*File, error)` returning a typed parse error, used by `Peek`; `read()` (locked) wraps it and quarantines on that error. Symlink check stays.
- **Delete backfill rather than fix it.** Any read-time lookup cannot prove the pid was not reused between registration and the read; the only trustworthy start time is the one taken at registration (`cli.startTimeOf`). Unverifiable entries can still be killed singly, or dropped with `hm release`.
- **Batch history.** `appendHistoryLocked(recs ...HistoryRecord)`: read lines once, append all, cap, one `atomicWrite`. `AppendHistory` stays single-record for callers.
- **Version 0 → quarantine**, version > SchemaVersion → fail closed (unchanged).

## Risks / Trade-offs

- [Rows written by older builds without start time can no longer be group-killed after `hm ls`] → refusal message says to stop the pid directly or `hm release <port>`; such rows age out as their process dies.
- **File overlap:** `internal/runner/terminate.go` lines 28–32 (`ErrNoStartTime` message) are also in `fix-process-termination`'s file (it edits lines 70–84). Different hunks; merge is trivial. `fix-hook-decisions` adds a `LockTimeout` field to `Store` in `registry.go` — also a different hunk.
