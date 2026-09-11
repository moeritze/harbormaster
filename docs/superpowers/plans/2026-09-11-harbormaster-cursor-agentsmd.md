# Harbormaster Cursor Adapter + AGENTS.md Implementation Plan (Plan 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Cursor sessions the same protection Claude Code sessions have (registry awareness, denied foreign kills, session-end cleanup) and give every other agent an AGENTS.md block.

**Architecture:** One more translator (`internal/hooks/adapters/cursor`) over the existing agent-agnostic hook core; one more installer over the existing settings helpers (`internal/install`); the AGENTS.md block is a marker-delimited region that install/uninstall own exclusively. Core decisions do not change.

**Tech Stack:** Go 1.27, cobra, `embed`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-10-harbormaster-design.md` §9.1, §9.3, §9.4.

## Global Constraints

Verified against https://cursor.com/docs/hooks and https://cursor.com/docs/context/rules on 2026-09-11:
- Cursor hooks file: `~/.cursor/hooks.json` (user) or `<project>/.cursor/hooks.json`; top level `{"version": 1, "hooks": {...}}`; entries are flat `{"command", "type": "command", "timeout", "matcher"?, "failClosed"?, "loop_limit"?}`.
- Events used: `sessionStart` (stdin `session_id` + common fields; stdout `{"env": {...}, "additional_context": "..."}`), `beforeShellExecution` (stdin `command`, `cwd`, `sandbox` + common fields; stdout `{"permission": "allow|deny|ask", "user_message", "agent_message"}`), `sessionEnd` (stdin `reason` ∈ completed|aborted|error|window_close|user_close; fire and forget). `afterShellExecution` is parsed but **not installed**: it documents no output fields, so the post-start nudge it would carry has nowhere to go.
- **Correction (2026-09-11 review):** `sessionStart`'s `env` output is available to *subsequent hook executions only* — it is **not** exported into the agent's shell. Ruling 2 below overstated this. Consequences, both implemented: (a) the Cursor scoped ownership check also treats an entry with `agent == "human"` and a non-empty `worktree` equal to the event's worktree as own, so a server the agent started with a plain `npm run dev` (registered as a `shell:<pid>` session) can still be managed by the conversation that started it; (b) the `sessionStart` `additional_context` carries one extra line naming the command that does attribute a server to the conversation: `HARBORMASTER_SESSION=<conversation_id> harbormaster run --label "<task>" -- <command>`.
- `sessionEnd` fires **once per conversation** (the per-turn event is `stop`, which harbormaster does not hook) and `completed` is the ordinary end of a piece of work, so for Cursor the hook **releases the registry entries and terminates nothing**, whatever the reason. Claude Code's `SessionEnd` is unchanged.
- Common stdin fields on every hook include `conversation_id`, `generation_id`, `hook_event_name`, `workspace_roots`.
- Exit 0 = use JSON; exit 2 = block; other = fail-open unless `failClosed`. No "no opinion" answer is documented for `beforeShellExecution`.
- Cursor rules: `.cursor/rules/*.mdc` with frontmatter `description`, `globs`, `alwaysApply`; user rules are UI-only; `AGENTS.md` at the project root is supported. No skills directory.
- Cursor exports `CURSOR_PROJECT_DIR`, `CURSOR_VERSION`, … but no session id, and nothing harbormaster emits reaches the agent's shell (see the correction above): identity reaches later *hook* executions through `sessionStart`'s `env`, and the agent itself through the `additional_context` line plus the same-worktree ownership fallback.
- OpenAI Codex: https://learn.chatgpt.com/docs/hooks documents `~/.codex/hooks.json` with Claude-like events and stdin (`session_id`, `cwd`, `hook_event_name`), but the PreToolUse decision shape was not verifiable → Codex support = AGENTS.md block (deferred adapter).
- Hooks always exit 0; failures logged to `hook-errors.log`; stdin cap 1 MiB (shared with the Claude path). Lint 0, race tests, coverage ≥ 80 %, `make smoke`. Commits: single subject + `Co-Authored-By: Claude Code <noreply@anthropic.com>`; never `Claude-Session:`.

## File Structure

```
internal/hooks/adapters/cursor/cursor.go       Parse(event, stdin) / Format(event, result) / FormatSession(event, session, result)
internal/hooks/adapters/cursor/cursor_test.go
internal/cli/hook.go                            + "cursor" agent
internal/install/cursor.go                      Cursor(CursorOptions) / CursorUninstall: hooks.json v1 merge, backup, symlink refusal, rule file
internal/install/agentsmd.go                    AgentsMD(path, dryRun) / AgentsMDUninstall: marker-delimited block
internal/install/cursor_test.go
internal/cli/install.go                         agents: claude | cursor | agents-md
skills/embed.go                                 + CursorRule() (SKILL.md → .mdc with description/alwaysApply)
README.md                                       "Cursor and other agents"
```

## Rulings (recorded during execution)
1. Cursor session identity = `conversation_id` (present on every hook), fallback `session_id`. Cost if wrong: entries keyed on a per-generation id would not match across a conversation.
2. `sessionStart` returns `env: {HARBORMASTER_AGENT: cursor, HARBORMASTER_SESSION: <conversation_id>}`. ~~so the agent's own `harbormaster` calls are owned by the conversation~~ — **retracted 2026-09-11**: that `env` reaches subsequent hook executions only, never the agent's shell. It is still emitted (the hook processes that read it are the ones deciding ownership), but the agent-facing gap is closed by the `additional_context` line and the same-worktree ownership fallback described in the Global Constraints correction.
3. Allow (with or without context) prints nothing for `beforeShellExecution`: an explicit `permission: allow` could bypass Cursor's own approval; the nudge is dropped. Cost: Cursor users get the guidance only via the rule/context.
4. Deny/Ask → `permission` deny/ask, `agent_message` = reason, short `user_message`.
5. Project installs also write `.cursor/rules/harbormaster.mdc` (`alwaysApply: false`, description from SKILL.md); user-level installs write hooks only (Cursor user rules are UI-only).
6. `install agents-md [--project DIR]` upserts a block between `<!-- harbormaster:start -->` and `<!-- harbormaster:end -->`; uninstall removes it and deletes the file if nothing else is left.
7. Codex adapter deferred (decision shapes unverified); AGENTS.md covers it.

---

### Task 1: Cursor adapter
Files: `internal/hooks/adapters/cursor/{cursor.go,cursor_test.go}`. Tests: parse beforeShellExecution (conversation_id → Session, cwd), fallback to `session_id`/`workspace_roots[0]`, afterShellExecution/sessionEnd (reason), garbage/unknown event errors; Format shapes (deny/ask JSON, allow → nil, afterShellExecution → nil, sessionEnd → nil); FormatSession (additional_context + env, nil when nothing to say). Implementation: see the committed file (`Parse`, `Format`, `FormatSession`).

### Task 2: `hook cursor <event>`
Files: `internal/cli/hook.go`, `internal/cli/hook_test.go`. Add the `cursor` case (sessionStart uses `FormatSession`); test: foreign kill → `"permission":"deny"`; sessionStart → env + additional_context; unwrapped `npm run dev` → empty stdout.

### Task 3: Cursor installer + AGENTS.md
Files: `internal/install/{cursor.go,agentsmd.go,cursor_test.go}`. `Cursor`/`CursorUninstall` mirror `Claude`/`ClaudeUninstall` (flat entries, `version: 1`, `ourCursorCommand` anchored regex, backup, symlink refusal, rule file only when `Rule != nil`). `AgentsMD`/`AgentsMDUninstall` with `upsertBlock`/`removeBlock`. Tests: shape, foreign entries preserved, idempotent, uninstall removes only ours, dry-run, lookalike command, AGENTS.md add/update/remove/delete-when-empty/dry-run.

### Task 4: CLI + rule rendering + docs
Files: `internal/cli/install.go`, `internal/cli/install_test.go`, `skills/embed.go` (`CursorRule`), `skills/embed_test.go`, `README.md`. `install|uninstall claude|cursor|agents-md [--project DIR] [--dry-run] [--command]`; `--project` defaults the command to bare `harbormaster`; `agents-md` defaults to the current directory.

## Self-review
Spec §9.1 (event model shared) ✓ Task 1; §9.3 install/uninstall with backup, `--project`, dry-run, marked entries, `--agents-md` ✓ Tasks 3–4; §9.4 shared skill delivered as a Cursor rule ✓ Task 4 (`.agents/skills` symlink dropped: neither Claude Code nor Cursor reads it). No placeholders. Names consistent: `cursor.Parse/Format/FormatSession`, `install.CursorOptions/Cursor/CursorUninstall/AgentsMD/AgentsMDUninstall`, `skills.CursorRule`.
