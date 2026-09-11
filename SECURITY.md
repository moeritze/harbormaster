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
