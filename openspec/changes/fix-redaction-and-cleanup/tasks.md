## 1. Regression tests (write first, must fail)

- [ ] 1.1 `internal/ident/ident_test.go` `RedactCmd` table: `-HAuthorization: Basic dXNlcjpw`, `--header=Authorization: Basic dXNlcjpw`, `--cookie sid=abc`, `-b sid=abc`, `--pass hunter2`, `https://s3/x?X-Amz-Signature=deadbeef`, `{"password":"x\"y"}` → no secret substring in output
- [ ] 1.2 `internal/gitctx/gitctx_test.go`: `TestDiscoverFromSubdir` — `git init` temp repo, mkdir `sub`, `Discover(sub)` returns Repo == Worktree == repo root (EvalSymlinks'd)
- [ ] 1.3 `internal/render/render_test.go`: `TestOwnerLineKeepsBaseNameOfLongPath` — worktree of 30 `/segment` components ending in `/wt-name` (longer than 120 bytes); `OwnerLine` contains `wt-name`

## 2. Fix

- [ ] 2.1 `internal/ident/ident.go`: `sensitiveKey` add `cookie|signature|pass\b`; `shortSecretFlag` → `^-[pub]$`; `jsonPair` value group → `((?:[^"\\]|\\.)*)` (double-quoted; keep single-quote form separately or drop it if no test needs it)
- [ ] 2.2 `redactArg`: handle `^(-H|--header=)(.+)$` by running `authHeader` on the remainder before the `assignment` branch
- [ ] 2.3 `internal/gitctx/gitctx.go` `Discover`: add `--path-format=absolute` first in rev-parse args; delete the relative-join branch (lines 44–46)
- [ ] 2.4 `internal/render/render.go:83` → `Text(ShortPath(e.Worktree))`; `internal/cli/history.go:29` → `render(shortPath(r.Worktree))`
- [ ] 2.5 `internal/cli/output.go`: delete `renderMax`, `shortSession`, `age`; delete their tests in `output_test.go` (`TestAgeBuckets`, `TestShortSession`) and replace `renderMax` uses with `120` or drop the duplicate of `render_test.TestTextSanitizesAndTruncates`
- [ ] 2.6 `internal/cli/claim.go`: `Cmd: cmdLine`; delete `maxStoredCmd` and its comment; delete `ident.SanitizeN` and `TestSanitizeN` (grep first: only caller is claim.go:99; `cmdLine` comes from `ident.RedactCmd` via runner.go or a fixed `claimed pid N` string)
- [ ] 2.7 `go test -race ./internal/...` and `make lint` green

## 3. Docs

- [ ] 3.1 README.md: redaction list (cookies, signatures, attached headers); requirements: git ≥ 2.31
