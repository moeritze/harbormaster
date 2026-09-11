# Harbormaster Hardening 2 Implementation Plan (Plan 5)

> **For agentic workers:** executed inline by the controller's fork (no subagents available to it). Steps use checkbox (`- [ ]`) syntax; each task is one commit with tests.

**Goal:** Close the open items from issues #2 and #4 and the Plan 2 deferred minors that need no further design decision: secret redaction, prune performance, history reasons, pid-reuse safety, corrupt-registry recovery, fixed tool paths, Windows/BSD build fixes, `$PORT` warning, timestamps, classifier miss, injectable env.

**Architecture:** No new packages except `internal/tools` (absolute tool path resolution). Registry pruning batches port probes (one shared 100 ms re-probe delay, bounded concurrency) instead of sleeping per entry; `Remove` takes its target before pruning; `read()` quarantines a corrupt file instead of failing every command. Entries gain `start_time` (from `ps -o lstart=`) so a signal is refused when the pid was reused.

**Tech Stack:** Go 1.27, existing deps only.

**Spec:** `docs/superpowers/specs/2026-09-10-harbormaster-design.md` §5, §6, §10.

## Global Constraints
- Lint 0 (golangci-lint v2.13.2), `go test -race ./...`, coverage ≥ 80 %, `make smoke`, `GOOS=windows`, `GOOS=linux`, `GOOS=freebsd` builds.
- No shell invocations; tools by absolute path (`/bin/ps`, `/usr/bin/ps`, `/usr/sbin/lsof`, `/usr/bin/lsof`, `/usr/bin/git`) with `$PATH` fallback.
- Commits: conventional subject + `Co-Authored-By: Claude Code <noreply@anthropic.com>`; never `Claude-Session:`.
- Do not touch `internal/hooks/adapters/*`, `internal/install`, `cli/install.go`, `cli/hook.go`, `.goreleaser.yaml`, `release.yml`, README Install / Claude Code sections, SECURITY.md supply-chain section.

---

### Task 1: RedactCmd covers URL credentials, auth headers, wider keys; per-argument truncation
- [ ] Tests in `internal/ident/ident_test.go`: `postgres://alice:swordfish@db/app` → `postgres://***@db/app`; `-H "Authorization: Bearer sk-abc"` → `Authorization: ***`; `Cookie: session=x` masked; `--auth=abc`, `DSN=…@…`, `API_URL=https://u:p@h` masked; `--port 3000` untouched; a 300-byte harmless arg is truncated on its own without hiding a later `--password=x` (still `***`).
- [ ] Implement: per-arg pipeline `redactURLCreds → redactHeader → assignment/flag masking → Sanitize`; join; cap total at 2048 bytes rune-safe with `…`.
- [ ] Commit `fix(ident): redact URL credentials and auth headers; truncate per argument`.

### Task 2: Batched prune probes; Remove before prune; history reasons
- [ ] Tests in `internal/registry`: 50 entries past grace with closed ports prune in < 500 ms with the real 100 ms delay; `Remove` of a dead-pid entry returns found=true and the caller's reason wins; probe cap (entries beyond 200 stay untouched this call).
- [ ] Implement `pruneEntries`: pid check sequential; port candidates probed concurrently (semaphore 8), one shared `reprobeDelay` sleep, second pass only for first-pass failures; `maxProbePerCall = 200`.
- [ ] `Remove`: `read()` → extract id → `prune` remaining → `commit`.
- [ ] `scripts/smoke.sh`: assert `history | grep -q killed` and exactly one record.
- [ ] Commit `perf(registry): batch port probes; Remove takes its target before pruning`.

### Task 3: pid-reuse guard and group-leader check
- [ ] `liveness.PidStartTime(pid) (string, error)` (unix: `ps -o lstart=`; windows: "", nil). `registry.Entry.StartTime string json:"start_time,omitempty"` set by `cli.newEntry` and `run`'s register.
- [ ] `runner.CheckStartTime(pid int, stored string, now func(int)(string,error)) error` refuses when stored is non-empty and differs. Used by `cli.terminateEntry` and `hooks.sessionEnd`.
- [ ] `terminate_unix.go`: when `group` and `unix.Getpgid(pid) != pid`, signal the single pid instead.
- [ ] Tests: start-time mismatch refused; Getpgid fallback covered by a spawned child without Setpgid.
- [ ] Commit `fix(runner): refuse signals when the pid was reused; group-signal only a leader`.

### Task 4: Corrupt registry quarantine; shorter read lock
- [ ] `read()`: parse error or unknown version → rename to `registry.json.corrupt-<unix>`, warn once to `Store.Warn` (default stderr), return empty document. Symlink refusal unchanged.
- [ ] `Peek`/`History` use `readLockTimeout = 1 s`.
- [ ] Tests for both. Commit `fix(registry): quarantine a corrupt registry; 1 s lock wait for reads`.

### Task 5: Fixed tool paths; git timeout and single call
- [ ] `internal/tools`: `Path(name) string` resolving once (`sync.Once` per name) from fixed candidates then `exec.LookPath`; empty when absent.
- [ ] `liveness` uses `tools.Path("ps")` (empty → error → Guard fails closed) and `tools.Path("lsof")`; `gitctx` uses `tools.Path("git")`, one `git rev-parse --show-toplevel --git-common-dir --abbrev-ref HEAD` with a 3 s `exec.CommandContext` timeout.
- [ ] Tests: `tools.Path("ps")` absolute; gitctx still passes. Commit `fix(liveness,gitctx): resolve ps/lsof/git to fixed paths; git timeout`.

### Task 6: Windows and BSD builds
- [ ] `//go:build unix` on `internal/cli/run_kill_test.go` and `internal/runner/runner_test.go`; `stdinIsTerminal` moves into `tty_linux.go`/`tty_darwin.go`; `tty_other.go` (`unix && !linux && !darwin`) returns false. `GOOS=windows go vet ./...` and `GOOS=freebsd go build ./...` pass.
- [ ] Commit `build: unix-only tests gated; BSD tty fallback`.

### Task 7: `$PORT` warning, timestamps, wording, classifier, env injection, small tests
- [ ] `resolveRunPort`: when `$PORT` chose the port and it differs from the deterministic port, warn on stderr.
- [ ] `history`: `At.Local().Format("2006-01-02 15:04 MST")`. `shortPath("/wt")` → `wt`. Unified `--force` hint in kill/release.
- [ ] `detect`: `hm run … --` blanks only the wrapper prefix so `hm run -- kill 1234` classifies as Kill.
- [ ] `hooks.NewWithEnv(a, getenv)`; `New` delegates with `os.Getenv`.
- [ ] Tests: `TestResolveRunPortUsesPortEnv` asserts the registered port; 60 s grace boundary; nil prober; corrupt history line.
- [ ] Commit `fix(cli,detect,hooks): PORT warning, local timestamps, wrapper-only blanking, injectable env`.

## Self-review
Covers issue #4 medium items (redaction, prune, pid reuse, corrupt recovery) and low items (tool paths, `$PORT`, timestamps, Windows note via build gating), issue #2 items (Remove/history, Windows tests, freebsd, history >1 MiB already done, git timeout/single call, wording, shortPath, tests), Plan 2 minors (`hm run -- kill`, injectable env). Deferred with a comment: `run` re-registration/last_seen, cross-process concurrency test, tcsetpgrp handoff, `getenv` fold into App, CI concurrency group (outside the allowed ci.yml scope), postShell tool-result check (payload field not confirmed), release pipeline (Plan 4), CODEOWNERS/harden-runner/scorecard token (repo settings).
