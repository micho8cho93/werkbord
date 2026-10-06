package localidentity

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"devboard/internal/deviceid"
)

// FileName is the identity's file in a FileStore's directory.
const FileName = "device-identity.json"

// ErrInsecure: the stored key is readable by someone other than its owner.
var ErrInsecure = errors.New("deviceid: the identity file is readable by other users")

// FileStore keeps the identity in one file in a directory only this user can enter.
//
// This is the interim storage: restrictive permissions (directory 0700, file
// 0600, written atomically), checked again every time the identity is read, with
// no operating-system keychain behind it yet. It is isolated behind Store so a
// keychain implementation replaces it without touching a caller. It protects the
// key from other users of the computer, not from malware running as this user,
// which is what a keychain's per-application access adds.
type FileStore struct {
	// Dir is the directory the file lives in; it is created 0700.
	Dir string
}

type fileFormat struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	PublicKey  string    `json:"publicKey"`
	PrivateKey string    `json:"privateKey"` // the Ed25519 seed, base64
}

func (f FileStore) path() string { return filepath.Join(f.Dir, FileName) }

// Load implements Store.
func (f FileStore) Load() (*Identity, error) {
	p := f.path()
	st, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s is mode %o; make it 0600", ErrInsecure, p, st.Mode().Perm())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var ff fileFormat
	if err := json.Unmarshal(b, &ff); err != nil {
		return nil, fmt.Errorf("deviceid: %s is damaged: %w", p, err)
	}
	if ff.Version != 1 {
		return nil, fmt.Errorf("deviceid: %s is version %d, which this Werkbord does not know", p, ff.Version)
	}
	seed, err := base64.RawURLEncoding.DecodeString(ff.PrivateKey)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("deviceid: %s has a damaged key", p)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if got := deviceid.EncodePublicKey(deviceid.PublicKey(priv.Public().(ed25519.PublicKey))); got != ff.PublicKey {
		return nil, fmt.Errorf("deviceid: %s: the private key does not match its public key", p)
	}
	if !deviceid.ValidID(ff.ID) {
		return nil, fmt.Errorf("deviceid: %s has an invalid device ID", p)
	}
	name, err := deviceid.CleanName(ff.Name)
	if err != nil {
		return nil, fmt.Errorf("deviceid: %s: %w", p, err)
	}
	return &Identity{id: deviceid.ID(ff.ID), name: name, created: ff.CreatedAt.UTC(), priv: priv}, nil
}

// Save implements Store. The file is written to a private temporary file in the
// same directory and renamed into place, so a crash leaves the old identity or the new one.
func (f FileStore) Save(i *Identity) error {
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(f.Dir, 0o700); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(fileFormat{
		Version: 1, ID: string(i.id), Name: i.name, CreatedAt: i.created,
		PublicKey:  deviceid.EncodePublicKey(i.PublicKey()),
		PrivateKey: base64.RawURLEncoding.EncodeToString(i.priv.Seed()),
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.Dir, ".device-identity-*") // created 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path())
}
