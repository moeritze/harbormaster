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

### Ports

`harbormaster port` prints a stable port for the current worktree, derived from
its path and kept inside `HARBORMASTER_BASE`..`HARBORMASTER_BASE+HARBORMASTER_RANGE`
(default 3000-3999). Put it in `.env.local` once and your OAuth redirects stay valid.

### Exit codes

0 ok · 1 denied/foreign · 2 unregistered conflict · 3 usage · 4 registry error

## Security

See SECURITY.md. No network access, no telemetry. State lives in
`~/.local/state/harbormaster` with mode 0700.
