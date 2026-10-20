## Purpose

Classify an agent's shell command as a kill, a dev-server start, or neither, and extract the ports and pids it targets, so hooks can protect servers owned by other sessions.

## ADDED Requirements

### Requirement: Harbormaster clauses end at a newline
A `harbormaster`/`hm` subcommand clause SHALL end at the next newline as well as at `;`, `&`, `|`, so a kill on a following line is still classified.

#### Scenario: Kill on the line after hm ls
- **WHEN** the command is `hm ls` followed by a newline and `kill -9 4242`
- **THEN** it is classified kill with pid 4242

### Requirement: Common kill idioms are detected
The classifier SHALL classify as kill, with the targeted port or pid extracted:
`lsof` with `-t` among its flags in any order together with a port, `kill` with a pid list inside backticks or `$(...)`, process-group kills with a negative pid (with or without a preceding signal flag and `--`), path-qualified `kill` (`/bin/kill`, `/usr/bin/kill`), and `fuser` with `-k` anywhere among its flags.

#### Scenario: lsof flags in any order
- **WHEN** the command is `lsof -i :3000 -t | xargs kill -9` or `lsof -nP -iTCP:3000 -sTCP:LISTEN -t | xargs kill`
- **THEN** it is classified kill with port 3000

#### Scenario: Backtick substitution
- **WHEN** the command is ``kill -9 `lsof -t -i:3000` ``
- **THEN** it is classified kill with port 3000

#### Scenario: Group kill
- **WHEN** the command is `kill -9 -1234` or `kill -TERM -- -1234`
- **THEN** it is classified kill with target 1234 (the group id)

#### Scenario: Path-qualified kill
- **WHEN** the command is `/bin/kill 1234`
- **THEN** it is classified kill with pid 1234

#### Scenario: fuser with flags before -k
- **WHEN** the command is `fuser -n tcp -k 3000`
- **THEN** it is classified kill with port 3000

### Requirement: Package management is not a server start
Installing or removing packages SHALL NOT be classified as a dev-server start, and a package script SHALL only match `dev|start|serve|preview` when the script name ends there.

#### Scenario: Installing vite
- **WHEN** the command is `npm i -D vite` or `yarn add vite`
- **THEN** it is classified none

#### Scenario: Script with a dev- prefix
- **WHEN** the command is `npm run dev-db-migrate`
- **THEN** it is classified none

#### Scenario: Real dev script still detected
- **WHEN** the command is `npm run dev` or `npx vite`
- **THEN** it is classified server_start
