# harbormaster

[![ci](https://github.com/moeritze/harbormaster/actions/workflows/ci.yml/badge.svg)](https://github.com/moeritze/harbormaster/actions/workflows/ci.yml)
[![codeql](https://github.com/moeritze/harbormaster/actions/workflows/codeql.yml/badge.svg)](https://github.com/moeritze/harbormaster/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/moeritze/harbormaster/badge)](https://scorecard.dev/viewer/?uri=github.com/moeritze/harbormaster)

Registry of local dev servers for multi-session, multi-worktree development.
Knows which port belongs to which worktree, which agent session started it,
and for what task. Agents stop killing each other's servers.

Not affiliated with Phabricator's Harbormaster.

## Install

    go install github.com/moeritze/harbormaster/cmd/harbormaster@main

There is no tagged release yet, so `@main` builds the current tip. Tagged,
signed releases (cosign keyless, SBOM, SLSA provenance) are planned; pin a
commit with `@<commit>` if you want a fixed build today.

`make install` also links `hm` as a short alias.

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

What the hooks do: at session start Claude sees the registry and this worktree's port; before a shell command, a `kill`/`pkill`/`lsof -ti | xargs kill` aimed at a port another session owns is denied with the owner shown, and an unwrapped `npm run dev`-style start gets a nudge (`HARBORMASTER_STRICT=1` denies it); at session end the session's own servers are released and stopped. Hooks always fail open: any internal error allows the command and logs to `$HARBORMASTER_HOME/hook-errors.log`.

The same files are available as a plugin in `adapters/claude-plugin/` (`claude --plugin-dir adapters/claude-plugin`).

## Security

See SECURITY.md, including what harbormaster explicitly does not guarantee:
ownership separates concurrent sessions of one OS user, it is not a boundary
against a hostile process running as you. No network access, no telemetry.
