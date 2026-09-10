# Harbormaster Core CLI + Repo Security Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `harbormaster` Go CLI with a self-cleaning port registry, the `run` process wrapper, ownership-safe `kill`, and a public GitHub repo whose CI and security posture match spec §12 from the first push.

**Architecture:** Single Go module. A flock-guarded JSON registry under `$HARBORMASTER_HOME` is the only state. Every read prunes dead entries using a `Prober` interface (pid alive + port listening) so tests can inject fakes. Cobra subcommands are thin; logic lives in `internal/*` packages with one responsibility each. Hooks and agent adapters are Plan 2/3 and are not built here.

**Tech Stack:** Go 1.27, `github.com/spf13/cobra v1.10.2`, `github.com/oklog/ulid/v2 v2.1.2`, `golang.org/x/sys v0.48.0`. GitHub Actions with SHA-pinned actions, golangci-lint v2.13.2 (gosec enabled), govulncheck v1.8.0, CodeQL, OpenSSF Scorecard, Dependabot.

**Spec:** `docs/superpowers/specs/2026-09-10-harbormaster-design.md` (this plan covers spec §5, §6, §7, §10, §11, §12, §13, milestones 1, 2, 6)

## Global Constraints

- Module path `github.com/moeritze/harbormaster`, Go `1.27`, license MIT.
- Dependencies limited to cobra, oklog/ulid, golang.org/x/sys. Anything else needs a note in the PR.
- Registry dir default `$XDG_STATE_HOME/harbormaster`, fallback `~/.local/state/harbormaster`, override `HARBORMASTER_HOME`. Dir mode `0700`, files `0600`.
- Registry schema version `1`, fields exactly as spec §5.
- Exit codes: 0 ok, 1 foreign/denied, 2 unregistered conflict, 3 usage error, 4 registry/lock error.
- Port defaults `HARBORMASTER_BASE=3000`, `HARBORMASTER_RANGE=1000`.
- Lock wait timeout 5 s. Listen wait timeout 60 s. Graceful kill timeout 10 s.
- `history.jsonl` capped at 1000 lines.
- Labels and session ids sanitized to printable, max 256 bytes.
- Never `sudo`. `kill --force` still refuses pid 1, own pid, other-uid pid.
- No network calls at runtime. No telemetry.
- All GitHub Actions pinned to full commit SHAs. Top-level `permissions: contents: read`.
- Coverage ≥ 80 % on `./internal/...`.
- Commits: conventional prefix (`feat:`, `test:`, `ci:`, `docs:`, `chore:`). No AI co-author trailers.
- Work happens on a feature branch in a git worktree (`superpowers:using-git-worktrees`), merged to `main` via PR once the repo exists on GitHub (Task 13). Until then, commit to the branch.

---

## File Structure

```
go.mod / go.sum
Makefile
.gitignore
LICENSE                        MIT
README.md                      short, usage, install, security note
SECURITY.md  CONTRIBUTING.md  CODE_OF_CONDUCT.md
.golangci.yml
.github/dependabot.yml
.github/workflows/ci.yml       test matrix, lint, govulncheck, coverage gate
.github/workflows/codeql.yml
.github/workflows/scorecard.yml
.github/workflows/dependency-review.yml
cmd/harbormaster/main.go       builds App, runs cobra root, maps errors to exit codes
internal/app/app.go            App struct: Store, Prober, Identity, Git, Stdout, Stderr, Now
internal/cli/root.go           cobra root + version
internal/cli/ls.go  port.go  check.go  claim.go  release.go  gc.go  history.go  run.go  kill.go
internal/cli/output.go         table + json rendering
internal/cli/errors.go         ExitError type
internal/registry/registry.go  Entry, File, Store, Update, Load
internal/registry/lock.go      flock with timeout
internal/registry/history.go   append + cap
internal/registry/prune.go     prune with grace period
internal/liveness/liveness.go  Prober interface, OS implementation (dial-based port check)
internal/liveness/pid_unix.go  PidAlive, PidUID, PidOnPort via lsof
internal/liveness/pid_windows.go best-effort stubs
internal/gitctx/gitctx.go      Discover(dir) → Repo, Worktree, Branch
internal/ports/ports.go        Deterministic, Resolve, Config
internal/ident/ident.go        Detect, Owns, Sanitize, RedactCmd
internal/runner/runner.go      Run: spawn, env inject, pgid, signals, register/unregister
internal/runner/terminate.go   Terminate(pid, group, timeout), Guard(pid)
```

---

### Task 1: Module scaffold, root command, Makefile

**Files:**
- Create: `go.mod`, `.gitignore`, `Makefile`, `LICENSE`, `README.md`
- Create: `cmd/harbormaster/main.go`, `internal/cli/root.go`, `internal/cli/errors.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Produces: `cli.NewRoot(a *app.App) *cobra.Command`; `cli.ExitError{Code int, Msg string}` implementing `error`; `app.App` struct (fields filled in later tasks, starts with `Stdout, Stderr io.Writer`, `Version string`).

- [ ] **Step 1: Init module and deps**

```bash
cd <worktree>
go mod init github.com/moeritze/harbormaster
go get github.com/spf13/cobra@v1.10.2 github.com/oklog/ulid/v2@v2.1.2 golang.org/x/sys@v0.48.0
```

- [ ] **Step 2: Write `.gitignore`, `LICENSE`, `Makefile`**

`.gitignore`:
```
bin/
coverage.out
dist/
```

`LICENSE`: MIT text, `Copyright (c) 2026 Moritz Röseler`.

`Makefile`:
```make
BIN := harbormaster
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/moeritze/harbormaster/internal/cli.Version=$(VERSION)

.PHONY: build test lint coverage-check install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/harbormaster

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

coverage-check:
	go test -race -coverprofile=coverage.out ./internal/...
	@go tool cover -func=coverage.out | tail -1 | awk '{gsub("%","",$$3); if ($$3+0 < 80) {print "coverage " $$3 "% is below 80%"; exit 1} else print "coverage " $$3 "%"}'

install: build
	install -d $(HOME)/.local/bin
	install -m 0755 bin/$(BIN) $(HOME)/.local/bin/$(BIN)
	ln -sf $(HOME)/.local/bin/$(BIN) $(HOME)/.local/bin/hm

clean:
	rm -rf bin coverage.out
```

- [ ] **Step 3: Write failing test for root/version**

`internal/cli/root_test.go`:
```go
package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moeritzeharbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/cli"
)

func TestVersionPrintsVersion(t *testing.T) {
	var out bytes.Buffer
	a := &app.App{Stdout: &out, Stderr: &out}
	cli.Version = "1.2.3-test"
	root := cli.NewRoot(a)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "harbormaster 1.2.3-test") {
		t.Fatalf("got %q", out.String())
	}
}
```
(Fix the typo in the first import path to `github.com/moeritze/harbormaster/internal/app` — it is written wrong above on purpose so you check imports compile.)

- [ ] **Step 4: Run test, expect compile failure**

Run: `go test ./internal/cli/ -run TestVersionPrintsVersion`
Expected: FAIL, package `app` and `cli` do not exist.

- [ ] **Step 5: Implement `app.App`, `cli.ExitError`, root**

`internal/app/app.go`:
```go
// Package app wires the dependencies every command needs.
package app

import (
	"io"
	"time"
)

// App carries injected dependencies. Later tasks add Store, Prober, Identity, Git.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

// Clock returns the current time via the injected clock.
func (a *App) Clock() time.Time { return a.now() }
```

`internal/cli/errors.go`:
```go
package cli

import "fmt"

// Exit codes per spec §6.5.
const (
	ExitOK           = 0
	ExitDenied       = 1
	ExitUnregistered = 2
	ExitUsage        = 3
	ExitRegistry     = 4
)

// ExitError carries a process exit code and a single actionable message.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

func exitf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Msg: fmt.Sprintf(format, args...)}
}
```

`internal/cli/root.go`:
```go
// Package cli defines the harbormaster cobra commands.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

// Version is set via -ldflags at build time.
var Version = "dev"

// NewRoot builds the root command with all subcommands attached.
func NewRoot(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:           "harbormaster",
		Short:         "Registry of local dev servers: who owns which port, from which worktree",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(a.Stdout, "harbormaster %s\n", Version)
			return err
		},
	})
	return root
}
```

`cmd/harbormaster/main.go`:
```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/cli"
)

func main() {
	a := &app.App{Stdout: os.Stdout, Stderr: os.Stderr}
	root := cli.NewRoot(a)
	if err := root.Execute(); err != nil {
		var ee *cli.ExitError
		if errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, ee.Msg)
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitUsage)
	}
}
```

- [ ] **Step 6: Run test, expect pass; build binary**

Run: `go test ./internal/cli/ -run TestVersionPrintsVersion && make build && ./bin/harbormaster version`
Expected: PASS, prints `harbormaster <git describe>`.

- [ ] **Step 7: Write README stub**

`README.md`:
```markdown
# harbormaster

Registry of local dev servers for multi-session, multi-worktree development.
Knows which port belongs to which worktree, which agent session started it,
and for what task. Agents stop killing each other's servers.

Not affiliated with Phabricator's Harbormaster.

## Install

    go install github.com/moeritze/harbormaster/cmd/harbormaster@latest

`make install` also links `hm` as a short alias.

## Usage

    hm run --label "auth flow" -- npm run dev   # start a server on this worktree's port
    hm ls                                       # who owns what
    hm port                                     # this worktree's deterministic port
    hm check 3000                               # is 3000 free, mine, or foreign?
    hm kill 3000                                # only your own; --force for others

## Security

See SECURITY.md. No network access, no telemetry. State lives in
`~/.local/state/harbormaster` with mode 0700.
```

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: scaffold module, root command, Makefile"
```

---

### Task 2: Repo hygiene and CI workflows

**Files:**
- Create: `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `.golangci.yml`, `.github/dependabot.yml`
- Create: `.github/workflows/ci.yml`, `.github/workflows/codeql.yml`, `.github/workflows/scorecard.yml`, `.github/workflows/dependency-review.yml`

**Interfaces:**
- Produces: `make lint` and `make coverage-check` used by CI; every later task must keep them green.

- [ ] **Step 1: Write `SECURITY.md`**

```markdown
# Security Policy

## Reporting

Report vulnerabilities privately via GitHub's "Report a vulnerability" button
on this repository (Security tab). Do not open public issues for security
problems.

You will get an acknowledgement within 3 business days and a fix or
mitigation plan within 14 days for confirmed issues.

## Scope

- The `harbormaster` binary and its handling of the registry, signals, and
  hook input.
- Install/uninstall changes to agent configuration files.

## Runtime guarantees

- Never runs with elevated privileges.
- Never signals a process not in its registry unless `--force`, and even then
  refuses pid 1, its own pid, and processes owned by another user.
- No network access. No telemetry.
- Registry files are mode 0600 in a 0700 directory.

## Supply chain

Releases are signed with Sigstore cosign (keyless, GitHub OIDC) and ship an
SBOM and SLSA provenance. Verify with `cosign verify-blob` as documented in
the release notes, or build from source with `go install`.
```

- [ ] **Step 2: Write `CONTRIBUTING.md`**

```markdown
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
```

- [ ] **Step 3: Write `CODE_OF_CONDUCT.md`**

Contributor Covenant v2.1 full text. Contact: the private vulnerability reporting channel is not for conduct issues; use the maintainer's GitHub profile email.

- [ ] **Step 4: Write `.golangci.yml`**

```yaml
version: "2"
linters:
  default: standard
  enable:
    - gosec
    - misspell
    - revive
    - unconvert
    - unparam
  settings:
    gosec:
      excludes:
        - G204 # subprocess launched with variable: harbormaster's whole job is running user commands; reviewed per call site
formatters:
  enable:
    - gofmt
    - goimports
```

- [ ] **Step 5: Write `.github/dependabot.yml`**

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule:
      interval: weekly
    groups:
      go-deps:
        patterns: ["*"]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
    groups:
      actions:
        patterns: ["*"]
```

- [ ] **Step 6: Write `.github/workflows/ci.yml`**

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

env:
  GOFLAGS: -mod=readonly

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - name: Harden runner
        if: runner.os == 'Linux'
        uses: step-security/harden-runner@e14015d583714f6e62063499dc959a02595150a1 # v2.21.1
        with:
          egress-policy: audit
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: make test
      - run: make coverage-check
      - run: go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@e14015d583714f6e62063499dc959a02595150a1 # v2.21.1
        with:
          egress-policy: audit
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a # v9.3.0
        with:
          version: v2.13.2

  windows-best-effort:
    runs-on: windows-latest
    continue-on-error: true
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - run: go build ./... && go test ./...
```

- [ ] **Step 7: Write `.github/workflows/codeql.yml`**

```yaml
name: codeql

on:
  push:
    branches: [main]
  pull_request:
  schedule:
    - cron: "0 6 * * 1"

permissions:
  contents: read

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: github/codeql-action/init@b96794f015dfd88f77b49b1c93e0fa7110f94c63 # v4.38.0
        with:
          languages: go
      - uses: github/codeql-action/analyze@b96794f015dfd88f77b49b1c93e0fa7110f94c63 # v4.38.0
```

- [ ] **Step 8: Write `.github/workflows/scorecard.yml`**

```yaml
name: scorecard

on:
  branch_protection_rule:
  schedule:
    - cron: "0 7 * * 1"
  push:
    branches: [main]

permissions: read-all

jobs:
  analysis:
    runs-on: ubuntu-latest
    permissions:
      security-events: write
      id-token: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: ossf/scorecard-action@2d1146689b8cda280b9bc96326124645441f03bc # v2.4.4
        with:
          results_file: results.sarif
          results_format: sarif
          publish_results: true
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: SARIF file
          path: results.sarif
          retention-days: 5
      - uses: github/codeql-action/upload-sarif@b96794f015dfd88f77b49b1c93e0fa7110f94c63 # v4.38.0
        with:
          sarif_file: results.sarif
```

- [ ] **Step 9: Write `.github/workflows/dependency-review.yml`**

```yaml
name: dependency-review

on: pull_request

permissions:
  contents: read

jobs:
  dependency-review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/dependency-review-action@a1d282b36b6f3519aa1f3fc636f609c47dddb294 # v5.0.0
        with:
          fail-on-severity: moderate
```

- [ ] **Step 10: Validate locally**

Run:
```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
make lint
make coverage-check
```
Expected: lint clean. Coverage gate currently fails (cli has ~1 test). That is expected until Task 9; note it, continue. `for f in .github/workflows/*.yml; do python3 -c "import yaml,sys; yaml.safe_load(open('$f'))" && echo "$f ok"; done` prints ok for all four.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "ci: add security policy, lint config, CI, CodeQL, Scorecard, dependency review"
```

---

### Task 3: Registry storage (schema, lock, atomic write, history)

**Files:**
- Create: `internal/registry/registry.go`, `internal/registry/lock.go`, `internal/registry/history.go`
- Test: `internal/registry/registry_test.go`

**Interfaces:**
- Produces:
  ```go
  type Entry struct { ID string; Port int; PID int; Cmd, Repo, Worktree, Branch, Agent, Session, Label string; StartedAt time.Time; HostUser string }
  type File struct { Version int; Entries []Entry }
  type HistoryRecord struct { Entry; Reason string; At time.Time }
  type Store struct { /* unexported */ }
  func Open(dir string, p Prober, now func() time.Time) (*Store, error)
  func (s *Store) Load() (*File, error)                       // lock, read, prune, write-if-pruned
  func (s *Store) Update(fn func(f *File) error) error         // lock, read, prune, fn, atomic write
  func (s *Store) History(limit int) ([]HistoryRecord, error)
  func (s *Store) Dir() string
  type Prober interface { PidAlive(pid int) bool; PortListening(port int) bool }
  ```
  Prune itself is implemented in Task 5; this task ships `Store` with a no-op prune hook so tests pass.

- [ ] **Step 1: Write failing tests**

`internal/registry/registry_test.go`:
```go
package registry_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

type alwaysAlive struct{}

func (alwaysAlive) PidAlive(int) bool      { return true }
func (alwaysAlive) PortListening(int) bool { return true }

func fixedNow() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

func open(t *testing.T) *registry.Store {
	t.Helper()
	s, err := registry.Open(filepath.Join(t.TempDir(), "hm"), alwaysAlive{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenCreatesDirWithMode0700(t *testing.T) {
	s := open(t)
	st, err := os.Stat(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}

func TestLoadEmptyReturnsVersion1(t *testing.T) {
	f, err := open(t).Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || len(f.Entries) != 0 {
		t.Fatalf("got %+v", f)
	}
}

func TestUpdatePersistsAndFileMode0600(t *testing.T) {
	s := open(t)
	err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "a", Port: 3000, PID: 1, StartedAt: fixedNow()})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.Load()
	if len(f.Entries) != 1 || f.Entries[0].Port != 3000 {
		t.Fatalf("got %+v", f.Entries)
	}
	st, _ := os.Stat(filepath.Join(s.Dir(), "registry.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "registry.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestConcurrentUpdatesUnderLockProduceValidFile(t *testing.T) {
	s := open(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Update(func(f *registry.File) error {
				f.Entries = append(f.Entries, registry.Entry{ID: string(rune('a' + i)), Port: 3000 + i, PID: 1, StartedAt: fixedNow()})
				return nil
			})
		}(i)
	}
	wg.Wait()
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 20 {
		t.Fatalf("expected 20 entries, got %d", len(f.Entries))
	}
}

func TestHistoryAppendAndCap(t *testing.T) {
	s := open(t)
	for i := 0; i < 1005; i++ {
		if err := s.AppendHistory(registry.HistoryRecord{Entry: registry.Entry{ID: "x", Port: i}, Reason: "test", At: fixedNow()}); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1000 {
		t.Fatalf("expected 1000, got %d", len(recs))
	}
	if recs[0].Port != 5 {
		t.Fatalf("oldest should be trimmed, first port %d", recs[0].Port)
	}
	last, _ := s.History(2)
	if len(last) != 2 || last[1].Port != 1004 {
		t.Fatalf("limit: %+v", last)
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	s := open(t)
	if err := os.WriteFile(filepath.Join(s.Dir(), "registry.json"), []byte(`{"version":99,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil {
		t.Fatal("expected version error")
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/registry/`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement**

`internal/registry/registry.go`:
```go
// Package registry stores which local port is owned by which process,
// worktree, and agent session. State is a single JSON file guarded by flock.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	SchemaVersion = 1
	fileName      = "registry.json"
	lockName      = "registry.lock"
	historyName   = "history.jsonl"
	lockTimeout   = 5 * time.Second
)

// Entry is one registered server. Field names are the on-disk schema (spec §5).
type Entry struct {
	ID        string    `json:"id"`
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	Cmd       string    `json:"cmd"`
	Repo      string    `json:"repo,omitempty"`
	Worktree  string    `json:"worktree,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Agent     string    `json:"agent"`
	Session   string    `json:"session,omitempty"`
	Label     string    `json:"label,omitempty"`
	StartedAt time.Time `json:"started_at"`
	HostUser  string    `json:"host_user"`
}

// File is the on-disk document.
type File struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Prober answers liveness questions. The OS implementation lives in
// package liveness; tests inject fakes.
type Prober interface {
	PidAlive(pid int) bool
	PortListening(port int) bool
}

// Store is a handle to the registry directory.
type Store struct {
	dir    string
	prober Prober
	now    func() time.Time
}

// Open ensures dir exists with mode 0700 and returns a Store.
func Open(dir string, p Prober, now func() time.Time) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod state dir: %w", err)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{dir: dir, prober: p, now: now}, nil
}

// Dir returns the state directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// Load reads, prunes, and (if anything was pruned) writes back.
func (s *Store) Load() (*File, error) {
	var out *File
	err := s.Update(func(f *File) error {
		cp := *f
		cp.Entries = append([]Entry(nil), f.Entries...)
		out = &cp
		return nil
	})
	return out, err
}

// Update runs fn on the pruned document under the lock and writes the result atomically.
func (s *Store) Update(fn func(f *File) error) error {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()

	f, err := s.read()
	if err != nil {
		return err
	}
	pruned := s.prune(f)
	if err := fn(f); err != nil {
		return err
	}
	for _, rec := range pruned {
		if err := s.appendHistoryLocked(rec); err != nil {
			return err
		}
	}
	return s.write(f)
}

func (s *Store) read() (*File, error) {
	b, err := os.ReadFile(s.path(fileName))
	if errors.Is(err, os.ErrNotExist) {
		return &File{Version: SchemaVersion, Entries: []Entry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	if f.Version != SchemaVersion {
		return nil, fmt.Errorf("registry schema version %d not supported (want %d)", f.Version, SchemaVersion)
	}
	if f.Entries == nil {
		f.Entries = []Entry{}
	}
	return &f, nil
}

func (s *Store) write(f *File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(fileName + ".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	if err := os.Rename(tmp, s.path(fileName)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit registry: %w", err)
	}
	return nil
}

// prune is replaced with real logic in prune.go (Task 5). Returns pruned records.
func (s *Store) prune(f *File) []HistoryRecord {
	return pruneEntries(f, s.prober, s.now())
}
```

Temporary stub so the package compiles until Task 5, `internal/registry/prune.go`:
```go
package registry

import "time"

func pruneEntries(_ *File, _ Prober, _ time.Time) []HistoryRecord { return nil }
```

`internal/registry/lock.go`:
```go
package registry

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// lock acquires an exclusive flock on path, polling until timeout.
func lock(path string, timeout time.Duration) (func(), error) {
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(fd.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(fd.Fd()), unix.LOCK_UN)
				_ = fd.Close()
			}, nil
		}
		if err != unix.EWOULDBLOCK {
			_ = fd.Close()
			return nil, fmt.Errorf("flock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = fd.Close()
			return nil, fmt.Errorf("registry locked by another process for more than %s", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```
Add `//go:build unix` at the top. Create `lock_windows.go` with `//go:build windows` that opens the file and returns a no-op unlock (best effort per spec).

`internal/registry/history.go`:
```go
package registry

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

const historyCap = 1000

// HistoryRecord is a pruned or released entry with the reason.
type HistoryRecord struct {
	Entry
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// AppendHistory appends under the lock. Used by release/kill paths.
func (s *Store) AppendHistory(rec HistoryRecord) error {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()
	return s.appendHistoryLocked(rec)
}

func (s *Store) appendHistoryLocked(rec HistoryRecord) error {
	lines, err := s.readHistoryLines()
	if err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	lines = append(lines, string(b))
	if len(lines) > historyCap {
		lines = lines[len(lines)-historyCap:]
	}
	tmp := s.path(historyName + ".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(historyName))
}

func (s *Store) readHistoryLines() ([]string, error) {
	fh, err := os.Open(s.path(historyName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var lines []string
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			lines = append(lines, t)
		}
	}
	return lines, sc.Err()
}

// History returns the most recent limit records, oldest first. limit <= 0 means all.
func (s *Store) History(limit int) ([]HistoryRecord, error) {
	lines, err := s.readHistoryLines()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]HistoryRecord, 0, len(lines))
	for _, l := range lines {
		var r HistoryRecord
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			continue // skip corrupt line rather than fail the whole read
		}
		out = append(out, r)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/registry/`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/registry
git commit -m "feat(registry): JSON store with flock, atomic writes, capped history"
```

---

### Task 4: Liveness probes

**Files:**
- Create: `internal/liveness/liveness.go`, `internal/liveness/pid_unix.go`, `internal/liveness/pid_windows.go`
- Test: `internal/liveness/liveness_test.go`

**Interfaces:**
- Produces:
  ```go
  type OS struct{ DialTimeout time.Duration }        // implements registry.Prober
  func (OS) PidAlive(pid int) bool
  func (o OS) PortListening(port int) bool
  func PidUID(pid int) (int, error)                  // unix: via ps; windows: error
  func PidOnPort(port int) (pid int, cmd string, ok bool) // unix: lsof; windows: ok=false
  ```

- [ ] **Step 1: Write failing tests**

`internal/liveness/liveness_test.go`:
```go
package liveness_test

import (
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/liveness"
)

func TestPidAliveSelfTrueDeadFalse(t *testing.T) {
	p := liveness.OS{}
	if !p.PidAlive(os.Getpid()) {
		t.Fatal("own pid should be alive")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no `true` binary")
	}
	if p.PidAlive(cmd.Process.Pid) {
		t.Fatal("exited pid should be dead")
	}
}

func TestPortListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	p := liveness.OS{DialTimeout: 200 * time.Millisecond}
	if !p.PortListening(port) {
		t.Fatal("expected listening")
	}
	ln.Close()
	if p.PortListening(port) {
		t.Fatal("expected closed")
	}
}

func TestPidUIDSelf(t *testing.T) {
	uid, err := liveness.PidUID(os.Getpid())
	if err != nil {
		t.Skip("PidUID unsupported here:", err)
	}
	if uid != os.Getuid() {
		t.Fatalf("uid %d != %d", uid, os.Getuid())
	}
}

func TestPidOnPortFindsListener(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not available")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	pid, _, ok := liveness.PidOnPort(port)
	if !ok || pid != os.Getpid() {
		t.Fatalf("ok=%v pid=%d want %d", ok, pid, os.Getpid())
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/liveness/`
Expected: FAIL, package missing.

- [ ] **Step 3: Implement**

`internal/liveness/liveness.go`:
```go
// Package liveness answers "is this pid alive" and "is this port listening".
package liveness

import (
	"net"
	"strconv"
	"time"
)

// OS probes the real operating system.
type OS struct {
	DialTimeout time.Duration
}

// PortListening dials 127.0.0.1:port. Works on every platform without lsof.
func (o OS) PortListening(port int) bool {
	d := o.DialTimeout
	if d == 0 {
		d = 200 * time.Millisecond
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), d)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
```

`internal/liveness/pid_unix.go`:
```go
//go:build unix

package liveness

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// PidAlive sends signal 0. EPERM means the process exists but belongs to someone else.
func (OS) PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// PidUID returns the real uid of pid via ps.
func PidUID(pid int) (int, error) {
	out, err := exec.Command("ps", "-o", "uid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, errors.New("no such process")
	}
	return strconv.Atoi(s)
}

// PidOnPort finds the listening pid on a TCP port via lsof. ok=false if unknown.
func PidOnPort(port int) (int, string, bool) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return 0, "", false
	}
	var pid int
	var cmd string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(string(line[1:]))
		case 'c':
			cmd = string(line[1:])
		}
		if pid != 0 && cmd != "" {
			return pid, cmd, true
		}
	}
	if pid != 0 {
		return pid, cmd, true
	}
	return 0, "", false
}
```

`internal/liveness/pid_windows.go`:
```go
//go:build windows

package liveness

import (
	"errors"
	"os"
)

func (OS) PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

func PidUID(int) (int, error) { return 0, errors.New("PidUID not supported on windows") }

func PidOnPort(int) (int, string, bool) { return 0, "", false }
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/liveness/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/liveness
git commit -m "feat(liveness): pid and port probes"
```

---

### Task 5: Registry prune with grace period

**Files:**
- Modify: `internal/registry/prune.go` (replace stub)
- Test: `internal/registry/prune_test.go`

**Interfaces:**
- Consumes: `registry.Prober`, `registry.File`, `registry.HistoryRecord`.
- Produces: `pruneEntries(f *File, p Prober, now time.Time) []HistoryRecord` (unexported, called from `Update`). Rules: pid dead → reason `pid_dead`; port not listening and `now - StartedAt >= 60s` → reason `port_closed`; within 60 s grace only the pid check applies.

- [ ] **Step 1: Write failing tests**

`internal/registry/prune_test.go`:
```go
package registry_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

type fakeProber struct {
	alive     map[int]bool
	listening map[int]bool
}

func (f fakeProber) PidAlive(pid int) bool       { return f.alive[pid] }
func (f fakeProber) PortListening(port int) bool { return f.listening[port] }

func TestPruneRemovesDeadPidAndClosedPortAfterGrace(t *testing.T) {
	now := fixedNow()
	p := fakeProber{
		alive:     map[int]bool{10: true, 11: false, 12: true, 13: true},
		listening: map[int]bool{3000: true, 3001: true, 3002: false, 3003: false},
	}
	s, err := registry.Open(filepath.Join(t.TempDir(), "hm"), p, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	seed := []registry.Entry{
		{ID: "ok", Port: 3000, PID: 10, StartedAt: now.Add(-time.Hour)},
		{ID: "deadpid", Port: 3001, PID: 11, StartedAt: now.Add(-time.Hour)},
		{ID: "closed-old", Port: 3002, PID: 12, StartedAt: now.Add(-2 * time.Minute)},
		{ID: "closed-fresh", Port: 3003, PID: 13, StartedAt: now.Add(-10 * time.Second)},
	}
	// Seed without pruning by using a store whose prober says everything is alive.
	seedStore, _ := registry.Open(s.Dir(), alwaysAlive{}, func() time.Time { return now })
	if err := seedStore.Update(func(f *registry.File) error { f.Entries = seed; return nil }); err != nil {
		t.Fatal(err)
	}

	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range f.Entries {
		ids[e.ID] = true
	}
	if !ids["ok"] || ids["deadpid"] || ids["closed-old"] || !ids["closed-fresh"] {
		t.Fatalf("surviving ids: %v", ids)
	}

	hist, _ := s.History(0)
	reasons := map[string]string{}
	for _, h := range hist {
		reasons[h.ID] = h.Reason
	}
	if reasons["deadpid"] != "pid_dead" || reasons["closed-old"] != "port_closed" {
		t.Fatalf("history reasons: %v", reasons)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

Run: `go test ./internal/registry/ -run TestPrune`
Expected: FAIL: deadpid and closed-old survive.

- [ ] **Step 3: Implement**

Replace `internal/registry/prune.go`:
```go
package registry

import "time"

// ListenGrace is how long after StartedAt an entry may have a closed port
// without being pruned. Matches the run command's listen timeout.
const ListenGrace = 60 * time.Second

func pruneEntries(f *File, p Prober, now time.Time) []HistoryRecord {
	if p == nil {
		return nil
	}
	kept := f.Entries[:0]
	var pruned []HistoryRecord
	for _, e := range f.Entries {
		switch {
		case !p.PidAlive(e.PID):
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "pid_dead", At: now})
		case now.Sub(e.StartedAt) >= ListenGrace && !p.PortListening(e.Port):
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "port_closed", At: now})
		default:
			kept = append(kept, e)
		}
	}
	f.Entries = kept
	return pruned
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/registry/`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/registry
git commit -m "feat(registry): prune dead pids and closed ports with listen grace"
```

---

### Task 6: Git context discovery

**Files:**
- Create: `internal/gitctx/gitctx.go`
- Test: `internal/gitctx/gitctx_test.go`

**Interfaces:**
- Produces:
  ```go
  type Context struct { Repo, Worktree, Branch string }
  func Discover(dir string) Context   // all empty when dir is not in a git repo
  ```
  `Repo` = parent dir of `git rev-parse --git-common-dir` (absolute). `Worktree` = `git rev-parse --show-toplevel`. `Branch` = `git rev-parse --abbrev-ref HEAD`, empty for detached.

- [ ] **Step 1: Write failing tests**

`internal/gitctx/gitctx_test.go`:
```go
package gitctx_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/moeritze/harbormaster/internal/gitctx"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiscoverOutsideGit(t *testing.T) {
	c := gitctx.Discover(t.TempDir())
	if c != (gitctx.Context{}) {
		t.Fatalf("expected empty, got %+v", c)
	}
}

func TestDiscoverRepoAndWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	root := t.TempDir()
	main := filepath.Join(root, "main")
	git(t, root, "init", "-q", "-b", "main", main)
	git(t, main, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "wt-feature")
	git(t, main, "worktree", "add", "-q", "-b", "feature", wt)

	c := gitctx.Discover(wt)
	if filepath.Base(c.Repo) != "main" {
		t.Fatalf("repo %q", c.Repo)
	}
	if filepath.Base(c.Worktree) != "wt-feature" {
		t.Fatalf("worktree %q", c.Worktree)
	}
	if c.Branch != "feature" {
		t.Fatalf("branch %q", c.Branch)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/gitctx/`
Expected: FAIL, package missing.

- [ ] **Step 3: Implement**

`internal/gitctx/gitctx.go`:
```go
// Package gitctx discovers repo, worktree, and branch for a directory.
package gitctx

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Context describes where a command runs. Empty fields mean "not in git".
type Context struct {
	Repo     string
	Worktree string
	Branch   string
}

// Discover never fails; it returns whatever git can tell.
func Discover(dir string) Context {
	top := run(dir, "rev-parse", "--show-toplevel")
	if top == "" {
		return Context{}
	}
	common := run(dir, "rev-parse", "--git-common-dir")
	if common != "" && !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	repo := filepath.Dir(filepath.Clean(common))
	branch := run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		branch = ""
	}
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	if w, err := filepath.EvalSymlinks(top); err == nil {
		top = w
	}
	return Context{Repo: repo, Worktree: top, Branch: branch}
}

func run(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/gitctx/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gitctx
git commit -m "feat(gitctx): discover repo, worktree, branch"
```

---

### Task 7: Deterministic port allocation

**Files:**
- Create: `internal/ports/ports.go`
- Test: `internal/ports/ports_test.go`

**Interfaces:**
- Produces:
  ```go
  type Config struct { Base, Range int }
  func ConfigFromEnv(getenv func(string) string) (Config, error)   // HARBORMASTER_BASE / HARBORMASTER_RANGE, defaults 3000/1000
  func Deterministic(key string, c Config) int                       // c.Base + fnv1a64(key) % c.Range
  func Resolve(key string, c Config, taken func(int) bool) (int, error) // probe upward from Deterministic within [Base, Base+Range)
  ```

- [ ] **Step 1: Write failing tests**

`internal/ports/ports_test.go`:
```go
package ports_test

import (
	"testing"

	"github.com/moeritze/harbormaster/internal/ports"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDefaultsAndOverrides(t *testing.T) {
	c, err := ports.ConfigFromEnv(env(nil))
	if err != nil || c.Base != 3000 || c.Range != 1000 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_BASE": "8000", "HARBORMASTER_RANGE": "10"}))
	if err != nil || c.Base != 8000 || c.Range != 10 {
		t.Fatalf("override: %+v %v", c, err)
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_BASE": "abc"})); err == nil {
		t.Fatal("expected error on non-numeric base")
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_RANGE": "0"})); err == nil {
		t.Fatal("expected error on zero range")
	}
}

func TestDeterministicStableAndInRange(t *testing.T) {
	c := ports.Config{Base: 3000, Range: 1000}
	a := ports.Deterministic("/Users/m/repo-wt/auth", c)
	b := ports.Deterministic("/Users/m/repo-wt/auth", c)
	if a != b {
		t.Fatalf("not stable: %d %d", a, b)
	}
	if a < 3000 || a >= 4000 {
		t.Fatalf("out of range: %d", a)
	}
	if ports.Deterministic("/Users/m/repo-wt/other", c) == a {
		t.Log("collision between two keys is allowed but unlikely; check hash if this repeats")
	}
}

func TestResolveProbesUpwardWithinRange(t *testing.T) {
	c := ports.Config{Base: 3000, Range: 5}
	want := ports.Deterministic("k", c)
	taken := map[int]bool{want: true}
	got, err := ports.Resolve("k", c, func(p int) bool { return taken[p] })
	if err != nil {
		t.Fatal(err)
	}
	if got == want || got < 3000 || got >= 3005 {
		t.Fatalf("got %d (deterministic %d)", got, want)
	}
	all := func(int) bool { return true }
	if _, err := ports.Resolve("k", c, all); err == nil {
		t.Fatal("expected exhaustion error")
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/ports/`
Expected: FAIL, package missing.

- [ ] **Step 3: Implement**

`internal/ports/ports.go`:
```go
// Package ports allocates a stable port per worktree.
package ports

import (
	"fmt"
	"hash/fnv"
	"strconv"
)

// Config is the allocation window [Base, Base+Range).
type Config struct {
	Base  int
	Range int
}

// ConfigFromEnv reads HARBORMASTER_BASE and HARBORMASTER_RANGE.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{Base: 3000, Range: 1000}
	if v := getenv("HARBORMASTER_BASE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("HARBORMASTER_BASE must be 1-65535, got %q", v)
		}
		c.Base = n
	}
	if v := getenv("HARBORMASTER_RANGE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("HARBORMASTER_RANGE must be >= 1, got %q", v)
		}
		c.Range = n
	}
	if c.Base+c.Range-1 > 65535 {
		return c, fmt.Errorf("port window %d-%d exceeds 65535", c.Base, c.Base+c.Range-1)
	}
	return c, nil
}

// Deterministic maps key to a stable port inside the window.
func Deterministic(key string, c Config) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return c.Base + int(h.Sum64()%uint64(c.Range))
}

// Resolve returns the deterministic port or the next free one above it,
// wrapping around inside the window. taken reports registry or OS conflicts.
func Resolve(key string, c Config, taken func(int) bool) (int, error) {
	start := Deterministic(key, c)
	for i := 0; i < c.Range; i++ {
		p := c.Base + (start-c.Base+i)%c.Range
		if !taken(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in %d-%d", c.Base, c.Base+c.Range-1)
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/ports/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ports
git commit -m "feat(ports): deterministic per-worktree port with upward probing"
```

---

### Task 8: Identity, ownership, sanitizing, redaction

**Files:**
- Create: `internal/ident/ident.go`
- Test: `internal/ident/ident_test.go`

**Interfaces:**
- Produces:
  ```go
  type Identity struct { Agent, Session, HostUser string }
  func Detect(getenv func(string) string, hostUser string) Identity
  func Owns(me Identity, e registry.Entry, myWorktree string) bool
  func Sanitize(s string) string          // printable runes only, max 256 bytes, no control chars
  func RedactCmd(args []string) string    // joins args, masks values of KEY/SECRET/TOKEN/PASSWORD assignments and flags
  ```
  Detection order: `HARBORMASTER_AGENT`+`HARBORMASTER_SESSION` explicit override → `CLAUDE_SESSION_ID` (agent `claude`) → else agent `human`, session empty. Cursor/Codex variables are added in Plan 3.

- [ ] **Step 1: Write failing tests**

`internal/ident/ident_test.go`:
```go
package ident_test

import (
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want ident.Identity
	}{
		{"human", nil, ident.Identity{Agent: "human", HostUser: "m"}},
		{"claude", map[string]string{"CLAUDE_SESSION_ID": "s1"}, ident.Identity{Agent: "claude", Session: "s1", HostUser: "m"}},
		{"override", map[string]string{"CLAUDE_SESSION_ID": "s1", "HARBORMASTER_AGENT": "cursor", "HARBORMASTER_SESSION": "c9"}, ident.Identity{Agent: "cursor", Session: "c9", HostUser: "m"}},
	}
	for _, c := range cases {
		if got := ident.Detect(env(c.env), "m"); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestOwns(t *testing.T) {
	me := ident.Identity{Agent: "claude", Session: "s1"}
	if !ident.Owns(me, registry.Entry{Session: "s1"}, "/wt/a") {
		t.Fatal("same session should own")
	}
	if ident.Owns(me, registry.Entry{Session: "s2", Worktree: "/wt/a"}, "/wt/a") {
		t.Fatal("different session should not own even in same worktree")
	}
	human := ident.Identity{Agent: "human"}
	if !ident.Owns(human, registry.Entry{Session: "s2", Worktree: "/wt/a"}, "/wt/a") {
		t.Fatal("no session: same worktree should own")
	}
	if ident.Owns(human, registry.Entry{Session: "s2", Worktree: "/wt/b"}, "/wt/a") {
		t.Fatal("no session: other worktree should not own")
	}
}

func TestSanitize(t *testing.T) {
	if got := ident.Sanitize("ok label"); got != "ok label" {
		t.Fatal(got)
	}
	if got := ident.Sanitize("bad\x1b[31mred\x00\n"); got != "bad[31mred" {
		t.Fatalf("%q", got)
	}
	long := ident.Sanitize(strings.Repeat("ä", 300))
	if len(long) > 256 {
		t.Fatalf("len %d", len(long))
	}
	if !strings.HasSuffix(long, "ä") {
		t.Fatal("must not cut a rune in half")
	}
}

func TestRedactCmd(t *testing.T) {
	got := ident.RedactCmd([]string{"npm", "run", "dev", "--", "--api-key=abc123", "STRIPE_SECRET=sk_live_1", "--token", "tkn", "--port", "3000"})
	if strings.Contains(got, "abc123") || strings.Contains(got, "sk_live_1") || strings.Contains(got, "tkn") {
		t.Fatalf("leaked: %s", got)
	}
	if !strings.Contains(got, "--port 3000") {
		t.Fatalf("over-redacted: %s", got)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/ident/`
Expected: FAIL, package missing.

- [ ] **Step 3: Implement**

`internal/ident/ident.go`:
```go
// Package ident identifies the calling agent session and applies data-safety rules.
package ident

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moeritze/harbormaster/internal/registry"
)

// Identity is who is calling harbormaster.
type Identity struct {
	Agent    string
	Session  string
	HostUser string
}

// Detect reads well-known environment variables.
func Detect(getenv func(string) string, hostUser string) Identity {
	id := Identity{Agent: "human", HostUser: hostUser}
	if a := getenv("HARBORMASTER_AGENT"); a != "" {
		id.Agent = Sanitize(a)
		id.Session = Sanitize(getenv("HARBORMASTER_SESSION"))
		return id
	}
	if s := getenv("CLAUDE_SESSION_ID"); s != "" {
		id.Agent = "claude"
		id.Session = Sanitize(s)
	}
	return id
}

// Owns implements spec §5 ownership: same session, or same worktree when the caller has none.
func Owns(me Identity, e registry.Entry, myWorktree string) bool {
	if me.Session != "" {
		return e.Session == me.Session
	}
	return myWorktree != "" && e.Worktree == myWorktree
}

const maxLen = 256

// Sanitize drops control and non-printable runes and caps at 256 bytes on a rune boundary.
func Sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > maxLen {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	sensitiveKey = regexp.MustCompile(`(?i)(key|secret|token|password|passwd|pwd)`)
	assignment   = regexp.MustCompile(`^(--?[A-Za-z0-9_-]+|[A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
)

// RedactCmd joins args for storage, masking sensitive values.
func RedactCmd(args []string) string {
	out := make([]string, 0, len(args))
	maskNext := false
	for _, a := range args {
		if maskNext {
			out = append(out, "***")
			maskNext = false
			continue
		}
		if m := assignment.FindStringSubmatch(a); m != nil {
			if sensitiveKey.MatchString(m[1]) {
				out = append(out, m[1]+"=***")
				continue
			}
			out = append(out, a)
			continue
		}
		if strings.HasPrefix(a, "-") && sensitiveKey.MatchString(a) {
			maskNext = true
		}
		out = append(out, a)
	}
	return Sanitize(strings.Join(out, " "))
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/ident/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ident
git commit -m "feat(ident): session detection, ownership, sanitize, secret redaction"
```

---

### Task 9: App wiring and read/write commands (`ls`, `port`, `check`, `claim`, `release`, `gc`, `history`)

**Files:**
- Modify: `internal/app/app.go`, `cmd/harbormaster/main.go`, `internal/cli/root.go`
- Create: `internal/cli/output.go`, `internal/cli/ls.go`, `internal/cli/port.go`, `internal/cli/check.go`, `internal/cli/claim.go`, `internal/cli/release.go`, `internal/cli/gc.go`, `internal/cli/history.go`
- Test: `internal/cli/commands_test.go`

**Interfaces:**
- Consumes: `registry.Store`, `liveness.OS`, `liveness.PidOnPort`, `gitctx.Discover`, `ports.*`, `ident.*`.
- Produces:
  ```go
  // app
  type App struct {
      Stdout, Stderr io.Writer; Now func() time.Time
      Store *registry.Store; Prober registry.Prober; Ident ident.Identity
      Git gitctx.Context; Ports ports.Config; Cwd string
      PidOnPort func(port int) (int, string, bool)
  }
  func New(getenv func(string) string, stdout, stderr io.Writer) (*App, error)  // builds everything from env + cwd
  func (a *App) WorktreeKey() string  // Git.Worktree or Cwd
  func (a *App) MyPort() (int, error) // ports.Resolve with taken = registry foreign entries or OS listener
  // cli helpers
  func findByPort(f *registry.File, port int) (registry.Entry, bool)
  func ownerLine(e registry.Entry, now time.Time) string  // `claude session 0132… in ../wt/auth ("label", 12m)`
  ```

- [ ] **Step 1: Write failing tests**

`internal/cli/commands_test.go`:
```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/cli"
	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

type fakeProber struct{ alive, listening map[int]bool }

func (f *fakeProber) PidAlive(p int) bool       { return f.alive[p] }
func (f *fakeProber) PortListening(p int) bool { return f.listening[p] }

type harness struct {
	app    *app.App
	out    *bytes.Buffer
	prober *fakeProber
	now    time.Time
}

func newHarness(t *testing.T, session, worktree string) *harness {
	t.Helper()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	pr := &fakeProber{alive: map[int]bool{}, listening: map[int]bool{}}
	dir := filepath.Join(t.TempDir(), "hm")
	st, err := registry.Open(dir, pr, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	a := &app.App{
		Stdout: out, Stderr: out,
		Now:    func() time.Time { return now },
		Store:  st, Prober: pr,
		Ident:  ident.Identity{Agent: "claude", Session: session, HostUser: "m"},
		Git:    gitctx.Context{Repo: "/r", Worktree: worktree, Branch: "feat"},
		Ports:  ports.Config{Base: 3000, Range: 1000},
		Cwd:    worktree,
		PidOnPort: func(int) (int, string, bool) { return 0, "", false },
	}
	return &harness{app: a, out: out, prober: pr, now: now}
}

func (h *harness) run(args ...string) error {
	h.out.Reset()
	root := cli.NewRoot(h.app)
	root.SetArgs(args)
	return root.Execute()
}

func (h *harness) seed(t *testing.T, e registry.Entry) {
	t.Helper()
	h.prober.alive[e.PID] = true
	h.prober.listening[e.Port] = true
	if e.StartedAt.IsZero() {
		e.StartedAt = h.now.Add(-12 * time.Minute)
	}
	if err := h.app.Store.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil }); err != nil {
		t.Fatal(err)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*cli.ExitError); ok {
		return ee.Code
	}
	return -1
}

func TestLsEmptyAndTable(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if err := h.run("ls"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "no registered servers") {
		t.Fatalf("%q", h.out.String())
	}
	h.seed(t, registry.Entry{ID: "x", Port: 3000, PID: 10, Agent: "claude", Session: "s2", Worktree: "/wt/b", Branch: "other", Label: "login flow"})
	_ = h.run("ls")
	s := h.out.String()
	for _, want := range []string{"3000", "claude", "/wt/b", "login flow", "12m"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	_ = h.run("ls", "--json")
	var rows []registry.Entry
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("json: %v %s", err, h.out.String())
	}
}

func TestPortIsDeterministic(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	_ = h.run("port")
	first := strings.TrimSpace(h.out.String())
	_ = h.run("port")
	if first != strings.TrimSpace(h.out.String()) || first == "" {
		t.Fatal("port not stable")
	}
}

func TestCheckExitCodes(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if c := exitCode(h.run("check", "3000")); c != 0 {
		t.Fatalf("free: %d", c)
	}
	h.seed(t, registry.Entry{ID: "mine", Port: 3000, PID: 10, Session: "s1"})
	if c := exitCode(h.run("check", "3000")); c != 0 {
		t.Fatalf("own: %d", c)
	}
	h.seed(t, registry.Entry{ID: "theirs", Port: 3001, PID: 11, Session: "s2", Worktree: "/wt/b", Label: "x"})
	err := h.run("check", "3001")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "s2") {
		t.Fatalf("foreign: %d %v", c, err)
	}
	h.app.PidOnPort = func(p int) (int, string, bool) { return 999, "node", p == 3002 }
	h.prober.listening[3002] = true
	if c := exitCode(h.run("check", "3002")); c != 2 {
		t.Fatalf("unregistered: %d", c)
	}
	if c := exitCode(h.run("check", "abc")); c != 3 {
		t.Fatalf("usage: %d", c)
	}
}

func TestClaimAndRelease(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.alive[42] = true
	h.prober.listening[3005] = true
	if err := h.run("claim", "3005", "--pid", "42", "--label", "manual"); err != nil {
		t.Fatal(err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].PID != 42 || f.Entries[0].Session != "s1" || f.Entries[0].Label != "manual" {
		t.Fatalf("%+v", f.Entries)
	}
	if c := exitCode(h.run("claim", "3005", "--pid", "42")); c != 1 {
		t.Fatalf("double claim should be denied, got %d", c)
	}
	if err := h.run("release", "3005"); err != nil {
		t.Fatal(err)
	}
	f, _ = h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatal("not released")
	}
	hist, _ := h.app.Store.History(0)
	if len(hist) != 1 || hist[0].Reason != "released" {
		t.Fatalf("history %+v", hist)
	}
}

func TestReleaseForeignDeniedUnlessForce(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3001, PID: 11, Session: "s2"})
	if c := exitCode(h.run("release", "3001")); c != 1 {
		t.Fatalf("got %d", c)
	}
	if err := h.run("release", "3001", "--force"); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseBySessionAndAllMine(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "a", Port: 3001, PID: 11, Session: "s1"})
	h.seed(t, registry.Entry{ID: "b", Port: 3002, PID: 12, Session: "s1"})
	h.seed(t, registry.Entry{ID: "c", Port: 3003, PID: 13, Session: "s2"})
	if err := h.run("release", "--all-mine"); err != nil {
		t.Fatal(err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].ID != "c" {
		t.Fatalf("%+v", f.Entries)
	}
	if err := h.run("release", "--session", "s2"); err != nil {
		t.Fatal(err)
	}
	f, _ = h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("%+v", f.Entries)
	}
}

func TestGcReportsPruned(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "dead", Port: 3001, PID: 11, Session: "s1"})
	h.prober.alive[11] = false
	if err := h.run("gc"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "pruned 1") {
		t.Fatalf("%q", h.out.String())
	}
}

func TestHistoryLists(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "a", Port: 3001, PID: 11, Session: "s1"})
	_ = h.run("release", "3001")
	_ = h.run("history")
	if !strings.Contains(h.out.String(), "released") || !strings.Contains(h.out.String(), "3001") {
		t.Fatalf("%q", h.out.String())
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/cli/`
Expected: FAIL, App fields and commands missing.

- [ ] **Step 3: Extend `app.App` and add `New`**

Replace `internal/app/app.go`:
```go
// Package app wires the dependencies every command needs.
package app

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

// App carries injected dependencies.
type App struct {
	Stdout    io.Writer
	Stderr    io.Writer
	Now       func() time.Time
	Store     *registry.Store
	Prober    registry.Prober
	Ident     ident.Identity
	Git       gitctx.Context
	Ports     ports.Config
	Cwd       string
	PidOnPort func(port int) (int, string, bool)
}

// New builds an App from the environment and current directory.
func New(getenv func(string) string, stdout, stderr io.Writer) (*App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	pc, err := ports.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	hostUser := ""
	if u, err := user.Current(); err == nil {
		hostUser = u.Username
	}
	prober := liveness.OS{}
	now := func() time.Time { return time.Now().UTC() }
	store, err := registry.Open(StateDir(getenv), prober, now)
	if err != nil {
		return nil, err
	}
	return &App{
		Stdout: stdout, Stderr: stderr, Now: now,
		Store: store, Prober: prober,
		Ident:     ident.Detect(getenv, hostUser),
		Git:       gitctx.Discover(cwd),
		Ports:     pc,
		Cwd:       cwd,
		PidOnPort: liveness.PidOnPort,
	}, nil
}

// StateDir resolves HARBORMASTER_HOME, then XDG_STATE_HOME, then ~/.local/state.
func StateDir(getenv func(string) string) string {
	if d := getenv("HARBORMASTER_HOME"); d != "" {
		return d
	}
	if x := getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "harbormaster")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "state", "harbormaster")
}

// Clock returns the current time via the injected clock.
func (a *App) Clock() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

// WorktreeKey is the hash input for deterministic ports.
func (a *App) WorktreeKey() string {
	if a.Git.Worktree != "" {
		return a.Git.Worktree
	}
	return a.Cwd
}

// MyPort resolves this worktree's port, skipping ports held by others.
func (a *App) MyPort() (int, error) {
	f, err := a.Store.Load()
	if err != nil {
		return 0, fmt.Errorf("registry: %w", err)
	}
	taken := func(p int) bool {
		for _, e := range f.Entries {
			if e.Port == p {
				return !ident.Owns(a.Ident, e, a.Git.Worktree)
			}
		}
		return a.Prober.PortListening(p)
	}
	return ports.Resolve(a.WorktreeKey(), a.Ports, taken)
}
```

Update `cmd/harbormaster/main.go` to use `app.New(os.Getenv, os.Stdout, os.Stderr)` and exit 4 with the error message if it fails.

- [ ] **Step 4: Implement output helpers**

`internal/cli/output.go`:
```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func age(now, t time.Time) string {
	d := now.Sub(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func shortSession(s string) string {
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}

func shortPath(p string) string {
	if p == "" {
		return "-"
	}
	return filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)
}

// ownerLine renders the actionable owner description used in errors.
func ownerLine(e registry.Entry, now time.Time) string {
	who := e.Agent
	if e.Session != "" {
		who += " session " + shortSession(e.Session)
	}
	where := shortPath(e.Worktree)
	label := ""
	if e.Label != "" {
		label = fmt.Sprintf("%q, ", e.Label)
	}
	return fmt.Sprintf("%s in %s (%s%s)", who, where, label, age(now, e.StartedAt))
}

func writeTable(w io.Writer, entries []registry.Entry, now time.Time) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "no registered servers")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PORT\tPID\tAGENT\tSESSION\tWORKTREE\tBRANCH\tLABEL\tAGE")
	for _, e := range entries {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Port, e.PID, e.Agent, shortSession(e.Session), e.Worktree, orDash(e.Branch), orDash(e.Label), age(now, e.StartedAt))
	}
	return tw.Flush()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func findByPort(f *registry.File, port int) (registry.Entry, bool) {
	for _, e := range f.Entries {
		if e.Port == port {
			return e, true
		}
	}
	return registry.Entry{}, false
}

func parsePort(s string) (int, error) {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil || p < 1 || p > 65535 {
		return 0, exitf(ExitUsage, "invalid port %q", s)
	}
	return p, nil
}
```

- [ ] **Step 5: Implement commands**

`internal/cli/ls.go`:
```go
package cli

import (
	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newLs(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List registered servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := a.Store.Load()
			if err != nil {
				return exitf(ExitRegistry, "registry: %v", err)
			}
			if asJSON {
				return writeJSON(a.Stdout, f.Entries)
			}
			return writeTable(a.Stdout, f.Entries, a.Clock())
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}
```

`internal/cli/port.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newPort(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "port",
		Short: "Print this worktree's deterministic port",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.MyPort()
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			_, err = fmt.Fprintln(a.Stdout, p)
			return err
		},
	}
}
```

`internal/cli/check.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
)

type checkResult struct {
	Port   int    `json:"port"`
	Status string `json:"status"` // free | own | foreign | unregistered
	Owner  string `json:"owner,omitempty"`
	PID    int    `json:"pid,omitempty"`
}

func newCheck(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check <port>",
		Short: "Report whether a port is free, yours, foreign, or held by an unregistered process",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := parsePort(args[0])
			if err != nil {
				return err
			}
			res, code := checkPort(a, port)
			if asJSON {
				if err := writeJSON(a.Stdout, res); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(a.Stdout, "port %d: %s", port, res.Status)
				if res.Owner != "" {
					fmt.Fprintf(a.Stdout, " (%s)", res.Owner)
				}
				fmt.Fprintln(a.Stdout)
			}
			if code != ExitOK {
				return exitf(code, "port %d %s: %s", port, res.Status, res.Owner)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func checkPort(a *app.App, port int) (checkResult, int) {
	f, err := a.Store.Load()
	if err != nil {
		return checkResult{Port: port, Status: "error", Owner: err.Error()}, ExitRegistry
	}
	if e, ok := findByPort(f, port); ok {
		if ident.Owns(a.Ident, e, a.Git.Worktree) {
			return checkResult{Port: port, Status: "own", PID: e.PID}, ExitOK
		}
		return checkResult{Port: port, Status: "foreign", Owner: ownerLine(e, a.Clock()), PID: e.PID}, ExitDenied
	}
	if a.Prober.PortListening(port) {
		pid, cmdName, ok := a.PidOnPort(port)
		owner := "unknown process"
		if ok {
			owner = fmt.Sprintf("pid %d (%s), not registered", pid, cmdName)
		}
		return checkResult{Port: port, Status: "unregistered", Owner: owner, PID: pid}, ExitUnregistered
	}
	return checkResult{Port: port, Status: "free"}, ExitOK
}
```

`internal/cli/claim.go`:
```go
package cli

import (
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func newClaim(a *app.App) *cobra.Command {
	var pid int
	var label string
	cmd := &cobra.Command{
		Use:   "claim <port>",
		Short: "Register a server you started without `run`",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := parsePort(args[0])
			if err != nil {
				return err
			}
			if pid == 0 {
				p, cmdName, ok := a.PidOnPort(port)
				if !ok {
					return exitf(ExitUsage, "cannot find a process on port %d, pass --pid", port)
				}
				pid = p
				_ = cmdName
			}
			if !a.Prober.PidAlive(pid) {
				return exitf(ExitUsage, "pid %d is not running", pid)
			}
			return a.Store.Update(func(f *registry.File) error {
				if e, ok := findByPort(f, port); ok {
					if ident.Owns(a.Ident, e, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d already registered by you (pid %d)", port, e.PID)
					}
					return exitf(ExitDenied, "port %d owned by %s", port, ownerLine(e, a.Clock()))
				}
				f.Entries = append(f.Entries, newEntry(a, port, pid, fmt.Sprintf("claimed pid %d", pid), label))
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&pid, "pid", 0, "pid listening on the port (default: detect)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	return cmd
}

// newEntry builds a registry entry for the current identity and worktree.
func newEntry(a *app.App, port, pid int, cmdLine, label string) registry.Entry {
	return registry.Entry{
		ID:        ulid.Make().String(),
		Port:      port,
		PID:       pid,
		Cmd:       ident.Sanitize(cmdLine),
		Repo:      a.Git.Repo,
		Worktree:  a.Git.Worktree,
		Branch:    a.Git.Branch,
		Agent:     a.Ident.Agent,
		Session:   a.Ident.Session,
		Label:     ident.Sanitize(label),
		StartedAt: a.Clock(),
		HostUser:  a.Ident.HostUser,
	}
}
```

`internal/cli/release.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func newRelease(a *app.App) *cobra.Command {
	var session string
	var allMine, force, kill bool
	cmd := &cobra.Command{
		Use:   "release [port]",
		Short: "Remove registry entries (yours by default)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var port int
			if len(args) == 1 {
				p, err := parsePort(args[0])
				if err != nil {
					return err
				}
				port = p
			}
			if port == 0 && session == "" && !allMine {
				return exitf(ExitUsage, "specify a port, --session ID, or --all-mine")
			}
			var removed []registry.Entry
			err := a.Store.Update(func(f *registry.File) error {
				kept := f.Entries[:0]
				for _, e := range f.Entries {
					match := (port != 0 && e.Port == port) ||
						(session != "" && e.Session == session) ||
						(allMine && ident.Owns(a.Ident, e, a.Git.Worktree))
					if !match {
						kept = append(kept, e)
						continue
					}
					if port != 0 && !force && !ident.Owns(a.Ident, e, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d owned by %s. Use --force to release anyway.", port, ownerLine(e, a.Clock()))
					}
					removed = append(removed, e)
				}
				f.Entries = kept
				return nil
			})
			if err != nil {
				return err
			}
			for _, e := range removed {
				if kill {
					if err := terminateEntry(a, e); err != nil {
						fmt.Fprintf(a.Stderr, "warn: kill pid %d: %v\n", e.PID, err)
					}
				}
				_ = a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "released", At: a.Clock()})
			}
			fmt.Fprintf(a.Stdout, "released %d\n", len(removed))
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "release every entry of this session id")
	cmd.Flags().BoolVar(&allMine, "all-mine", false, "release every entry you own")
	cmd.Flags().BoolVar(&force, "force", false, "release entries owned by others")
	cmd.Flags().BoolVar(&kill, "kill", false, "also terminate the processes")
	return cmd
}
```

`terminateEntry` is implemented in Task 11. For now add to `release.go` a stub so this task compiles:
```go
// terminateEntry is implemented in Task 11 (runner.Terminate + Guard).
func terminateEntry(a *app.App, e registry.Entry) error { return nil }
```

`internal/cli/gc.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newGc(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Prune dead entries now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			before, err := a.Store.History(0)
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			if _, err := a.Store.Load(); err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			after, _ := a.Store.History(0)
			fmt.Fprintf(a.Stdout, "pruned %d\n", len(after)-len(before))
			return nil
		},
	}
}
```

`internal/cli/history.go`:
```go
package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newHistory(a *app.App) *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show pruned and released entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			recs, err := a.Store.History(limit)
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			if asJSON {
				return writeJSON(a.Stdout, recs)
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "AT\tPORT\tREASON\tAGENT\tWORKTREE\tLABEL")
			for _, r := range recs {
				fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\n", r.At.Format("2006-01-02 15:04"), r.Port, r.Reason, r.Agent, shortPath(r.Worktree), orDash(r.Label))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().IntVar(&limit, "limit", 50, "most recent N records, 0 for all")
	return cmd
}
```

Register all in `root.go`: `root.AddCommand(newLs(a), newPort(a), newCheck(a), newClaim(a), newRelease(a), newGc(a), newHistory(a))`.

- [ ] **Step 6: Run tests, expect pass**

Run: `go test -race ./internal/cli/ && make lint`
Expected: PASS, lint clean.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(cli): ls, port, check, claim, release, gc, history"
```

---

### Task 10: Runner (`run` core: spawn, env, process group, signals, register/unregister)

**Files:**
- Create: `internal/runner/runner.go`, `internal/runner/terminate.go`, `internal/runner/terminate_unix.go`, `internal/runner/terminate_windows.go`
- Test: `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `app.App`, `registry.*`, `ident.RedactCmd`.
- Produces:
  ```go
  type Options struct { Port int; Label string; EnvNames []string; Args []string; ListenTimeout time.Duration; KillTimeout time.Duration }
  func Run(ctx context.Context, a *app.App, opts Options, register func(port, pid int, cmdLine, label string) (registry.Entry, error)) (exitCode int, err error)
  func Terminate(pid int, group bool, timeout time.Duration) error   // SIGTERM (group if true), wait, SIGKILL
  func Guard(pid int, uid func(int) (int, error)) error              // refuses pid <= 1, own pid, other uid
  ```
  `register` is injected so the runner does not depend on cli's `newEntry`; cli passes a closure.

- [ ] **Step 1: Write failing tests (uses a self re-exec listener helper)**

`internal/runner/runner_test.go`:
```go
package runner_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/runner"
)

// TestMain turns the test binary into a tiny TCP listener when HM_TEST_LISTENER=1.
func TestMain(m *testing.M) {
	if os.Getenv("HM_TEST_LISTENER") == "1" {
		port := os.Getenv("PORT")
		if alt := os.Getenv("HM_TEST_ENV_NAME"); alt != "" {
			port = os.Getenv(alt)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			fmt.Fprintln(os.Stderr, "listen:", err)
			os.Exit(7)
		}
		fmt.Println("listening on", port)
		select {} // run until killed
	}
	os.Exit(m.Run())
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newApp(t *testing.T) (*app.App, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	st, err := registry.Open(filepath.Join(t.TempDir(), "hm"), liveness.OS{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &app.App{Stdout: out, Stderr: out, Store: st, Prober: liveness.OS{}}, out
}

func listenerArgs(envName string) []string {
	return []string{os.Args[0], "-test.run=^$"}
}

func register(a *app.App) func(port, pid int, cmdLine, label string) (registry.Entry, error) {
	return func(port, pid int, cmdLine, label string) (registry.Entry, error) {
		e := registry.Entry{ID: strconv.Itoa(pid), Port: port, PID: pid, Cmd: cmdLine, Label: label, Agent: "test", StartedAt: time.Now().UTC()}
		err := a.Store.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil })
		return e, err
	}
}

func TestRunRegistersInjectsPortAndUnregistersOnSignal(t *testing.T) {
	a, out := newApp(t)
	port := freePort(t)
	t.Setenv("HM_TEST_LISTENER", "1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var code int
	var runErr error
	go func() {
		code, runErr = runner.Run(ctx, a, runner.Options{Port: port, Label: "t", Args: listenerArgs(""), ListenTimeout: 5 * time.Second, KillTimeout: 2 * time.Second}, register(a))
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !(liveness.OS{}).PortListening(port) {
		time.Sleep(50 * time.Millisecond)
	}
	if !(liveness.OS{}).PortListening(port) {
		t.Fatalf("child never listened; output:\n%s", out.String())
	}
	f, _ := a.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].Port != port {
		t.Fatalf("entry missing: %+v", f.Entries)
	}
	childPid := f.Entries[0].PID

	cancel() // simulates SIGTERM to the wrapper
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancel")
	}
	if runErr != nil {
		t.Fatalf("run err: %v", runErr)
	}
	if code == 0 {
		t.Log("exit code 0 after signal is acceptable only if the child exited cleanly")
	}
	f, _ = a.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("entry not removed: %+v", f.Entries)
	}
	if (liveness.OS{}).PidAlive(childPid) {
		t.Fatalf("child %d still alive", childPid)
	}
}

func TestRunCustomEnvName(t *testing.T) {
	a, _ := newApp(t)
	port := freePort(t)
	t.Setenv("HM_TEST_LISTENER", "1")
	t.Setenv("HM_TEST_ENV_NAME", "VITE_PORT")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = runner.Run(ctx, a, runner.Options{Port: port, EnvNames: []string{"VITE_PORT"}, Args: listenerArgs("VITE_PORT"), ListenTimeout: 5 * time.Second, KillTimeout: 2 * time.Second}, register(a))
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !(liveness.OS{}).PortListening(port) {
		time.Sleep(50 * time.Millisecond)
	}
	listening := (liveness.OS{}).PortListening(port)
	cancel()
	<-done
	if !listening {
		t.Fatal("VITE_PORT was not injected")
	}
}

func TestRunPropagatesChildExitCode(t *testing.T) {
	a, _ := newApp(t)
	code, err := runner.Run(context.Background(), a, runner.Options{Port: freePort(t), Args: []string{"sh", "-c", "exit 3"}, ListenTimeout: time.Second, KillTimeout: time.Second}, register(a))
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Fatalf("code %d", code)
	}
	f, _ := a.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("entry should be removed after exit: %+v", f.Entries)
	}
}

func TestGuard(t *testing.T) {
	sameUID := func(int) (int, error) { return os.Getuid(), nil }
	if err := runner.Guard(1, sameUID); err == nil {
		t.Fatal("pid 1 must be refused")
	}
	if err := runner.Guard(os.Getpid(), sameUID); err == nil {
		t.Fatal("own pid must be refused")
	}
	if err := runner.Guard(99999, func(int) (int, error) { return os.Getuid() + 1, nil }); err == nil {
		t.Fatal("other uid must be refused")
	}
	if err := runner.Guard(99999, sameUID); err != nil {
		t.Fatalf("same uid should pass: %v", err)
	}
}

func TestTerminateKillsProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := runner.Terminate(cmd.Process.Pid, true, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if (liveness.OS{}).PidAlive(cmd.Process.Pid) {
		t.Fatal("still alive")
	}
}
```
Note `listenerArgs` ignores its parameter; keep the signature so both tests read the same. The `-test.run=^$` flag makes the re-executed test binary run zero tests and reach `TestMain`'s listener branch immediately.

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/runner/`
Expected: FAIL, package missing.

- [ ] **Step 3: Implement**

`internal/runner/runner.go`:
```go
// Package runner starts a dev server under harbormaster's supervision.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

// Options controls one supervised run.
type Options struct {
	Port          int
	Label         string
	EnvNames      []string // extra env names to set to the port, PORT is always set
	Args          []string
	ListenTimeout time.Duration
	KillTimeout   time.Duration
}

// Register creates the registry entry for the spawned child.
type Register func(port, pid int, cmdLine, label string) (registry.Entry, error)

// Run spawns opts.Args in its own process group with the port injected,
// registers it, forwards termination signals, and removes the entry on exit.
func Run(ctx context.Context, a *app.App, opts Options, register Register) (int, error) {
	if len(opts.Args) == 0 {
		return 3, errors.New("no command given")
	}
	if opts.ListenTimeout == 0 {
		opts.ListenTimeout = 60 * time.Second
	}
	if opts.KillTimeout == 0 {
		opts.KillTimeout = 10 * time.Second
	}

	cmd := exec.Command(opts.Args[0], opts.Args[1:]...) //nolint:gosec // running the user's command is the purpose
	cmd.Stdin = os.Stdin
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(opts.Port))
	for _, n := range opts.EnvNames {
		cmd.Env = append(cmd.Env, n+"="+strconv.Itoa(opts.Port))
	}
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return 3, fmt.Errorf("start %q: %w", opts.Args[0], err)
	}
	pid := cmd.Process.Pid

	entry, err := register(opts.Port, pid, ident.RedactCmd(opts.Args), opts.Label)
	if err != nil {
		_ = Terminate(pid, true, opts.KillTimeout)
		_ = cmd.Wait()
		return 4, fmt.Errorf("register: %w", err)
	}
	defer unregister(a, entry)

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	go warnIfNotListening(a, opts.Port, opts.ListenTimeout, waitErr)

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	select {
	case err := <-waitErr:
		return exitCode(err), nil
	case <-sigs:
	case <-ctx.Done():
	}
	_ = Terminate(pid, true, opts.KillTimeout)
	err = <-waitErr
	return exitCode(err), nil
}

func unregister(a *app.App, e registry.Entry) {
	_ = a.Store.Update(func(f *registry.File) error {
		kept := f.Entries[:0]
		for _, x := range f.Entries {
			if x.ID != e.ID {
				kept = append(kept, x)
			}
		}
		f.Entries = kept
		return nil
	})
	_ = a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "exited", At: a.Clock()})
}

func warnIfNotListening(a *app.App, port int, timeout time.Duration, done <-chan error) {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		case <-tick.C:
			if a.Prober.PortListening(port) {
				return
			}
		}
	}
	fmt.Fprintf(a.Stderr, "harbormaster: warning: nothing listening on port %d after %s; the entry stays registered while the process lives\n", port, timeout)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}
```

`internal/runner/terminate.go`:
```go
package runner

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Guard refuses pids harbormaster must never signal, even with --force (spec §10).
func Guard(pid int, uid func(int) (int, error)) error {
	if pid <= 1 {
		return fmt.Errorf("refusing to signal pid %d", pid)
	}
	if pid == os.Getpid() {
		return errors.New("refusing to signal my own pid")
	}
	u, err := uid(pid)
	if err != nil {
		return fmt.Errorf("cannot determine owner of pid %d: %w", pid, err)
	}
	if u != os.Getuid() {
		return fmt.Errorf("pid %d belongs to uid %d, not you", pid, u)
	}
	return nil
}

// Terminate sends SIGTERM (to the process group when group is true), waits up
// to timeout for exit, then SIGKILL.
func Terminate(pid int, group bool, timeout time.Duration) error {
	if err := sendTerm(pid, group); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return sendKill(pid, group)
}
```

`internal/runner/terminate_unix.go`:
```go
//go:build unix

package runner

import (
	"errors"
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func target(pid int, group bool) int {
	if group {
		return -pid
	}
	return pid
}

func sendTerm(pid int, group bool) error {
	err := syscall.Kill(target(pid, group), syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func sendKill(pid int, group bool) error {
	err := syscall.Kill(target(pid, group), syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
```

`internal/runner/terminate_windows.go`:
```go
//go:build windows

package runner

import (
	"os"
	"os/exec"
)

func setProcessGroup(*exec.Cmd) {}

func sendTerm(pid int, _ bool) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	return p.Kill()
}

func sendKill(pid int, group bool) error { return sendTerm(pid, group) }

func alive(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test -race ./internal/runner/`
Expected: PASS (5 tests). If `TestRunRegistersInjectsPortAndUnregistersOnSignal` hangs, check that `cancel()` reaches the `ctx.Done()` branch and that `Terminate` targets `-pid`.

- [ ] **Step 5: Commit**

```bash
git add internal/runner
git commit -m "feat(runner): supervised run with port injection, process group, signal forwarding"
```

---

### Task 11: `run` and `kill` commands, `release --kill`

**Files:**
- Create: `internal/cli/run.go`, `internal/cli/kill.go`
- Modify: `internal/cli/release.go` (replace `terminateEntry` stub), `internal/cli/root.go`
- Test: `internal/cli/run_kill_test.go`

**Interfaces:**
- Consumes: `runner.Run`, `runner.Terminate`, `runner.Guard`, `liveness.PidUID`, `app.MyPort`, `newEntry`.
- Produces: `terminateEntry(a *app.App, e registry.Entry) error` (Guard then Terminate; group=true only when `e.Cmd` does not start with `claimed pid`), `hm run`, `hm kill`.

- [ ] **Step 1: Write failing tests**

`internal/cli/run_kill_test.go`:
```go
package cli_test

import (
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/registry"
)

func TestRunRefusesForeignPort(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3000, PID: 11, Session: "s2", Worktree: "/wt/b", Label: "theirs"})
	err := h.run("run", "--port", "3000", "--", "true")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "theirs") {
		t.Fatalf("code %d err %v", c, err)
	}
}

func TestRunRefusesUnregisteredListener(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.listening[3000] = true
	h.app.PidOnPort = func(int) (int, string, bool) { return 555, "node", true }
	err := h.run("run", "--port", "3000", "--", "true")
	if c := exitCode(err); c != 2 || !strings.Contains(err.Error(), "555") {
		t.Fatalf("code %d err %v", c, err)
	}
}

func TestRunRequiresCommand(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if c := exitCode(h.run("run", "--port", "3000")); c != 3 {
		t.Fatalf("code %d", c)
	}
}

func TestKillForeignDeniedOwnAllowed(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3001, PID: 11, Session: "s2"})
	if c := exitCode(h.run("kill", "3001")); c != 1 {
		t.Fatalf("foreign should be denied, got %d", c)
	}
	if c := exitCode(h.run("kill", "3999")); c != 2 {
		t.Fatalf("unregistered free port should be exit 2, got %d", c)
	}
}
```
The own-pid kill path is exercised end-to-end in Task 12's smoke test, since it needs a real child process.

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/cli/ -run 'TestRun|TestKill'`
Expected: FAIL, unknown command "run"/"kill".

- [ ] **Step 3: Implement**

`internal/cli/run.go`:
```go
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/runner"
)

func newRun(a *app.App) *cobra.Command {
	var port int
	var label string
	var envNames []string
	cmd := &cobra.Command{
		Use:   "run [--port N] [--label TEXT] [--env NAME] -- <command...>",
		Short: "Start a dev server on this worktree's port and register it",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return exitf(ExitUsage, "usage: harbormaster run [flags] -- <command...>")
			}
			resolved, err := resolveRunPort(a, port)
			if err != nil {
				return err
			}
			reg := func(p, pid int, cmdLine, lbl string) (registry.Entry, error) {
				e := newEntry(a, p, pid, cmdLine, lbl)
				err := a.Store.Update(func(f *registry.File) error {
					if x, ok := findByPort(f, p); ok && !ident.Owns(a.Ident, x, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d owned by %s", p, ownerLine(x, a.Clock()))
					}
					f.Entries = append(f.Entries, e)
					return nil
				})
				return e, err
			}
			fmt.Fprintf(a.Stderr, "harbormaster: port %d, %s\n", resolved, strings.Join(args, " "))
			code, err := runner.Run(cmd.Context(), a, runner.Options{
				Port: resolved, Label: label, EnvNames: envNames, Args: args,
				ListenTimeout: 60 * time.Second, KillTimeout: 10 * time.Second,
			}, reg)
			if err != nil {
				return exitf(code, "%v", err)
			}
			if code != 0 {
				return &ExitError{Code: code, Msg: fmt.Sprintf("%s exited with %d", args[0], code)}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port to use (default: $PORT, then this worktree's deterministic port)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	cmd.Flags().StringArrayVar(&envNames, "env", nil, "additional env var name to set to the port (PORT is always set)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// resolveRunPort applies spec §6.1 steps 1-2.
func resolveRunPort(a *app.App, flag int) (int, error) {
	if flag == 0 {
		if v := getenv("PORT"); v != "" {
			p, err := parsePort(v)
			if err != nil {
				return 0, err
			}
			flag = p
		}
	}
	if flag == 0 {
		p, err := a.MyPort()
		if err != nil {
			return 0, exitf(ExitRegistry, "%v", err)
		}
		return p, nil
	}
	res, code := checkPort(a, flag)
	switch code {
	case ExitOK:
		return flag, nil
	case ExitDenied:
		return 0, exitf(ExitDenied, "port %d owned by %s. Run `harbormaster port` for this worktree's port.", flag, res.Owner)
	default:
		return 0, exitf(code, "port %d is held by %s", flag, res.Owner)
	}
}
```
Add to `internal/cli/root.go` a package-level `var getenv = os.Getenv` so tests can override it if needed, and register `newRun(a), newKill(a)`.

`internal/cli/kill.go`:
```go
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/runner"
)

func newKill(a *app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "kill <port>",
		Short: "Stop the registered server on a port (yours only, unless --force)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := parsePort(args[0])
			if err != nil {
				return err
			}
			f, err := a.Store.Load()
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			e, ok := findByPort(f, port)
			if !ok {
				return exitf(ExitUnregistered, "port %d is not registered; harbormaster only kills servers it knows about", port)
			}
			if !force && !ident.Owns(a.Ident, e, a.Git.Worktree) {
				return exitf(ExitDenied, "port %d owned by %s. Use --force only if you are sure.", port, ownerLine(e, a.Clock()))
			}
			if err := terminateEntry(a, e); err != nil {
				return exitf(ExitDenied, "%v", err)
			}
			_ = a.Store.Update(func(f *registry.File) error {
				kept := f.Entries[:0]
				for _, x := range f.Entries {
					if x.ID != e.ID {
						kept = append(kept, x)
					}
				}
				f.Entries = kept
				return nil
			})
			_ = a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "killed", At: a.Clock()})
			fmt.Fprintf(a.Stdout, "killed pid %d on port %d\n", e.PID, port)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "kill even if owned by another session")
	return cmd
}

// terminateEntry applies the safety guard, then terminates. Entries created by
// `run` own a process group; claimed pids are signaled individually.
func terminateEntry(a *app.App, e registry.Entry) error {
	if err := runner.Guard(e.PID, liveness.PidUID); err != nil {
		return err
	}
	group := !strings.HasPrefix(e.Cmd, "claimed pid")
	return runner.Terminate(e.PID, group, 10*time.Second)
}
```
Remove the `terminateEntry` stub from `release.go`.

- [ ] **Step 4: Run all tests and lint**

Run: `go test -race ./... && make lint && make coverage-check`
Expected: PASS, lint clean, coverage ≥ 80 %. If coverage is short, add table cases to `detect`-free areas first: `output.go` (`age` buckets, `shortPath`) and `ports` edge cases.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(cli): run and kill with ownership guard"
```

---

### Task 12: End-to-end smoke test script and README usage

**Files:**
- Create: `scripts/smoke.sh`
- Modify: `README.md`, `Makefile` (add `smoke` target)

**Interfaces:**
- Consumes: built binary. Produces: `make smoke`, run in CI on macOS and Linux.

- [ ] **Step 1: Write the smoke script**

`scripts/smoke.sh`:
```bash
#!/usr/bin/env bash
# End-to-end check of run/ls/check/kill with two simulated sessions.
set -euo pipefail

BIN="${BIN:-./bin/harbormaster}"
export HARBORMASTER_HOME
HARBORMASTER_HOME="$(mktemp -d)"
trap 'rm -rf "$HARBORMASTER_HOME"; kill 0 2>/dev/null || true' EXIT

fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }

port="$("$BIN" port)"
[[ "$port" =~ ^[0-9]+$ ]] || fail "port not numeric: $port"
[[ "$("$BIN" port)" == "$port" ]] || fail "port not deterministic"

# Session A starts a server.
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=A "$BIN" run --port "$port" --label "smoke A" -- python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1 &
for _ in $(seq 1 50); do "$BIN" ls --json | grep -q "\"port\": $port" && break; sleep 0.1; done
"$BIN" ls | grep -q "smoke A" || fail "A not listed"

# Session B: check says foreign, run is refused, kill is refused.
set +e
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=B "$BIN" check "$port"; rc=$?
set -e
[[ $rc -eq 1 ]] || fail "check should exit 1 for foreign, got $rc"
set +e
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=B "$BIN" run --port "$port" -- true; rc=$?
set -e
[[ $rc -eq 1 ]] || fail "run should be refused on foreign port, got $rc"
set +e
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=B "$BIN" kill "$port"; rc=$?
set -e
[[ $rc -eq 1 ]] || fail "kill should be refused for foreign, got $rc"

# Session A kills its own.
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=A "$BIN" kill "$port" || fail "own kill failed"
for _ in $(seq 1 50); do "$BIN" ls | grep -q "no registered servers" && break; sleep 0.1; done
"$BIN" ls | grep -q "no registered servers" || fail "entry not removed after kill"
"$BIN" history | grep -q killed || fail "history missing killed record"

echo "SMOKE OK"
```
`chmod +x scripts/smoke.sh`. Add to `Makefile`:
```make
smoke: build
	./scripts/smoke.sh
```
Add `- run: make smoke` after `make coverage-check` in the `test` job of `.github/workflows/ci.yml`.

- [ ] **Step 2: Run it**

Run: `make smoke`
Expected: `SMOKE OK`. If `python3` is missing, the script fails at the `run` line; document `python3` as a test-only requirement in CONTRIBUTING.md.

- [ ] **Step 3: Update README usage section**

Append to `README.md` under Usage:
```markdown
### How ownership works

Each entry records the agent session that started it (from `CLAUDE_SESSION_ID`
or `HARBORMASTER_SESSION`). `kill` and `release` refuse entries that belong to
another session unless `--force`. Even `--force` never touches pid 1, your own
pid, or another user's process.

### Ports

`harbormaster port` prints a stable port for the current worktree, derived from
its path and kept inside `HARBORMASTER_BASE`..`HARBORMASTER_BASE+HARBORMASTER_RANGE`
(default 3000-3999). Put it in `.env.local` once and your OAuth redirects stay valid.

### Exit codes

0 ok · 1 denied/foreign · 2 unregistered conflict · 3 usage · 4 registry error
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test: end-to-end smoke script; docs: usage"
```

---

### Task 13: Publish repository with protections

**Files:** none in-tree. GitHub settings only.

**Interfaces:** Produces the public repo `moeritze/harbormaster` with `main` protected and security features on, so Plan 2 can open PRs.

- [ ] **Step 1: Confirm with the user**

Creating a public repository is outward-facing. Stop and ask: "Ready to create public repo github.com/moeritze/harbormaster and push `main`?" Proceed only on yes.

- [ ] **Step 2: Create and push**

```bash
cd <main checkout>   # the primary clone, on main, with the feature branch merged locally
git merge --ff-only <feature-branch>
gh repo create moeritze/harbormaster --public --source . --push \
  --description "Registry of local dev servers for multi-session, multi-worktree development. Agents stop killing each other's ports."
```

- [ ] **Step 3: Enable security features**

```bash
R=moeritze/harbormaster
gh api -X PATCH "repos/$R" -f has_wiki=false -f has_projects=false -f delete_branch_on_merge=true -f allow_auto_merge=true
gh api -X PUT "repos/$R/vulnerability-alerts"
gh api -X PUT "repos/$R/automated-security-fixes"
gh api -X PUT "repos/$R/private-vulnerability-reporting"
gh api -X PATCH "repos/$R" --input - <<'JSON'
{"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}
JSON
```

- [ ] **Step 4: Protect `main` with a ruleset**

```bash
gh api -X POST "repos/$R/rulesets" --input - <<'JSON'
{
  "name": "main",
  "target": "branch",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/heads/main"], "exclude": []}},
  "rules": [
    {"type": "deletion"},
    {"type": "non_fast_forward"},
    {"type": "required_linear_history"},
    {"type": "pull_request", "parameters": {"required_approving_review_count": 0, "dismiss_stale_reviews_on_push": true, "require_code_owner_review": false, "require_last_push_approval": false, "required_review_thread_resolution": true}},
    {"type": "required_status_checks", "parameters": {"strict_required_status_checks_policy": true, "required_status_checks": [
      {"context": "test (ubuntu-latest)"}, {"context": "test (macos-latest)"}, {"context": "lint"}, {"context": "analyze"}, {"context": "dependency-review"}
    ]}}
  ]
}
JSON
```
Approving review count is 0 because there is one maintainer; raise it when a second maintainer joins.

- [ ] **Step 5: Verify CI is green**

```bash
gh run list --limit 5
gh run watch
```
Expected: `ci`, `codeql`, `scorecard` succeed on `main`. If `scorecard` fails on `publish_results`, the repo must be public and the workflow must run from the default branch; re-run once.

- [ ] **Step 6: Record status**

Append to `README.md` badges for CI, CodeQL, and OpenSSF Scorecard, commit via a PR (branch protection now applies):
```bash
git checkout -b docs/badges
git commit -am "docs: add CI and security badges"
gh pr create --fill
```
Merge once checks pass.

---

## Self-review

**Spec coverage.** §5 registry → Tasks 3, 5, 8. §6 CLI (all commands except `hook`, `install`, `uninstall`, which are Plan 2/3) → Tasks 9, 11. §6.1 run steps 1-5 → Tasks 10, 11 (`resolveRunPort` covers steps 1-2, `runner.Run` covers 3-5). §6.5 exit codes → Task 1 constants, asserted in Tasks 9, 11, 12. §7 ports → Task 7, `MyPort` in Task 9. §10 process safety → `Guard` in Task 10, `terminateEntry` in Task 11; data safety (0600/0700, redaction, no network) → Tasks 3, 8; hook input safety → Plan 2. §11 layout → matches File Structure minus `detect`, `hooks`, `install` (Plan 2/3). §12 → Tasks 2, 13. §13 tests → each task; concurrent writers in Task 3; two concurrent runs same port in Task 12 smoke; safety tests in Task 10. `history` capped 1000 → Task 3.

**Placeholder scan.** No TBD/TODO. Every code step shows code. Stubs in Tasks 3 (`pruneEntries`) and 9 (`terminateEntry`) are explicit and replaced in Tasks 5 and 11.

**Type consistency.** `registry.Prober` used by `app.App.Prober`, `liveness.OS` satisfies it. `runner.Register` signature matches the closure in `cli/run.go`. `checkPort` returns `(checkResult, int)` in both `check.go` and `run.go`. `ownerLine(e, now)` used consistently. `Guard(pid, uid func(int)(int,error))` matches `liveness.PidUID`.
