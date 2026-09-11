# Security Policy

## Reporting

Report vulnerabilities privately via GitHub's "Report a vulnerability" button
on this repository (Security tab). Do not open public issues for security
problems.

You will get an acknowledgement within 3 business days and a fix or
mitigation plan within 14 days for confirmed issues.

## Scope

The `harbormaster` binary: registry handling, port allocation, the `run`
supervisor, and signal handling in `kill`/`release`.

## Runtime guarantees

- Never runs with elevated privileges. No `sudo` anywhere, and no shell: your
  command is executed directly, never through `sh -c`.
- Never signals a pid that is not in its registry. `--force` overrides
  ownership only; it still refuses pid 1, harbormaster's own pid, and any pid
  owned by another user.
- `claim <port> --pid P` registers P only when the OS shows P listening on
  that port. Registration is what licenses `kill` to signal a process, so an
  unverifiable pid is refused unless you pass `--force`.
- Any `kill` or `release` refuses a caller with no session id unless
  `--force` -- the port form, `--all-mine` and `--session <id>` alike.
  Ownership has to be claimed, not inherited from being in the right
  directory.
- No network access beyond loopback liveness dials. No telemetry.
- Registry and history are written to a fresh randomly named temp file opened
  with `O_EXCL`, fsynced, then renamed into place, so no predictable temp path
  can be pre-planted. If `registry.json` or `history.jsonl` is a symlink,
  harbormaster refuses to read or write it. Both files are mode 0600.
- The state directory is created mode 0700. An existing one is validated, not
  repaired. A directory writable by group or other is refused with the `chmod`
  that fixes it. A symlinked directory, or one owned by another user, is
  refused outright -- there is no mode to correct: move the state dir or set
  `HARBORMASTER_HOME` to one you own.
- Values that other sessions wrote (worktree, repo, branch, command, label)
  and process names read from the OS are stripped of control characters and
  truncated before being printed, so a registry row cannot inject terminal
  escapes into your shell or an agent's context. `--json` output is the stored
  value, for machines to handle.
- `run` redacts secret-looking arguments (`*KEY*`, `*SECRET*`, `*TOKEN*`,
  `*PASSWORD*`) in the banner it prints and in the `cmd` field it stores, and
  rejects an `--env` name that is not a plain variable name or that would
  change how the child resolves programs and libraries (`PATH`, `LD_PRELOAD`
  and friends).
- A damaged, over-long line in `history.jsonl` is skipped rather than failing
  every later read and write. Registry failures exit with code 4.

## What harbormaster does not guarantee

- **Ownership is cooperative bookkeeping, not a security boundary.** It
  separates concurrent sessions of one OS user from each other. Any process
  running as that user can set `HARBORMASTER_SESSION` to another session's id
  and act as that session, or edit `registry.json` directly. harbormaster
  stops sessions from tripping over each other by accident; it does not
  contain a hostile process that already runs as you. When no agent sets a
  session id, the fallback session id is derived from the terminal or shell
  (`TMUX_PANE`, `ITERM_SESSION_ID`, `TERM_SESSION_ID`, or the parent pid) --
  it identifies a shell, not a secret, and anyone in the same shell or with
  the same parent pid can reproduce it.
- **It relies on external tools from `$PATH`**: `ps` (process owner), `lsof`
  (which pid holds a port), and `git` (repo, worktree, branch). If `$PATH` is
  under someone else's control, so are those answers.
- **File modes are only as good as the directory you point it at.** They are
  enforced for a state directory harbormaster created. A pre-existing
  directory is never re-chmodded: if it is a symlink, group- or
  other-writable, or owned by another user, harbormaster refuses to run
  instead of silently fixing or using it.
- **Windows is best effort.** There is no file locking there, so concurrent
  writers can interleave, and `kill` / `release --kill` do not work because
  the process-owner check they depend on is unavailable. `claim --pid` needs
  `--force` for the same reason: the port-to-pid lookup is unimplemented.

## Supply chain

Releases are produced only by `.github/workflows/release.yml`, which runs on
`v*` tags inside the `release` GitHub environment. That environment requires
the maintainer's approval before the job starts, so a pushed tag alone cannot
publish anything. Every action in the workflow is pinned to a commit SHA; the
SLSA generator is pinned to a release tag as its documentation requires.

Each release ships:

- `checksums.txt` (sha256 of every archive and SBOM) and
  `checksums.txt.sigstore.json`, a Sigstore bundle produced by cosign keyless
  signing with the workflow's GitHub OIDC identity. There are no stored
  signing keys.
- an SBOM (`*.sbom.json`, SPDX JSON, generated by syft) for every archive
- `harbormaster.intoto.jsonl`, SLSA v1 build provenance from
  `slsa-framework/slsa-github-generator`, attached by a second job a few
  minutes after the release first appears
- a Homebrew cask pushed to `moeritze/homebrew-tap` (only when the tap token
  is configured; the release does not depend on it)

Verify (requires cosign v3 or newer; older cosign needs `--new-bundle-format`):

    cosign verify-blob --bundle checksums.txt.sigstore.json \
      --certificate-identity-regexp '^https://github\.com/moeritze/harbormaster/\.github/workflows/release\.yml@refs/tags/v' \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
    sha256sum -c checksums.txt --ignore-missing
    slsa-verifier verify-artifact <archive> --provenance-path harbormaster.intoto.jsonl \
      --source-uri github.com/moeritze/harbormaster --source-tag <tag>

**Homebrew:** the cask's post-install hook removes the macOS quarantine
attribute from the installed binary; brew itself verifies only the cask's own
sha256, which goreleaser derives from the signed `checksums.txt`, not the
cosign signature or SLSA provenance directly.

Anything built from `main` with `go install …@main` is unsigned.
