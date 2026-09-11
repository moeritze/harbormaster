// Package registry stores which local port is owned by which process,
// worktree, and agent session. State is a single JSON file guarded by flock.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// SchemaVersion is the on-disk registry.json schema version this build understands.
	SchemaVersion = 1
	fileName      = "registry.json"
	lockName      = "registry.lock"
	historyName   = "history.jsonl"
	lockTimeout   = 5 * time.Second
)

// Entry is one registered server. Field names are the on-disk schema (spec §5).
type Entry struct {
	ID        string    `json:"id"`
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	Cmd       string    `json:"cmd"`
	Repo      string    `json:"repo,omitempty"`
	Worktree  string    `json:"worktree,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Agent     string    `json:"agent"`
	Session   string    `json:"session,omitempty"`
	Label     string    `json:"label,omitempty"`
	StartedAt time.Time `json:"started_at"`
	HostUser  string    `json:"host_user"`
}

// File is the on-disk document.
type File struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Prober answers liveness questions. The OS implementation lives in
// package liveness; tests inject fakes.
type Prober interface {
	PidAlive(pid int) bool
	PortListening(port int) bool
}

// Store is a handle to the registry directory.
type Store struct {
	dir    string
	prober Prober
	now    func() time.Time
}

// Open ensures dir exists with mode 0700 and returns a Store.
func Open(dir string, p Prober, now func() time.Time) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 0700 is required (not permissive) for a directory: rwx for owner only
		return nil, fmt.Errorf("chmod state dir: %w", err)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{dir: dir, prober: p, now: now}, nil
}

// Dir returns the state directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// Load reads, prunes, and (only if anything was pruned) writes back.
func (s *Store) Load() (*File, error) {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return nil, err
	}
	defer unlock()

	f, pruned, err := s.readPruned()
	if err != nil {
		return nil, err
	}
	if len(pruned) > 0 {
		if err := s.commit(f, pruned); err != nil {
			return nil, err
		}
	}
	cp := *f
	cp.Entries = append([]Entry(nil), f.Entries...)
	return &cp, nil
}

// Update runs fn on the pruned document under the lock and writes the result atomically.
func (s *Store) Update(fn func(f *File) error) error {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()

	f, pruned, err := s.readPruned()
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		return err
	}
	return s.commit(f, pruned)
}

// readPruned reads the document and prunes it in memory. It is side-effect
// free: nothing is written to registry.json or history.jsonl. Must be
// called with the lock held. The caller is responsible for calling commit
// to persist both the pruned document and the pruned records.
func (s *Store) readPruned() (*File, []HistoryRecord, error) {
	f, err := s.read()
	if err != nil {
		return nil, nil, err
	}
	pruned := s.prune(f)
	return f, pruned, nil
}

// commit persists f and then, only once that succeeds, appends each pruned
// record to history. The registry write must happen first: if the history
// append then fails, a retry re-reads a registry that already reflects the
// pruned entries, so it finds nothing left to prune and cannot duplicate
// history records. Must be called with the lock held.
func (s *Store) commit(f *File, pruned []HistoryRecord) error {
	if err := s.write(f); err != nil {
		return err
	}
	for _, rec := range pruned {
		if err := s.appendHistoryLocked(rec); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) read() (*File, error) {
	b, err := os.ReadFile(s.path(fileName))
	if errors.Is(err, os.ErrNotExist) {
		return &File{Version: SchemaVersion, Entries: []Entry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	if f.Version != SchemaVersion {
		return nil, fmt.Errorf("registry schema version %d not supported (want %d)", f.Version, SchemaVersion)
	}
	if f.Entries == nil {
		f.Entries = []Entry{}
	}
	return &f, nil
}

func (s *Store) write(f *File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(fileName + ".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	if err := os.Rename(tmp, s.path(fileName)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit registry: %w", err)
	}
	return nil
}

// prune is replaced with real logic in prune.go (Task 5). Returns pruned records.
func (s *Store) prune(f *File) []HistoryRecord {
	return pruneEntries(f, s.prober, s.now())
}
