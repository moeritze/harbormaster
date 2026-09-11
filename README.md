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

Each entry records the agent session that started it (from `CLAUDE_SESSION_ID`
or `HARBORMASTER_SESSION`). `kill` and `release` refuse entries that belong to
another session unless `--force`. `--force` overrides ownership, never
registration: harbormaster never signals a pid it has no entry for, so `claim`
a server it did not start before killing it. Even `--force` never touches
pid 1, your own pid, or another user's process.

If you have no session id at all, any `kill` or `release` refuses to act on
anything -- the port form, `--all-mine` and `--session <id>` alike -- and tells
you to set `HARBORMASTER_SESSION` (or pass `--force`). Reads -- `ls`, `check`,
`run`'s conflict check -- still fall back to matching the worktree, so nothing
about the day-to-day flow changes.

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
an existing one: if it finds a symlink, a directory group or other can write,
or one owned by another user, it refuses to run and says which `chmod` fixes
it.

## Security

See SECURITY.md, including what harbormaster explicitly does not guarantee:
ownership separates concurrent sessions of one OS user, it is not a boundary
against a hostile process running as you. No network access, no telemetry.
