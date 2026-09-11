package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// settings is a JSON document we edit surgically, keeping unknown keys.
type settings struct {
	doc  map[string]any
	perm os.FileMode
}

func dirOf(path string) string {
	return filepath.Dir(path)
}

func readSettings(path string) (*settings, error) {
	s := &settings{doc: map[string]any{}, perm: 0o600}
	b, err := os.ReadFile(path) //nolint:gosec // path is derived from the caller's configured ConfigDir
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil {
		s.perm = st.Mode().Perm()
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&s.doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v); fix it or move it aside before installing", path, err)
	}
	return s, nil
}

func (s *settings) hooks() map[string]any {
	h, _ := s.doc["hooks"].(map[string]any)
	if h == nil {
		h = map[string]any{}
		s.doc["hooks"] = h
	}
	return h
}

func (s *settings) write(path string) error {
	b, err := json.MarshalIndent(s.doc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dirOf(path), ".settings-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(s.perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
