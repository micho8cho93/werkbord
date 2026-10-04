package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AtomicJSON durably replaces owner-only runner state. The containing directory
// is private, and the old state survives a crash before rename.
func AtomicJSON(path string, value any) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	data, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".runner-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	if d, e := os.Open(filepath.Dir(path)); e == nil {
		defer d.Close()
		_ = d.Sync()
	}
	return nil
}
