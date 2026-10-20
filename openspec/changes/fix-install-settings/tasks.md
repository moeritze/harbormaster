## 1. Regression tests (write first, must fail)

- [ ] 1.1 `internal/install/claude_test.go`: `TestUninstallKeepsForeignCommandInSharedEntry` — seed `PreToolUse: [{matcher: Bash, hooks: [ours, {type: command, command: my-guard.sh}]}]`; after `ClaudeUninstall` the entry exists with only `my-guard.sh`
- [ ] 1.2 `claude_test.go`: `TestInstallDoesNotEscapeHTML` — user hook `cd x && make lint`; after `Claude` install the raw file contains `&&`, not `&`
- [ ] 1.3 `internal/install/cursor_test.go`: `TestAgentsMDKeepsFileMode` — AGENTS.md 0644 keeps 0644 after `AgentsMD` and `AgentsMDUninstall` and leaves no temp file in the directory. Passes today; it guards the atomic rewrite in 2.5 (atomicity itself is not observable in a test)

## 2. Fix

- [ ] 2.1 `internal/install/claude.go` `ClaudeUninstall`: for each entry, filter its `hooks` array with the per-command check now in `isOurs`; drop the entry only when the filtered array is empty; record the event in `Removed` when anything was filtered
- [ ] 2.2 Split `isOurs` into `isOurCommand(h any) bool` (used by uninstall filter) and keep `isOurs(entry)` for install's "already present" check
- [ ] 2.3 `internal/install/settings.go` `write`: `json.NewEncoder(&buf)` with `SetEscapeHTML(false)`, `SetIndent("", "  ")` (Encode already appends `\n`)
- [ ] 2.4 Extract `writeAtomic(path string, b []byte, perm os.FileMode) error` from `settings.write` (CreateTemp in `filepath.Dir(path)`, write, chmod, close, rename); `settings.write` calls it; delete `dirOf`
- [ ] 2.5 `internal/install/agentsmd.go`: both `os.WriteFile` calls → `writeAtomic(path, ..., mode)` where mode is the existing file's perm or 0o600 when new
- [ ] 2.6 `go test -race ./internal/install ./internal/cli` green
