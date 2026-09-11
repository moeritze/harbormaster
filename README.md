# harbormaster

[![ci](https://github.com/moeritze/harbormaster/actions/workflows/ci.yml/badge.svg)](https://github.com/moeritze/harbormaster/actions/workflows/ci.yml)
[![codeql](https://github.com/moeritze/harbormaster/actions/workflows/codeql.yml/badge.svg)](https://github.com/moeritze/harbormaster/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/moeritze/harbormaster/badge)](https://scorecard.dev/viewer/?uri=github.com/moeritze/harbormaster)

Registry of local dev servers for multi-session, multi-worktree development.
Knows which port belongs to which worktree, which agent session started it,
and for what task. Agents stop killing each other's servers.

Not affiliated with Phabricator's Harbormaster.

## Install

Homebrew cask: coming with the first release that publishes to `moeritze/homebrew-tap` (requires the maintainer's tap token). Until then use `go install` or the signed archives below.

Go toolchain, pinned to a tagged release:

    go install github.com/moeritze/harbormaster/cmd/harbormaster@v0.1.0

Or download an archive from the [releases page](https://github.com/moeritze/harbormaster/releases). Every release ships `checksums.txt`, a Sigstore bundle for it, an SBOM per archive, and SLSA v1 provenance. Verify before you trust a download (requires cosign v3 or newer; older cosign needs `--new-bundle-format`):

    cosign verify-blob --bundle checksums.txt.sigstore.json \
      --certificate-identity-regexp '^https://github\.com/moeritze/harbormaster/\.github/workflows/release\.yml@refs/tags/v' \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
    sha256sum -c checksums.txt --ignore-missing
    slsa-verifier verify-artifact harbormaster_*_darwin_arm64.tar.gz \
      --provenance-path harbormaster.intoto.jsonl \
      --source-uri github.com/moeritze/harbormaster --source-tag v0.1.0

`go install …@main` builds the unsigned tip of `main`; use it only for development. `make install` from a checkout also links `hm` as a short alias.

## Usage

    hm run --label "auth flow" -- npm run dev   # start a server on this worktree's port
    hm ls                                       # who owns what
    hm port                                     # this worktree's deterministic port
    hm check 3000                               # is 3000 free, mine, or foreign?
    hm kill 3000                                # only your own; --force for others

When you run `harbormaster run` from an interactive terminal, the child's stdin
is detached, so dev-server keyboard shortcuts are unavailable; agents (piped
stdin) are unaffected.

### How ownership works

Each entry records the agent session that started it. `HARBORMASTER_SESSION`
always overrides everything else. Otherwise harbormaster looks for
`CLAUDE_CODE_SESSION_ID` (what Claude Code's Bash tool exports) or, failing
that, the older `CLAUDE_SESSION_ID`. With none of those set, it falls back to
a stable id for the current shell: `TMUX_PANE`, `ITERM_SESSION_ID`, or
`TERM_SESSION_ID` (as `tmux:...` / `iterm:...` / `term:...`), or else
`shell:<parent pid>`. `kill` and `release` refuse entries that belong to
another session unless `--force`. `--force` overrides ownership, never
registration: harbormaster never signals a pid it has no entry for, so `claim`
a server it did not start before killing it. Even `--force` never touches
pid 1, your own pid, or another user's process.

The only way to end up with no session id at all is setting `HARBORMASTER_AGENT`
without `HARBORMASTER_SESSION` -- every other path always yields a non-empty
id. In that case any `kill` or `release` refuses to act on anything -- the
port form, `--all-mine` and `--session <id>` alike -- and tells you to set
`HARBORMASTER_SESSION` (or pass `--force`). Reads -- `ls`, `check`, `run`'s
conflict check -- still fall back to matching the worktree, so nothing about
the day-to-day flow changes.

`claim <port> --pid P` registers P only if the OS agrees that P is the process
listening on that port; `claim --force` registers it anyway, which you need
when `lsof` is unavailable (including on Windows).

### Ports

`harbormaster port` prints a stable port for the current worktree, derived from
its path and kept inside `HARBORMASTER_BASE`..`HARBORMASTER_BASE+HARBORMASTER_RANGE`
(default 3000-3999). Put it in `.env.local` once and your OAuth redirects stay valid.

### Exit codes

0 ok · 1 denied/foreign · 2 unregistered conflict · 3 usage · 4 registry error

### State directory

State lives in the first of these that is set:

1. `HARBORMASTER_HOME`
2. `$XDG_STATE_HOME/harbormaster`
3. `~/.local/state/harbormaster`

It holds `registry.json`, `history.jsonl`, and the `registry.lock` flock
target. harbormaster creates the directory with mode 0700 and never re-chmods
an existing one. A directory group or other can write is refused with the
`chmod` that fixes it; a symlinked directory, or one owned by another user, is
refused outright -- move the state dir or set `HARBORMASTER_HOME` to one you
own.

## Claude Code integration

    harbormaster install claude            # user-level: ~/.claude/settings.json + ~/.claude/skills/harbormaster
    harbormaster install claude --project . # per-repo: .claude/settings.json + .claude/skills/harbormaster
    harbormaster install claude --dry-run  # show what would change
    harbormaster uninstall claude          # remove exactly what was added

What the hooks do: at session start Claude sees the registry and this worktree's port; before a shell command, a `kill`/`pkill`/`lsof -ti | xargs kill` aimed at a port another session owns is denied with the owner shown, a kill with no port or pid in it (`pkill -f node`) while other sessions have servers running asks you first, and an unwrapped `npm run dev`-style start gets a nudge (`HARBORMASTER_STRICT=1` denies it); at session end the session's own servers are released and stopped, unless the session merely cleared or resumed its transcript. Hooks never auto-approve anything: an allow carries context but leaves Claude Code's own permission prompt exactly as it was. They always fail open: any internal error allows the command and logs to `$HARBORMASTER_HOME/hook-errors.log`.

Set `HARBORMASTER_HOOKS=0` in Claude Code's environment to switch the hooks off
without uninstalling them: every deny and ask becomes an allow whose reason is
attached as context instead. Every deny and ask reason names this escape hatch,
so a session that hits a wrong decision can always get past it.

A `--project` install writes the bare name `harbormaster` into a file that gets
committed, because this machine's absolute path is wrong in every other
checkout. That only works if the agent can find the binary: an app launched
from the Dock, Spotlight or a `.desktop` entry runs no login shell, so its PATH
is roughly `/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin` -- without
`~/.local/bin`, `~/go/bin` or any version manager's shims. `install --project`
checks those directories and warns when the bare name is not there; if you see
that warning and the hooks do not fire, reinstall with
`--command /absolute/path/to/harbormaster` (pinned to this machine) or put the
binary somewhere on that PATH.

The same files are available as a plugin in `adapters/claude-plugin/` (`claude --plugin-dir adapters/claude-plugin`).

## Cursor and other agents

    harbormaster install cursor              # user-level: ~/.cursor/hooks.json
    harbormaster install cursor --project .  # per-repo: .cursor/hooks.json + .cursor/rules/harbormaster.mdc
    harbormaster install agents-md           # AGENTS.md block in the current directory (Codex, Cursor, others)
    harbormaster uninstall cursor|agents-md  # remove exactly what was added

Cursor hooks (verified against https://cursor.com/docs/hooks on 2026-09-11):
`sessionStart` injects the registry and this worktree's port; `beforeShellExecution`
denies kills of ports other sessions own and asks on blind kills (`pkill`,
`killall`); `sessionEnd` releases the conversation's entries. Cursor documents no
"no opinion" answer for shell hooks, so harbormaster prints nothing on allow and
the wrap-your-server nudge is not delivered to Cursor; the rule file and the
session-start context carry that guidance. `afterShellExecution` is not installed:
it has no output fields, so there is nowhere to put the answer.

Two Cursor-specific behaviours are worth knowing:

- **Identity.** `sessionStart`'s `env` output is handed to *subsequent hook
  executions* only -- it does not reach the shell the agent runs commands in, so
  it cannot make the agent's own `harbormaster run` calls carry the conversation
  id. The session-start context therefore spells out the one command that does:
  `HARBORMASTER_SESSION=<conversation_id> harbormaster run --label "<task>" -- <command>`.
  Failing that, harbormaster treats a server registered from a plain shell (agent
  `human`) *in the same worktree* as belonging to the conversation, so the agent
  can still manage what it just started. Servers in other worktrees, and servers
  owned by another agent session, stay foreign.
- **Session end never kills.** Cursor's `sessionEnd` fires once per conversation
  with `reason` in `completed | aborted | error | window_close | user_close`;
  "completed" is the ordinary end of a piece of work, and the per-turn event is
  `stop`, which harbormaster does not hook. So for Cursor the hook releases the
  registry entries and stops nothing -- a dev server you are still looking at
  survives the conversation that started it. Stop it yourself with
  `harbormaster kill <port>`. (Claude Code's `SessionEnd` does stop the session's
  own servers, as before.)

Cursor deny and ask reasons name the escape hatch that works there: remove the
harbormaster entries from `~/.cursor/hooks.json`, or launch Cursor with
`HARBORMASTER_HOOKS=0` in its environment.

The `--command` note above applies to `install cursor --project` in exactly the
same way: Cursor is a GUI app, so a bare `harbormaster` it cannot find means
hooks that never fire, and `install --project` warns when that is the case.

OpenAI Codex documents hooks too (https://learn.chatgpt.com/docs/hooks), but their decision payloads are not yet verified, so Codex gets the AGENTS.md block for now.

## Security

See SECURITY.md, including what harbormaster explicitly does not guarantee:
ownership separates concurrent sessions of one OS user, it is not a boundary
against a hostile process running as you. No network access, no telemetry.
