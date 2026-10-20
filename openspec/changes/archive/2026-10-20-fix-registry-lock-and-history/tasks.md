## 1. Regression tests (write first, must fail)

- [x] 1.1 Timing guard is the existing `TestPruneFiftyStaleEntriesIsFast` (prune_perf_test.go): reported failing on main (812 ms), measured 0.40–0.46 s vs the 0.5 s budget here, i.e. flaky. Add `TestCommitAppendsAllRecords` in `internal/registry/history_internal_test.go`: `commit` with 1 001 pruned records keeps the last 1 000 in order (pins the batched append's cap and order)
- [x] 1.2 `internal/registry/registry_test.go`: `TestPeekDoesNotWaitForLock` — hold `lock(registry.lock)` in the test, call `Peek`, want entries in < 100 ms
- [x] 1.3 `registry_test.go`: `TestPeekDoesNotQuarantine` — write `not json`, `Peek` returns error, file still present, no quarantine copy
- [x] 1.4 `registry_test.go`: `TestLoadQuarantinesVersionZero` — table `{}`, `null`, `{"entries":[]}`; `Load` succeeds empty and a quarantine copy exists. Keep `TestLoadRejectsUnknownVersion` (version 2 not quarantined)
- [x] 1.5 `internal/registry/prune_test.go`: replace `TestPruneBackfillsMissingStartTime` with `TestPruneNeverFillsStartTime` — entry without StartTime survives `Load`, `Update`, `Prune` with StartTime still empty on disk
- [x] 1.6 `internal/cli/run_kill_test.go`: rewrite `TestKillRefusesGroupSignalForEntryWithoutStartTime` — refusal persists after `hm ls`; drop the "names the repair `harbormaster ls`" assertion

## 2. Fix

- [x] 2.1 `history.go`: `appendHistoryLocked(recs ...HistoryRecord)` with one read and one `atomicWrite`; `registry.go` `commit` calls it once with `pruned...` (skip when empty)
- [x] 2.2 `registry.go`: split `read()` into unlocked `readFile()` (symlink check, ReadFile, Unmarshal, version check) and locked `read()` that quarantines on parse error or Version == 0
- [x] 2.3 `registry.go` `Peek`: no lock, `return s.readFile()`; drop the copy. `readLockTimeout` stays for `History`
- [x] 2.4 `prune.go`: delete `backfillStartTimes`, `maxBackfillPerCall`, `pruneOptions.startTimeOf`; `pruneEntries` returns only `[]HistoryRecord`
- [x] 2.5 `registry.go`: drop `filled` from `readPruned`, `Load`, `Prune`, `prune`; delete `Store.StartTimeOf` and its default in `Open`; delete `starttime_unix.go`, `starttime_windows.go`
- [x] 2.6 Remove `StartTimeOf` assignments in tests: `internal/registry/prune_test.go`, `internal/cli/kill_internal_test.go:28`, `run_kill_test.go:311`
- [x] 2.7 `internal/runner/terminate.go`: `ErrNoStartTime` text → "entry has no recorded start time; refusing a process-group signal (stop the process yourself, or drop the entry with harbormaster release <port>)" without the `hm ls` hint; update its comment
- [x] 2.8 `go test -race ./...` and `make lint` green

## 3. Docs

- [x] 3.1 README.md: quarantine section mentions version-0 files; pid-reuse guard section says entries without a start time are never group-signalled and are not repaired by `ls`
