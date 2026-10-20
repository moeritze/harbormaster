## 1. Regression tests (write first, must fail)

- [x] 1.1 In `internal/detect/detect_test.go` `TestClassify` table, add rows: `"hm ls\nkill -9 4242"` → Kill pid 4242; `lsof -i :3000 -t | xargs kill -9`, `lsof -nP -iTCP:3000 -sTCP:LISTEN -t | xargs kill`, ``kill -9 `lsof -t -i:3000` ``, `fuser -n tcp -k 3000` → Kill port 3000; `kill -9 -1234`, `kill -TERM -- -1234` → Kill pid 1234; `/bin/kill 1234` → Kill pid 1234
- [x] 1.2 Add rows: `npm i -D vite`, `npm install vite`, `yarn add vite`, `pnpm add -D vite`, `npm run dev-db-migrate` → None; keep `npm run dev`, `npx vite`, `npm run dev -- --port 3000` → ServerStart
- [x] 1.3 Keep existing negative rows passing (`xargs kill -9` with no pid is not pid 9; `hm kill 3000` alone is not a kill)

## 2. Fix (`internal/detect/detect.go`)

- [x] 2.1 `hmClauseRe`, `hmRunPrefixRe`: `[^;&|]` → `[^;&|\n]` (all three occurrences)
- [x] 2.2 Add `` ` `` and `/` handling to the clause-start prefix of the `kill` pattern and `killPidRe` (path-qualified: `(?:/\S*/)?kill`); add `` ` `` to `lsof-t` prefix
- [x] 2.3 `kill` pattern and `killPidRe`: allow signal flags before `--`, and a negative final target after signal flags (`kill -9 -1234`); extract the absolute value as pid
- [x] 2.4 `lsof-t` pattern and its `portPatterns` entry: match `-t` anywhere among flags and the port from `-i[TCP]:N` / `-i :N` in any order (two alternations, flag set before or after `-i`)
- [x] 2.5 `fuser-k`: `fuser\s+(?:-\S+\s+(?:\w+\s+)?)*-k\b`; widen the matching `portPatterns` entry the same way
- [x] 2.6 `notServerRe`: add `(?:npm|pnpm|yarn|bun)\s+(?:i|install|add|remove|rm|uninstall)\b`; `pkg-script`: replace trailing `\b` with `(?:$|[\s;&|)])`
- [x] 2.7 `go test -race ./internal/detect` green, including `TestClassifyNeverPanicsOnGarbage`

## 3. Docs

- [x] 3.1 README.md hook section: list the detected kill forms (lsof any flag order, group kill, path-qualified kill, fuser)
