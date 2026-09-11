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

- Never runs with elevated privileges.
- Never signals a pid that is not in its registry. `--force` overrides
  ownership only; it still refuses pid 1, harbormaster's own pid, and any pid
  owned by another user.
- No network access beyond loopback liveness dials. No telemetry.
- Registry files are mode 0600 in a 0700 directory.

## Supply chain (planned)

Signed releases (Sigstore cosign, keyless), an SBOM, and SLSA provenance are
planned for the first tagged release. Until then, build from source with
`go install github.com/moeritze/harbormaster/cmd/harbormaster@<commit>`.
