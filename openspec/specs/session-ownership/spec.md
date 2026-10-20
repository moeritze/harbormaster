# session-ownership Specification

## Purpose
Decide which agent session may release or stop which registered dev server, so parallel sessions cannot stop each other's servers by accident.

## Requirements

### Requirement: Release requires ownership of every matched entry
`release` SHALL refuse, without `--force`, every matched entry the caller does not own, regardless of which selector (`<port>`, `--session`, `--all-mine`) matched it. Passing `--session` with the caller's own id SHALL NOT grant ownership of an entry belonging to another session.

#### Scenario: Port of a foreign entry with own session id
- **WHEN** session A runs `hm release 3000 --session A --kill` and port 3000 is registered to session B
- **THEN** the command exits with the denied code, the entry stays, and B's process is not signalled

#### Scenario: Own session id still releases own entries
- **WHEN** session A runs `hm release --session A`
- **THEN** every entry of session A is removed

### Requirement: release --kill never orphans a server
With `--kill`, an entry SHALL only leave the registry if its process may be signalled (same guard as `kill`) and termination succeeds. Otherwise the entry SHALL remain registered and the command SHALL exit non-zero naming the port.

#### Scenario: Guard refuses the signal
- **WHEN** `hm release 3000 --kill` targets an own entry whose group signal is refused (no recorded start time)
- **THEN** the entry stays in the registry and the exit code is non-zero

#### Scenario: Termination fails
- **WHEN** the signal is allowed but termination returns an error
- **THEN** the entry is restored to the registry and the exit code is non-zero

### Requirement: hm port is stable
`harbormaster port` SHALL print the worktree's deterministic port regardless of whether it is in use. When it is taken by another owner or another process, it SHALL print a warning on stderr naming the port `run` would use instead, and still exit 0.

#### Scenario: Deterministic port busy
- **WHEN** the deterministic port of this worktree is registered to another session
- **THEN** stdout is the deterministic port and stderr names the fallback port

#### Scenario: Deterministic port free
- **WHEN** the deterministic port is free
- **THEN** stdout is the deterministic port and stderr is empty
