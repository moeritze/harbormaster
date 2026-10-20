## Purpose

Add and remove harbormaster's hooks and agent instructions in user-owned config files without damaging anything the user wrote.

## ADDED Requirements

### Requirement: Uninstall removes only harbormaster's commands
Uninstall SHALL remove only hook commands harbormaster installed. A hook entry that also contains other commands SHALL keep them; the entry SHALL be removed only when no command remains.

#### Scenario: Shared entry
- **WHEN** a PreToolUse entry's hooks are `[harbormaster hook claude PreToolUse, my-guard.sh]` and the user uninstalls
- **THEN** the entry remains with only `my-guard.sh`

#### Scenario: Entry with only our command
- **WHEN** an entry contains only harbormaster's command
- **THEN** the entry is removed, and the event key too if it becomes empty

### Requirement: Settings strings are preserved verbatim
Rewriting settings.json SHALL NOT escape `&`, `<` or `>` in any string.

#### Scenario: Command with &&
- **WHEN** settings.json has a user hook `cd x && make lint` and harbormaster installs
- **THEN** the rewritten file contains `cd x && make lint` literally

### Requirement: AGENTS.md is replaced atomically
Writing AGENTS.md SHALL go through a temporary file in the same directory renamed over the target, keeping the existing file mode.

#### Scenario: Existing file mode kept
- **WHEN** AGENTS.md has mode 0644 and the harbormaster block is added
- **THEN** the new AGENTS.md has mode 0644 and contains the block
