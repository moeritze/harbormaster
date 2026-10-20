## Purpose

Keep the shared registry (registry.json, history.jsonl) valid, fast to update and recoverable while many sessions use it concurrently.

## ADDED Requirements

### Requirement: Registered ports are valid TCP ports
`run` SHALL reject an explicit `--port` outside 1–65535 with a usage error before starting the command; `--port 0` SHALL mean "use the default port".

#### Scenario: Port out of range
- **WHEN** the user runs `hm run --port 70000 -- npm run dev` or `--port -5`
- **THEN** the command exits with the usage code, starts nothing and registers nothing
