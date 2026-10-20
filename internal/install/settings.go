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
	raw  []byte // exact bytes read from disk, nil if the file did not exist; used for the backup
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
	s.raw = b
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

// hooks returns the document's "hooks" object, creating it when absent. A
// "hooks" key that holds something other than a JSON object is somebody's
// configuration, however odd: it is refused by name rather than silently
// replaced, so an install can never drop it on the floor.
func (s *settings) hooks(path string) (map[string]any, error) {
	v, ok := s.doc["hooks"]
	if !ok || v == nil {
		h := map[string]any{}
		s.doc["hooks"] = h
		return h, nil
	}
	h, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: \"hooks\" is not a JSON object; fix it or move it aside before installing", path)
	}
	return h, nil
}

// eventList returns the array registered under one hook event. Like hooks,
// a value of the wrong type is refused by name instead of replaced.
func eventList(path, event string, hooks map[string]any) ([]any, error) {
	v, ok := hooks[event]
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: \"hooks.%s\" is not a JSON array; fix it or move it aside before installing", path, event)
	}
	return list, nil
}

// refuseSymlink is the guard every path install reads or writes goes
// through. Writing through a symlink means writing to wherever it points —
// possibly outside the config dir entirely — and the atomic rename in write
// would replace the link with a regular file, silently detaching it.
func refuseSymlink(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; harbormaster will not write through it — replace it with a regular file or point HARBORMASTER at another config dir", path)
	}
	return nil
}

func (s *settings) write(path string) error {
	// SetEscapeHTML(false): Marshal would turn a user's "a && b" into "a \u0026\u0026 b".
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.doc); err != nil {
		return err
	}
	return writeAtomic(path, buf.Bytes(), s.perm)
}

// writeAtomic replaces path with b via a temp file in the same directory, so
// a crash mid-write leaves either the old file or the new one, never half.
func writeAtomic(path string, b []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
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
