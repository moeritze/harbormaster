## Purpose

Let Claude Code and Cursor hooks stop an agent from killing or colliding with another session's dev server, answering fast and failing open.

## ADDED Requirements

### Requirement: Hooks ignore dead registry entries
Before denying or asking about a shell command, the hook SHALL disregard registry entries whose process is no longer alive.

#### Scenario: Restart after a crash
- **WHEN** a foreign entry for port 3000 has a dead pid and the agent runs `npm run dev -- --port 3000`
- **THEN** the hook does not deny

#### Scenario: Kill of a dead pid
- **WHEN** a foreign entry has dead pid 4242 and the agent runs `kill 4242`
- **THEN** the hook does not deny

#### Scenario: Live foreign entry still protected
- **WHEN** the foreign entry's pid is alive
- **THEN** the hook denies as before

### Requirement: Hook port hint does no registry write
The port named in hook context and deny reasons SHALL be computed from the entries the hook already read, without taking the registry lock, pruning or probing ports.

#### Scenario: Deny under lock contention
- **WHEN** another process holds the registry lock and the hook denies a server start
- **THEN** the deny reason names this worktree's port and the hook does not wait for the lock

### Requirement: SessionEnd finishes within the hook timeout
SessionEnd's registry update SHALL give up waiting for the lock, and bound its pruning, early enough to finish within the installed SessionEnd hook timeout; on giving up it SHALL log and allow.

#### Scenario: Lock held at session end
- **WHEN** the registry lock is held for longer than the SessionEnd timeout
- **THEN** the hook returns before the timeout, logs the failure, and exits 0
