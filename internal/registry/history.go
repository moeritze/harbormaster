package registry

import (
	"bufio"
	"encoding/json"
	"errors"
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
	lines, err := s.readHistoryLines()
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
	tmp := s.path(historyName + ".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(historyName))
}

func (s *Store) readHistoryLines() ([]string, error) {
	fh, err := os.Open(s.path(historyName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	var lines []string
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			lines = append(lines, t)
		}
	}
	return lines, sc.Err()
}

// History returns the most recent limit records, oldest first. limit <= 0 means all.
func (s *Store) History(limit int) ([]HistoryRecord, error) {
	lines, err := s.readHistoryLines()
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
