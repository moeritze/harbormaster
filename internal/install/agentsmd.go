package install

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// AGENTS.md is read by Cursor, Codex and other agents that have no hook
// API harbormaster supports. The block is delimited so a later install
// replaces it in place and an uninstall removes exactly it.
const (
	agentsStart = "<!-- harbormaster:start -->"
	agentsEnd   = "<!-- harbormaster:end -->"
)

// AgentsBlock is the text placed between the markers.
const AgentsBlock = `## harbormaster (local dev-server registry)

Start dev servers with ` + "`harbormaster run --label \"<task>\" -- <command>`" + ` (alias ` + "`hm run`" + `) so other agent sessions and worktrees can see them. Before touching a port, run ` + "`harbormaster check <port>`" + `; ` + "`harbormaster ls`" + ` shows every registered server and ` + "`harbormaster port`" + ` prints this worktree's port. Never ` + "`kill`" + `, ` + "`pkill`" + `, ` + "`fuser -k`" + ` or ` + "`lsof -ti | xargs kill`" + ` a port another session owns; use ` + "`harbormaster kill <port>`" + ` only for ports this session owns. Exit codes: 0 ok, 1 denied/foreign, 2 unregistered conflict, 3 usage, 4 registry error.`

// AgentsMD writes or refreshes the harbormaster block in path (created
// when missing). Report.Actions lists the change; DryRun writes nothing.
func AgentsMD(path string, dryRun bool) (Report, error) {
	r := Report{Settings: path}
	if err := refuseSymlink(path); err != nil {
		return r, err
	}
	cur, err := os.ReadFile(path) //nolint:gosec // path is the caller's chosen AGENTS.md
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return r, err
	}
	next, action := upsertBlock(string(cur), AgentsBlock)
	if action == "" {
		r.Skipped = append(r.Skipped, "AGENTS.md")
		r.Actions = append(r.Actions, "harbormaster block in "+path+" is current")
		return r, nil
	}
	r.Added = append(r.Added, "AGENTS.md")
	r.Actions = append(r.Actions, fmt.Sprintf("%s harbormaster block in %s", action, path))
	if dryRun {
		return r, nil
	}
	return r, os.WriteFile(path, []byte(next), 0o600) //nolint:gosec // path is the caller's chosen AGENTS.md
}

// AgentsMDUninstall removes the block; an AGENTS.md left empty is deleted.
func AgentsMDUninstall(path string, dryRun bool) (Report, error) {
	r := Report{Settings: path}
	if err := refuseSymlink(path); err != nil {
		return r, err
	}
	cur, err := os.ReadFile(path) //nolint:gosec // path is the caller's chosen AGENTS.md
	if errors.Is(err, os.ErrNotExist) {
		r.Actions = append(r.Actions, "no "+path)
		return r, nil
	}
	if err != nil {
		return r, err
	}
	next, found := removeBlock(string(cur))
	if !found {
		r.Actions = append(r.Actions, "no harbormaster block in "+path)
		return r, nil
	}
	r.Removed = append(r.Removed, "AGENTS.md")
	if strings.TrimSpace(next) == "" {
		r.Actions = append(r.Actions, "remove "+path+" (only held the harbormaster block)")
		if dryRun {
			return r, nil
		}
		return r, os.Remove(path)
	}
	r.Actions = append(r.Actions, "remove harbormaster block from "+path)
	if dryRun {
		return r, nil
	}
	return r, os.WriteFile(path, []byte(next), 0o600) //nolint:gosec // path is the caller's chosen AGENTS.md
}

func upsertBlock(doc, block string) (string, string) {
	wrapped := agentsStart + "\n" + block + "\n" + agentsEnd + "\n"
	s, e := strings.Index(doc, agentsStart), strings.Index(doc, agentsEnd)
	if s >= 0 && e > s {
		existing := doc[s : e+len(agentsEnd)+1]
		if existing == wrapped {
			return doc, ""
		}
		return doc[:s] + wrapped + strings.TrimPrefix(doc[e+len(agentsEnd):], "\n"), "update"
	}
	if doc != "" && !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	if doc != "" {
		doc += "\n"
	}
	return doc + wrapped, "add"
}

func removeBlock(doc string) (string, bool) {
	s, e := strings.Index(doc, agentsStart), strings.Index(doc, agentsEnd)
	if s < 0 || e < s {
		return doc, false
	}
	rest := strings.TrimPrefix(doc[e+len(agentsEnd):], "\n")
	head := strings.TrimRight(doc[:s], "\n")
	if head != "" {
		head += "\n"
	}
	return head + rest, true
}
