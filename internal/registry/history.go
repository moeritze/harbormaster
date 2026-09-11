package registry

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const historyCap = 1000

// HistoryRecord is a pruned or released entry with the reason.
type HistoryRecord struct {
	Entry
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// AppendHistory appends under the lock. Used by release/kill paths.
func (s *Store) AppendHistory(rec HistoryRecord) error {
	unlock, err := lock(s.path(lockName), lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()
	return s.appendHistoryLocked(rec)
}

func (s *Store) appendHistoryLocked(rec HistoryRecord) error {
	lines, _, err := s.readHistoryLines()
	if err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	lines = append(lines, string(b))
	if len(lines) > historyCap {
		lines = lines[len(lines)-historyCap:]
	}
	return s.atomicWrite(historyName, "history-*.tmp", []byte(strings.Join(lines, "\n")+"\n"))
}

// maxHistoryLine caps how long one history line may be. A longer line cannot
// be a record this build wrote (a record is a few hundred bytes), so it is
// damage: junk from a crashed writer, or a file someone else appended to.
const maxHistoryLine = 1024 * 1024

// readHistoryLines returns the non-empty lines of history.jsonl and how many
// over-long lines it skipped. A bufio.Scanner used to fail the whole read on
// a line past its buffer limit, which broke every later write -- one damaged
// line made `release`, `kill` and pruning error out for good. Reading with
// bufio.Reader instead lets the damaged line be dropped (and dropped from
// the file on the next append) while every valid record survives.
func (s *Store) readHistoryLines() ([]string, int, error) {
	if err := checkNotSymlink(s.path(historyName)); err != nil {
		return nil, 0, err
	}
	fh, err := os.Open(s.path(historyName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = fh.Close() }()

	var lines []string
	skipped := 0
	r := bufio.NewReader(fh)
	for {
		line, readErr := r.ReadString('\n')
		if len(line) > maxHistoryLine {
			skipped++
		} else if t := strings.TrimSpace(line); t != "" {
			lines = append(lines, t)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return lines, skipped, nil
			}
			return nil, 0, readErr
		}
	}
}

// History returns the most recent limit records, oldest first. limit <= 0
// means all. It reads under the lock like every other path that touches the
// state files, so it cannot observe history.jsonl mid-rename or race the
// symlink check it makes before opening the file.
func (s *Store) History(limit int) ([]HistoryRecord, error) {
	unlock, err := lock(s.path(lockName), readLockTimeout)
	if err != nil {
		return nil, err
	}
	defer unlock()

	lines, _, err := s.readHistoryLines()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]HistoryRecord, 0, len(lines))
	for _, l := range lines {
		var r HistoryRecord
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			continue // skip corrupt line rather than fail the whole read
		}
		out = append(out, r)
	}
	return out, nil
}
