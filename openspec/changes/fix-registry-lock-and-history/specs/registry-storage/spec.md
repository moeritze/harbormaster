## Purpose

Keep the shared registry (registry.json, history.jsonl) valid, fast to update and recoverable while many sessions use it concurrently.

## ADDED Requirements

### Requirement: Pruning many entries writes history once
A single registry update that prunes N entries SHALL write history.jsonl once, not N times.

#### Scenario: Fifty stale entries
- **WHEN** a load prunes 50 entries whose ports are closed
- **THEN** all 50 records are appended to history in one write and the load finishes within the prune budget

### Requirement: Hook reads never wait for the registry lock
The read-only view used by hooks SHALL NOT take the registry lock. It SHALL return the last fully written registry, and SHALL report a damaged or unsupported file as an error without moving it.

#### Scenario: Lock held by a slow writer
- **WHEN** another process holds the registry lock for longer than 1 s and a hook reads the registry
- **THEN** the hook gets the current entries immediately and can deny a foreign kill

#### Scenario: Damaged file on peek
- **WHEN** registry.json is not valid JSON and a hook reads it
- **THEN** the read returns an error and registry.json is left in place

### Requirement: Start times are recorded only at registration
A registry entry's start time SHALL only be set when the entry is created. Reads and prunes SHALL NOT fill in a missing start time, because the pid may already belong to another process. An entry without a start time SHALL never be signalled as a process group.

#### Scenario: Entry without a start time survives a prune
- **WHEN** an entry has no start time and `hm ls` prunes the registry
- **THEN** the stored entry still has no start time

#### Scenario: Group kill stays refused
- **WHEN** `hm kill` targets a spawned entry without a start time
- **THEN** the group signal is refused and the entry stays registered

### Requirement: Versionless registry is quarantined
A registry.json that parses but has version 0 (empty object, `null`, missing version) SHALL be quarantined like unparsable JSON, and commands SHALL continue from an empty registry. A version greater than the supported one SHALL still fail closed without quarantine.

#### Scenario: Empty object
- **WHEN** registry.json contains `{}` and the user runs `hm ls`
- **THEN** the file is moved aside, a "quarantined" history record is written, and `hm ls` succeeds with no entries
