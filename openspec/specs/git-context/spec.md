# git-context Specification

## Purpose
Identify the repository, worktree and branch a command runs in, so ports and ownership are keyed to the right worktree, and display them readably.

## Requirements

### Requirement: Repo is correct from any subdirectory
Discovery SHALL return the same repo and worktree from a subdirectory as from the worktree root, for both main checkouts and linked worktrees.

#### Scenario: Subdirectory of a main checkout
- **WHEN** discovery runs in `<repo>/internal`
- **THEN** Repo is `<repo>` and Worktree is `<repo>`

### Requirement: Shortened paths keep the base name
Displayed worktree paths SHALL be shortened to `parent/base` before being length-capped, so the base name is always visible.

#### Scenario: Very long worktree path
- **WHEN** a worktree path is longer than the display cap
- **THEN** the owner line and history show `parent/base`, not a truncated prefix
