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
	// Spawned marks entries harbormaster started itself with `run`, which
	// puts the child in its own process group. Only those may be signaled
	// as a group; a claimed pid is signaled individually.
	Spawned bool `json:"spawned,omitempty"`
	// StartTime is the process start time as reported by the OS when the
	// entry was written. Before signalling, the live value is compared to
	// it: a pid that has been reused since belongs to some other process.
	StartTime string `json:"start_time,omitempty"`
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

// Open creates dir with mode 0700 if it does not exist yet, validates it,
// and returns a Store. An existing directory is left exactly as the user set
// it up: every command would otherwise silently re-chmod a directory the
// user may have shared on purpose (a group-readable state dir). It is
// validated rather than repaired, so a directory harbormaster cannot vouch
// for is refused instead of quietly used.
func Open(dir string, p Prober, now func() time.Time) (*Store, error) {
	if _, err := os.Stat(dir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("state dir: %w", err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
	}
	if err := validateStateDir(dir); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{dir: dir, prober: p, now: now}, nil
}

// validateStateDir refuses a state directory harbormaster cannot trust: a
// symlink (whoever controls the link controls where state lands), one that
// group or other can write (they could swap registry.json for a symlink or
// plant files), or one owned by a different user.
func validateStateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state dir %s is a symlink; refusing to operate", dir)
	}
	if mode := fi.Mode().Perm(); mode&0o022 != 0 {
		return fmt.Errorf("state dir %s is group/other-writable (%o); fix with chmod 700", dir, mode)
	}
	return checkOwner(dir, fi)
}

// checkNotSymlink refuses to read or write a state file that has been
// replaced by a symlink. os.ReadFile and os.WriteFile both follow one, so a
// planted link would redirect harbormaster's reads and writes to a file of
// the planter's choosing.
func checkNotSymlink(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("registry file %s is a symlink; refusing to operate", path)
	}
	return nil
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

// Peek returns the registry as stored, without pruning or probing. Hooks
// use it because they must answer in milliseconds; entries may be stale.
func (s *Store) Peek() (*File, error) {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return nil, err
	}
	defer unlock()
	f, err := s.read()
	if err != nil {
		return nil, err
	}
	cp := *f
	cp.Entries = append([]Entry(nil), f.Entries...)
	return &cp, nil
}

// Prune removes dead entries now and returns what was pruned.
func (s *Store) Prune() ([]HistoryRecord, error) {
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
	return pruned, nil
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

// Remove drops the entry with the given id. ok is false when it was not
// present. The target is taken out BEFORE the rest is pruned: the caller
// (kill, release, the run wrapper, a session-end hook) has already dealt
// with that process and wants to record its own reason; if pruning ran
// first it would claim the same entry as pid_dead and the caller's record
// would never be written.
func (s *Store) Remove(id string) (Entry, bool, error) {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return Entry{}, false, err
	}
	defer unlock()

	f, err := s.read()
	if err != nil {
		return Entry{}, false, err
	}
	var removed Entry
	found := false
	kept := f.Entries[:0]
	for _, e := range f.Entries {
		if e.ID == id {
			removed = e
			found = true
			continue
		}
		kept = append(kept, e)
	}
	f.Entries = kept
	pruned := s.prune(f)
	if err := s.commit(f, pruned); err != nil {
		return Entry{}, false, err
	}
	return removed, found, nil
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
	if err := checkNotSymlink(s.path(fileName)); err != nil {
		return nil, err
	}
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
	return s.atomicWrite(fileName, "registry-*.tmp", append(b, '\n'))
}

// atomicWrite writes data to a fresh temp file in s.dir and renames it over
// name. os.CreateTemp picks a random name and opens it with O_EXCL, so --
// unlike the fixed "<name>.tmp" path this replaced -- there is no predictable
// path an attacker can pre-plant a symlink at for the write to follow. The
// temp file is removed on every failure path so a broken write leaves no
// litter behind.
func (s *Store) atomicWrite(name, pattern string, data []byte) error {
	if err := checkNotSymlink(s.path(name)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, pattern)
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	tmpName := tmp.Name()
	fail := func(what string, err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("%s %s: %w", what, name, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail("write", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("write", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("write", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := os.Rename(tmpName, s.path(name)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}

// prune is replaced with real logic in prune.go (Task 5). Returns pruned records.
func (s *Store) prune(f *File) []HistoryRecord {
	return pruneEntries(f, s.prober, s.now())
}
