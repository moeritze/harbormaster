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
// when missing). Report.Actions lists the change; DryRun writes nothing. A
// file whose markers are not exactly one balanced pair is refused untouched.
func AgentsMD(path string, dryRun bool) (Report, error) {
	r := Report{Settings: path}
	if err := refuseSymlink(path); err != nil {
		return r, err
	}
	cur, err := os.ReadFile(path) //nolint:gosec // path is the caller's chosen AGENTS.md
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return r, err
	}
	next, action, err := upsertBlock(string(cur), AgentsBlock)
	if err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
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
// Markers that are not exactly one balanced pair are refused untouched.
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
	next, found, err := removeBlock(string(cur))
	if err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
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

// blockBounds locates the harbormaster block: start is the offset of the
// start marker and end the offset just past the end marker, including the
// single newline that terminates it when there is one (a file that ends
// exactly at the end marker has none). start is -1 when the document holds
// no block at all.
//
// Anything but a clean, balanced, single pair of markers is an error rather
// than a guess. AGENTS.md is a file humans edit: a stray marker means the
// file was hand-edited or two installs collided, and rewriting a region
// harbormaster cannot delimit would eat the user's own text.
func blockBounds(doc string) (start, end int, err error) {
	nStart, nEnd := strings.Count(doc, agentsStart), strings.Count(doc, agentsEnd)
	switch {
	case nStart > 1 || nEnd > 1:
		return 0, 0, fmt.Errorf("found %d %q and %d %q markers; harbormaster owns exactly one block — remove the extra markers by hand and run this again", nStart, agentsStart, nEnd, agentsEnd)
	case nStart != nEnd:
		return 0, 0, fmt.Errorf("found %d %q and %d %q markers; harbormaster will not rewrite an unbalanced block — fix the markers by hand and run this again", nStart, agentsStart, nEnd, agentsEnd)
	case nStart == 0:
		return -1, -1, nil
	}
	s := strings.Index(doc, agentsStart)
	// The end marker counts only after the start marker: an end that
	// precedes its start delimits nothing.
	rel := strings.Index(doc[s:], agentsEnd)
	if rel < 0 {
		return 0, 0, fmt.Errorf("%q appears before %q; harbormaster will not rewrite an unbalanced block — fix the markers by hand and run this again", agentsEnd, agentsStart)
	}
	e := s + rel + len(agentsEnd)
	if e < len(doc) && doc[e] == '\n' {
		e++
	}
	return s, e, nil
}

func upsertBlock(doc, block string) (string, string, error) {
	wrapped := agentsStart + "\n" + block + "\n" + agentsEnd + "\n"
	s, e, err := blockBounds(doc)
	if err != nil {
		return "", "", err
	}
	if s >= 0 {
		if doc[s:e] == wrapped {
			return doc, "", nil
		}
		return doc[:s] + wrapped + doc[e:], "update", nil
	}
	if doc != "" && !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	if doc != "" {
		doc += "\n"
	}
	return doc + wrapped, "add", nil
}

func removeBlock(doc string) (string, bool, error) {
	s, e, err := blockBounds(doc)
	if err != nil {
		return "", false, err
	}
	if s < 0 {
		return doc, false, nil
	}
	head := strings.TrimRight(doc[:s], "\n")
	if head != "" {
		head += "\n"
	}
	return head + doc[e:], true, nil
}
