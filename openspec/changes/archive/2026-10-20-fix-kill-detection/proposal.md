## Why

The PreToolUse hook only protects a foreign server if `internal/detect` classifies the command as a kill. Audit findings #2, #3 and #16 show common kill idioms classified `none` (hook allows them) and harmless package-manager commands classified `server_start` (denied in STRICT mode). The README promises `lsof … | xargs kill` is denied; several spellings of it are not.

## What Changes

- #2: a newline ends an `hm`/`harbormaster` clause, like `;`, `&`, `|`. `hm ls\nkill -9 4242` is a kill.
- #3: classify as kill: `lsof` with `-t` anywhere among its flags plus a port (`lsof -i :3000 -t`, `lsof -nP -iTCP:3000 -sTCP:LISTEN -t`), kill inside backticks, group kills (`kill -9 -1234`, `kill -TERM -- -1234`), path-qualified kill (`/bin/kill 1234`), and `fuser` with `-k` after other flags (`fuser -n tcp -k 3000`). Extract the pid/port of each.
- #16: package installs/removals (`npm i|install|add|remove|uninstall`, `yarn add|remove`, `pnpm add|remove`, `bun add|remove`) are not server starts; a script name must end at a word boundary that is not `-` (`npm run dev-db-migrate` is not `dev`).

## Non-goals

- Parsing shell properly; detect stays a regex heuristic.
- Remote kills (`ssh host kill`), job specs (`kill %1`).

## Capabilities

### New Capabilities
- `kill-detection`: which shell commands count as kills or dev-server starts, and which ports/pids they target.

### Modified Capabilities

## Impact

- `internal/detect/detect.go`, `internal/detect/detect_test.go` only.
- README.md: the hook section's list of detected kill forms.
