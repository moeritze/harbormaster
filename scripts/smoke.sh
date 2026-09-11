#!/usr/bin/env bash
# End-to-end check of run/ls/check/kill with two simulated sessions.
set -euo pipefail

BIN="${BIN:-./bin/harbormaster}"
export HARBORMASTER_HOME
HARBORMASTER_HOME="$(mktemp -d)"
bg=""
trap 'rm -rf "$HARBORMASTER_HOME"; if [[ -n "${bg:-}" ]]; then kill "$bg" 2>/dev/null || true; wait "$bg" 2>/dev/null || true; fi' EXIT

fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }

port="$("$BIN" port)"
[[ "$port" =~ ^[0-9]+$ ]] || fail "port not numeric: $port"
[[ "$("$BIN" port)" == "$port" ]] || fail "port not deterministic"

# Session A starts a server.
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=A "$BIN" run --port "$port" --label "smoke A" -- python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1 &
bg=$!
for _ in $(seq 1 50); do "$BIN" ls --json | grep -q "\"port\": $port" && break; sleep 0.1; done
"$BIN" ls | grep -q "smoke A" || fail "A not listed"

# Session B: check says foreign, run is refused, kill is refused.
set +e
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=B "$BIN" check "$port"; rc=$?
set -e
[[ $rc -eq 1 ]] || fail "check should exit 1 for foreign, got $rc"
set +e
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=B "$BIN" run --port "$port" -- true; rc=$?
set -e
[[ $rc -eq 1 ]] || fail "run should be refused on foreign port, got $rc"
set +e
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=B "$BIN" kill "$port"; rc=$?
set -e
[[ $rc -eq 1 ]] || fail "kill should be refused for foreign, got $rc"

# Session A kills its own.
HARBORMASTER_AGENT=claude HARBORMASTER_SESSION=A "$BIN" kill "$port" || fail "own kill failed"
for _ in $(seq 1 50); do "$BIN" ls | grep -q "no registered servers" && break; sleep 0.1; done
"$BIN" ls | grep -q "no registered servers" || fail "entry not removed after kill"
# One shutdown, one history record. Which reason it carries depends on who
# removed the entry first: for a `run`-supervised server the wrapper's own
# prune usually gets there before kill does, so it reads pid_dead rather than
# killed. What must never happen again is the same shutdown logged three times.
for _ in $(seq 1 50); do [[ "$("$BIN" history --json | grep -c '"reason"')" -ge 1 ]] && break; sleep 0.1; done
records="$("$BIN" history --json | grep -c '"reason"')"
[[ "$records" -eq 1 ]] || fail "expected 1 history record for one shutdown, got $records"
"$BIN" history | grep -qE 'killed|exited|pid_dead' || fail "history missing the shutdown record"

echo "SMOKE OK"
