# harbormaster — Design Spec

**Date:** 2026-09-10
**Status:** Draft, awaiting review
**License:** MIT
**Repo:** github.com/moeritze/harbormaster

## 1. Problem

Developers increasingly run several coding-agent sessions in parallel, one per git worktree. Each session starts its own dev server. Sessions are unaware of each other, which causes two failure modes:

1. **Port collision.** Session B starts `npm run dev` while session A already holds port 3000. The dev server silently moves to 3001. Session B then tests, screenshots, or curls port 3000, which is session A's app.
2. **Cross-session interference.** Session B runs `lsof -ti:3000 | xargs kill` or `pkill -f next` to "free" a port and kills session A's server. Session-end cleanup routines do the same.

No existing tool tracks *who* owns a local port, *from which worktree*, *for which task*, in a way agents can query and respect.

## 2. Goals

- A single, agent-agnostic CLI (`harbormaster`) that maintains a self-cleaning registry of local dev servers: port, pid, repo, worktree, branch, owning agent session, task label.
- Deterministic, stable port per worktree so OAuth redirects, Firebase auth domains, and webhooks can be configured once.
- Adapters that make agents (Claude Code, Cursor, Codex CLI) aware of the registry at session start, stop them from killing foreign servers, and release their own servers at session end.
- One shared skill document, in the open Agent Skills format, loaded by every supported agent.
- Publishable as open source with secure-by-default CI from the first commit.

## 3. Non-goals (v1)

- Background daemon or socket server.
- Reverse proxy / `*.localhost` hostnames.
- Multi-service project config (`.harbormaster.toml`). Planned v2.
- Web dashboard.
- Windows guarantees. Best effort only.
- Docker port mappings.
- npm distribution wrapper. Planned v2.

## 4. Architecture

Approach: **lockfile registry + process wrapper + agent hooks.** No long-running process.

```
                ┌──────────────────────────────┐
  agent hooks ─►│  harbormaster hook <agent> <ev>  │
                └──────────────┬───────────────┘
                               │ normalized event
                ┌──────────────▼───────────────┐
  harbormaster run ►│        core (Go)             │◄── harbormaster ls/check/kill/...
                │ registry · liveness · ports  │
                │ detect · gitctx · runner     │
                └──────────────┬───────────────┘
                               │ flock + atomic write
                ┌──────────────▼───────────────┐
                │ ~/.local/state/harbormaster/     │
                │   registry.json  history.jsonl│
                └──────────────────────────────┘
```

The registry file is the single source of truth. Every command reads it, prunes dead entries, and operates on live state. A daemon or proxy can be layered later without changing the schema.

## 5. Registry

**Location:** `$XDG_STATE_HOME/harbormaster/registry.json`, falling back to `~/.local/state/harbormaster/`. Override with `HARBORMASTER_HOME`. Sibling files: `registry.lock` (flock target), `history.jsonl` (append-only log of pruned/released entries).

**Schema (version 1):**

```json
{
  "version": 1,
  "entries": [
    {
      "id": "01J9...ULID",
      "port": 3417,
      "pid": 48213,
      "cmd": "npm run dev",
      "repo": "/Users/m/repositories/riftbinder",
      "worktree": "/Users/m/repositories/riftbinder-wt/auth-feature",
      "branch": "feat/auth",
      "agent": "claude",
      "session": "session_0132...",
      "label": "auth feature login flow",
      "started_at": "2026-09-10T17:13:00Z",
      "host_user": "moritzroeseler",
      "spawned": true
    }
  ]
}
```

`spawned` is set only by `run`; entries without it are signaled individually.

**Invariants:**

- Every read prunes. An entry is dead when its pid is gone **or** its port is not listening on two consecutive probes 100 ms apart (after the listen grace). Both are checked to guard against pid reuse. Pruned entries are appended to `history.jsonl` with a `reason` field.
- Every write is: acquire `flock` → read → prune → mutate → write to temp file → `rename` over `registry.json`. Lock held for the whole sequence. Lock wait timeout 5 s, then fail loudly.
- `agent` and `session` come from the environment: `CLAUDE_SESSION_ID`, Cursor and Codex equivalents (exact variable names verified during planning), else `agent: "human"`, `session: ""`.
- `repo` is the parent of `git rev-parse --git-common-dir` (shared across worktrees). `worktree` is `git rev-parse --show-toplevel`. Both optional; the tool works outside git with `repo`/`worktree` empty.
- `history.jsonl` is capped at 1000 lines, oldest trimmed on write.

**Ownership:** an entry is "mine" if `session` matches the caller's session id. If the caller has no session id, fall back to matching `worktree`. `--force` bypasses ownership for `kill` and `release`.

## 6. CLI

```
harbormaster run [--port N] [--label "..."] [--env NAME] -- <cmd...>
harbormaster ls  [--json]
harbormaster check <port>
harbormaster claim <port> [--pid P] [--label "..."]
harbormaster release <port> | --session ID | --all-mine
harbormaster kill <port> [--force] [--json]
harbormaster port
harbormaster gc
harbormaster history [--json]
harbormaster hook <agent> <event>
harbormaster install <agent> [--project] [--agents-md]
harbormaster uninstall <agent> [--project]
harbormaster version
```

### 6.1 `run`

1. Resolve port: `--port` flag, then `PORT` env, then deterministic worktree port (§7).
2. If the port has a foreign registry entry: exit 1, print owner (agent, session, worktree, label), suggest `harbormaster port`. If the port is held by an unregistered process: exit 1, print pid and command from the OS.
3. Spawn `<cmd>` with `PORT=N` injected. `--env NAME` injects `NAME=N` instead of or in addition to `PORT` (repeatable). Stdio is passed through so the agent sees server output.
4. Register an entry with the child's pid immediately after spawn. Poll until the port is listening, timeout 60 s. On timeout warn on stderr, keep the entry (the pid check will prune it if the process dies).
5. On child exit, or on SIGINT/SIGTERM/SIGHUP to `harbormaster`, forward the signal to the child's process group, wait up to 10 s, then SIGKILL. Remove the entry. Exit with the child's exit code.

The child runs in its own process group so that killing the wrapper kills the whole server tree (Next.js spawns workers).

### 6.2 `check <port>`

Exit 0 if free or owned by caller. Exit 1 if foreign-owned, printing the owner. Exit 2 if held by an unregistered process. `--json` for machine use.

### 6.3 `kill <port>`

Refuses ports that are not registered (exit 2). Refuses foreign-owned entries unless `--force` (exit 1). `--force` overrides ownership, never registration: use `claim` first for a process harbormaster did not start. Sends SIGTERM to the registered pid's process group, waits 10 s, SIGKILL, removes entry.

### 6.4 `release`

Removes entries without killing unless `--kill` is given. `--session ID` releases everything for a session (used by session-end hooks, with `--kill`). `--all-mine` releases the caller's entries.

### 6.5 Output

Human-readable table by default. `--json` everywhere for hooks and agents. Errors go to stderr as a single actionable line, for example:

```
port 3000 owned by claude session 0132… in ../riftbinder-wt/auth ("login flow", 12m). Run `harbormaster port` for this worktree's port.
```

Exit codes: 0 ok, 1 foreign/denied, 2 unregistered conflict, 3 usage error, 4 registry/lock error.

## 7. Port allocation

**Deterministic per worktree (v1):**

```
port = base + (fnv1a64(abs(worktree_path)) % range)
```

Defaults: `base=3000`, `range=1000` → 3000–3999. Configurable via `HARBORMASTER_BASE`, `HARBORMASTER_RANGE`. If the computed port is busy (registry or OS), probe upward within the range until free. Outside git, the hash input is the current directory.

`harbormaster port` prints the resolved port so it can be used in `.env.local`, OAuth settings, etc.

**v2 (out of scope, but the design leaves room):** `.harbormaster.toml` in the repo declaring named services with base ports; per-worktree offset applied to each. The registry schema needs no change; entries gain an optional `service` field.

## 8. Command detection (`internal/detect`)

A regex table classifies shell commands into:

- `kill`: `kill`, `pkill`, `killall`, `fuser -k`, `lsof -ti:N`, `npx kill-port`, `kill-port`. Extract target ports and pids where present.
- `server_start`: `npm|pnpm|yarn|bun run dev|start|serve|preview`, `next dev`, `vite`, `astro dev`, `nuxt dev`, `remix dev`, `ng serve`, `python -m http.server`, `uvicorn`, `flask run`, `rails s`, `php artisan serve`, `go run` with `--port`, `cargo run` with `--port`. Detect whether already wrapped in `harbormaster run`.
- `port_ref`: explicit `--port N`, `-p N`, `PORT=N`, `localhost:N`, `:N` in URLs.

The classifier is heuristic by design. A miss degrades to awareness-only. A false positive produces a deny the user can override with `--force` or by setting `HARBORMASTER_STRICT=0`. The table is data-driven and tested against a fixture list of at least 50 commands.

## 9. Agent adapters

### 9.1 Internal event model

`harbormaster hook <agent> <event>` reads the agent's JSON from stdin, normalizes it, runs core logic, and writes the agent's expected JSON to stdout.

```
Normalized: { kind: session_start | pre_shell | post_shell | session_end,
              cmd, session, cwd, agent }
```

| Internal      | Claude Code          | Cursor                 | Codex CLI          |
|---------------|----------------------|------------------------|--------------------|
| session_start | `SessionStart`       | `sessionStart`         | `SessionStart`     |
| pre_shell     | `PreToolUse` (Bash)  | `beforeShellExecution` | `PreToolUse`       |
| post_shell    | `PostToolUse` (Bash) | `afterShellExecution`  | `PostToolUse`      |
| session_end   | `SessionEnd`         | `stop`                 | `SessionEnd`/`Stop`|

Exact event names, payload shapes, and response formats for Cursor and Codex are to be verified against current documentation during the planning phase. The adapter layer isolates these differences; core logic does not change per agent.

### 9.2 Core hook behavior

| Event         | Behavior |
|---------------|----------|
| session_start | Return additional context: `harbormaster ls` table, this worktree's deterministic port, and a one-line rule: "Start dev servers with `harbormaster run -- <cmd>`." |
| pre_shell     | (1) `kill` class targeting a foreign-owned port or pid → **deny** with owner info. (2) `server_start` not wrapped in `harbormaster run` → inject context suggesting the wrapper; **allow** by default, **deny** when `HARBORMASTER_STRICT=1`. (3) `port_ref` to a foreign-owned port in a server-start command → **deny**. |
| post_shell    | If a `server_start` ran unwrapped in the background, best-effort `claim`: scan for new listeners whose process cwd is under this worktree and register them. |
| session_end   | `release --session <id> --kill`. |

Hook execution must complete in under 100 ms in the common case (registry read + regex). No network, no git subprocess unless the registry is being written.

### 9.3 Install

`harbormaster install claude|cursor|codex` merges hook definitions into the user-level config (`~/.claude/settings.json`, `~/.cursor/hooks.json`, `~/.codex/config.toml`). Existing config is preserved; harbormaster entries are marked so `uninstall` can remove exactly what it added. `--project` writes to the repo-level equivalent instead. A backup of the config file is written next to it before the first modification.

`--agents-md` appends a short fenced block to `AGENTS.md` for agents without hooks.

### 9.4 Shared skill

`skills/harbormaster/SKILL.md` is the single canonical skill, written in the Agent Skills format (`.agents/skills/` convention). `install` symlinks it into `~/.claude/skills/harbormaster`, `~/.cursor/skills/harbormaster`, `~/.codex/skills/harbormaster`, or `.agents/skills/harbormaster` with `--project`. The Claude plugin in `adapters/claude-plugin/` references the same file via symlink. Content: when to use, `run` usage, reading `ls`, handling a foreign-port error, never bypass with `--force` without asking the user. Target length under 60 lines.

## 10. Safety procedures

These apply to the tool's runtime behavior, separate from repository security (§12).

**Process safety**
- `kill` and `release --kill` never signal a pid that is not in the registry. `--force` overrides ownership only; it still refuses pid 1, the caller's own pid, and any pid whose owner uid differs from the caller.
- Signals are sent to the process group created by `run`, never to arbitrary groups. For `claim`ed pids (not spawned by harbormaster), only the pid itself is signaled.
- Never runs anything with elevated privileges. No `sudo` anywhere in the codebase or docs.
- Hooks default to **allow** on any internal error. A broken harbormaster must never block an agent from working. Errors are logged to `$HARBORMASTER_HOME/hook-errors.log`.

**Data safety**
- The registry stores only paths, pids, ports, branch names, session ids, and user-supplied labels. Never environment variables, never command arguments beyond the command line as typed. `run` redacts values of environment-style arguments matching `*KEY*`, `*SECRET*`, `*TOKEN*`, `*PASSWORD*` in the stored `cmd` field.
- No network access, no telemetry, no update checks. `harbormaster version` is offline.
- Registry and history are user-readable only (`0600`). The state directory is `0700`.
- `install` never writes outside the agent config directories and the skill directories listed in §9.3. Every file it touches is listed in `--dry-run` output.

**Hook input safety**
- Hook stdin is untrusted. Parse with strict JSON, cap at 1 MiB, reject on malformed input with an allow decision.
- Command strings are classified with regex only. They are never evaluated, never passed to a shell.
- Session ids and labels are sanitized to printable characters, max 256 bytes, before being stored or printed, to prevent terminal escape injection into agent context.

**Agent context safety**
- Context injected at `session_start` is plain text with no instructions beyond the single usage rule. Labels from other sessions are shown verbatim but sanitized as above; they are data, not instructions, and the skill says so.

## 11. Repository layout

```
harbormaster/
  cmd/harbormaster/main.go
  internal/
    registry/      schema, lock, prune, atomic write, history
    liveness/      pid alive, port listening (darwin, linux; windows best effort)
    ports/         deterministic allocation, probing
    runner/        run: spawn, env inject, process group, signal forwarding
    detect/        shell command classifier + fixtures
    gitctx/        repo, worktree, branch discovery
    hooks/         core event logic
    hooks/adapters/claude, cursor, codex
    install/       config merge/unmerge per agent
  skills/harbormaster/SKILL.md
  adapters/claude-plugin/
    .claude-plugin/plugin.json
    hooks/hooks.json
    skills/harbormaster -> ../../../skills/harbormaster (symlink)
  docs/superpowers/specs/
  .github/workflows/
  .goreleaser.yaml
  SECURITY.md  CONTRIBUTING.md  CODE_OF_CONDUCT.md  LICENSE  README.md
```

Single Go module, Go 1.27, standard library plus at most: a CLI framework (cobra or stdlib flag; decision at planning), `golang.org/x/sys` for flock and process groups, a ULID package. Dependencies are kept minimal and reviewed.

## 12. Public-repo security and CI (from first commit)

**Repository hygiene**
- `LICENSE` (MIT), `SECURITY.md` with a private disclosure channel (GitHub private vulnerability reporting enabled) and a response-time commitment, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` (Contributor Covenant).
- Branch protection on `main`: PRs required, at least the CI checks below required, force-push disabled, linear history.
- Dependabot enabled for Go modules and GitHub Actions, weekly, grouped.
- GitHub secret scanning and push protection enabled.
- No secrets in the repo, ever. Release signing uses GitHub OIDC (keyless), not stored keys.

**CI (`.github/workflows/ci.yml`), on every PR and push to `main`)**
- Matrix: `macos-latest`, `ubuntu-latest`. Windows job runs but is `continue-on-error` until Windows support is declared.
- Steps: `go vet`, `staticcheck`, `golangci-lint` (with `gosec` enabled), `go test -race -cover ./...`, coverage threshold 80 % on `internal/`.
- `govulncheck` on every run.
- All third-party actions pinned to full commit SHAs, not tags.
- Workflow `permissions:` set to `contents: read` at the top level; jobs that need more request it explicitly.
- Fork PRs run with read-only tokens. No `pull_request_target` usage.

**Security scanning**
- CodeQL workflow for Go on PRs and a weekly schedule.
- OpenSSF Scorecard workflow, weekly, badge in README.
- Dependency review action on PRs (fails on known-vulnerable additions or license conflicts).

**Release (`.github/workflows/release.yml`, on tag `v*`)**
- goreleaser builds darwin/linux (amd64, arm64), windows best effort.
- Artifacts signed with cosign keyless (Sigstore, GitHub OIDC). Checksums file signed.
- SLSA level 3 provenance via `slsa-framework/slsa-github-generator`.
- SBOM (SPDX) attached to each release via `anchore/sbom-action` or goreleaser's built-in.
- Homebrew tap `moeritze/homebrew-tap` updated by goreleaser using a fine-grained token scoped to that single repo, stored as a repository secret, rotated yearly.
- Release workflow requires an environment with a required reviewer (the maintainer), so a compromised PR cannot publish.

**Supply-chain for users**
- README documents: verify with `cosign verify-blob`, or install via `go install github.com/moeritze/harbormaster/cmd/harbormaster@vX.Y.Z` to build from source.
- `go.sum` committed. `GOFLAGS=-mod=readonly` in CI.

**Hooks and agent configs in this repo**
- The Claude plugin's `hooks.json` and any `.claude/settings.json` in the repo only invoke `harbormaster` itself. No inline shell. Reviewers check this on every PR touching `adapters/`.

## 13. Testing strategy

- **Unit:** registry (lock contention, prune, atomic write, history cap), ports (determinism, wraparound, probing), detect (fixture table ≥ 50 commands with expected class and extracted ports), adapters (golden stdin → golden stdout per agent and event), install (merge and unmerge round-trip leaves the config byte-identical).
- **Integration:** `run` a tiny Go HTTP listener, assert the entry appears, kill the child, assert it is pruned. Two concurrent `run`s for the same port: second fails with exit 1 and owner info. 20 concurrent writers under lock produce a valid file.
- **Hook end-to-end:** feed fixture `PreToolUse` JSON for `lsof -ti:3000 | xargs kill` while 3000 is foreign-owned, assert deny with owner text. Feed the same while free, assert allow.
- **Safety tests:** `kill --force` refuses pid 1, own pid, other-uid pid. Hook with malformed stdin returns allow. Label with escape sequences is stored sanitized.
- TDD throughout. Tests are the acceptance criteria for each task in the implementation plan.

## 14. Milestones

1. **Core:** registry, liveness, ports, gitctx, `ls`, `check`, `claim`, `release`, `port`, `gc`, `history`.
2. **Runner:** `run`, `kill`, signal handling, process groups.
3. **Detect + hooks core:** classifier, normalized events, decisions.
4. **Claude adapter + plugin + shared skill.** Install on this machine, dogfood.
5. **Cursor and Codex adapters** after verifying their hook APIs.
6. **Repo security + CI + release** per §12. Done in parallel with milestone 1 so every PR is checked from the start.
7. **Docs + v0.1.0 release.**

## 15. Open questions

None blocking. Items deferred to planning: CLI framework choice, exact Cursor/Codex hook payloads, Windows liveness approach.
