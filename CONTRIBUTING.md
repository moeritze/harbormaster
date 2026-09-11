# Contributing

1. Open an issue before large changes.
2. Fork, branch from `main`, keep PRs focused.
3. `make test lint coverage-check` must pass. CI enforces the same.
4. Tests first. Every behavior change needs a test.
5. Dependencies: cobra, oklog/ulid, golang.org/x/sys only. Adding one needs
   justification in the PR.
6. Hook definitions and agent config templates in `adapters/` may only invoke
   the `harbormaster` binary. No inline shell.
7. Conventional commit prefixes: feat, fix, test, ci, docs, chore, refactor.
