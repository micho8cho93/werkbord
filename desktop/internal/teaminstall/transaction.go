package teaminstall

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type installTransaction struct {
	Phase string            `json:"phase"`
	Files map[string][]byte `json:"files"`
}

// Installation transactions contain only the old service definition and local
// connection metadata. They never restore an old Team database over newer work.
// Helpers.previous and failed candidates are retained until explicit cleanup.
func beginInstall(root, plist string) error {
	if err := recoverInstall(root, plist); err != nil {
		return err
	}
	// Archive the last verified generation; never silently delete an old installation.
	if _, err := os.Stat(filepath.Join(root, "Helpers.previous")); err == nil {
		dir, err := os.MkdirTemp(root, "install-backup-")
		if err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(root, "Helpers.previous"), filepath.Join(dir, "Helpers")); err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(root, "install-transaction.json"), filepath.Join(dir, "transaction.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	x := installTransaction{Phase: "prepared", Files: map[string][]byte{}}
	for name, path := range map[string]string{"plist": plist, "access.key": filepath.Join(root, "access.key"), "owner": filepath.Join(root, "owner")} {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			x.Files[name] = nil
			continue
		}
		if err != nil {
			return err
		}
		x.Files[name] = b
	}
	return saveInstall(root, x)
}

func saveInstall(root string, x installTransaction) error {
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	return atomicFile(filepath.Join(root, "install-transaction.json"), b, 0600)
}

func readInstall(root string) (installTransaction, error) {
	var x installTransaction
	b, err := os.ReadFile(filepath.Join(root, "install-transaction.json"))
	if err != nil {
		return x, err
	}
	if len(b) > 1<<20 || json.Unmarshal(b, &x) != nil || len(x.Files) != 3 {
		return x, errors.New("Team installation recovery marker is unreadable; preserved for recovery")
	}
	switch x.Phase {
	case "prepared", "verified", "rolled_back":
	default:
		return x, errors.New("unknown Team installation phase")
	}
	return x, nil
}

func recoverInstall(root, plist string) error {
	x, err := readInstall(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if x.Phase != "prepared" {
		return nil
	}
	previous := filepath.Join(root, "Helpers.previous")
	if _, err = os.Stat(previous); err == nil {
		if _, err = os.Stat(filepath.Join(root, "Helpers")); err == nil {
			dir, err := os.MkdirTemp(root, "install-failed-")
			if err != nil {
				return err
			}
			if err = os.Rename(filepath.Join(root, "Helpers"), filepath.Join(dir, "Helpers")); err != nil {
				return err
			}
		}
		if err = os.Rename(previous, filepath.Join(root, "Helpers")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for name, path := range map[string]string{"plist": plist, "access.key": filepath.Join(root, "access.key"), "owner": filepath.Join(root, "owner")} {
		b, ok := x.Files[name]
		if !ok {
			return errors.New("Team recovery metadata missing")
		}
		if b == nil {
			if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		mode := os.FileMode(0600)
		if name == "plist" {
			mode = 0644
		}
		if err = atomicFile(path, b, mode); err != nil {
			return err
		}
	}
	x.Phase = "rolled_back"
	return saveInstall(root, x)
}

func finishInstall(root string) error {
	x, err := readInstall(root)
	if err != nil {
		return err
	}
	x.Phase = "verified"
	return saveInstall(root, x)
}

func atomicFile(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".team-install-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
