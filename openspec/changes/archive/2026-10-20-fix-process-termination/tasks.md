## 1. Regression tests (write first, must fail)

- [x] 1.1 `internal/runner/runner_test.go`: `TestRunTerminatesGroupMemberThatIgnoresTERM` — args `sh -c "(trap '' TERM; exec sleep 30) </dev/null >/dev/null 2>&1 & echo $! > PIDFILE; exit 0"`, KillTimeout 1 s; after `Run` returns the worker pid is dead within 3 s (fails today: worker survives)
- [x] 1.2 `runner_test.go`: `TestTerminateGroupKillsTERMIgnoringWorker` — start the same `sh` with `Setpgid`, call `Terminate(pid, true, 500ms)`; worker dead afterwards
- [x] 1.3 `runner_test.go`: `TestRunSignalDuringRegisterStopsChild` — register func blocks on a channel; send `syscall.SIGHUP` to `os.Getpid()` while blocked, then unblock with an error; want `Run` to return and the child pid (captured in register) dead. Today the signal kills the test binary, so it fails

## 2. Fix

- [x] 2.1 `internal/runner/terminate_unix.go`: add `gone(pid int, group bool) bool` (ESRCH gone; EPERM gone only for a group target)
- [x] 2.2 `internal/runner/terminate_windows.go`: `gone(pid int, _ bool) bool { return !alive(pid) }`
- [x] 2.3 `internal/runner/terminate.go` `Terminate`: poll `gone(pid, group)` instead of `alive(pid)`; delete `alive` if no caller remains (grep first)
- [x] 2.4 `internal/runner/runner.go` `Run`: create `sigs`, `signal.Notify`, `defer signal.Stop` before `cmd.Start()`
- [x] 2.5 `go test -race ./internal/runner ./internal/cli` green (incl. `TestRunRegisterFailureReapsPromptly`, `TestTerminateGroupFallsBackForNonLeader`)

## 3. Docs

- [x] 3.1 README.md `run` section: a stopped server's whole process group is killed, including workers that ignore SIGTERM
