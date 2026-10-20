# process-termination Specification

## Purpose
Stop the processes harbormaster started or registered completely and only those, so no dev server survives its supervisor holding a port.

## Requirements

### Requirement: Signals during registration stop the child
`hm run` SHALL handle SIGINT, SIGTERM and SIGHUP from the moment the child is started. A signal received before registration completes SHALL terminate the child's process group before `run` exits.

#### Scenario: Ctrl-C while waiting for the registry lock
- **WHEN** `hm run -- <server>` is blocked registering and receives SIGINT
- **THEN** `run` terminates the child's process group, exits, and no process of that group remains

### Requirement: Group termination waits for every member
Terminating a process group SHALL wait until no process in the group remains, and SHALL send SIGKILL to the group if any member is still alive when the timeout expires, even if the group leader has already exited.

#### Scenario: Worker ignores SIGTERM
- **WHEN** `hm run -- sh -c '<worker that ignores TERM>'` is stopped and `sh` exits on SIGTERM while the worker ignores it
- **THEN** the worker receives SIGKILL after the kill timeout and the port is freed

#### Scenario: Group already empty
- **WHEN** every member of the group has exited
- **THEN** termination returns immediately without waiting for the timeout
