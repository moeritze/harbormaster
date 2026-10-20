## Why

Commands are stored in registry.json/history.jsonl and shown to other sessions, so secrets in them must be masked; several common forms still leak (#13). Git discovery from a subdirectory of a main checkout computes the wrong repo (#17), which breaks worktree identity. Long worktree paths render without their base name. A few one-line wrappers and a redundant sanitize pass add code without value.

## What Changes

- #13: also mask attached header flags (`-HAuthorization: …`, `--header=Authorization: …`), `--cookie`/`-b` values, keys containing `cookie`, `signature`, or a bare `pass`, and JSON string values containing escaped quotes.
- #17: `git rev-parse --path-format=absolute` so `--git-common-dir` is never relative to the cwd.
- Render worktree paths as `Text(ShortPath(p))` (shorten first, then sanitize/truncate) in `render.OwnerLine` and `cli history`.
- Cleanup: delete `cli/output.go` wrappers `shortSession`, `age`, `renderMax` (only `output_test.go` uses them; `render_test.go` covers the same) and `ident.SanitizeN` (its only caller re-caps `RedactCmd` output, already sanitized and capped).

## Non-goals

- Shell-aware redaction; it stays per-argument pattern matching that over-redacts when unsure.
- Supporting git older than 2.31.

## Capabilities

### New Capabilities
- `command-redaction`: which parts of a stored or displayed command line are masked.
- `git-context`: how repo, worktree and branch are discovered and displayed.

### Modified Capabilities

## Impact

- `internal/ident/ident.go`, `ident_test.go`; `internal/gitctx/gitctx.go`, `gitctx_test.go`; `internal/render/render.go`, `render_test.go`
- `internal/cli/output.go`, `output_test.go`, `history.go`, `claim.go`
