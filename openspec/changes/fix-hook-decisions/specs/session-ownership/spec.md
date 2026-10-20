## Purpose

Decide which agent session may release or stop which registered dev server, so parallel sessions cannot stop each other's servers by accident.

## ADDED Requirements

### Requirement: hm port is stable
`harbormaster port` SHALL print the worktree's deterministic port regardless of whether it is in use. When it is taken by another owner or another process, it SHALL print a warning on stderr naming the port `run` would use instead, and still exit 0.

#### Scenario: Deterministic port busy
- **WHEN** the deterministic port of this worktree is registered to another session
- **THEN** stdout is the deterministic port and stderr names the fallback port

#### Scenario: Deterministic port free
- **WHEN** the deterministic port is free
- **THEN** stdout is the deterministic port and stderr is empty
