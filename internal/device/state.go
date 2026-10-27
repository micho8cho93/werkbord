// Package device holds local capability selection and the loopback client
// gateway. It owns no database, scheduler, execution engine or network provider.
package device

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"werkbord/internal/clientaccess"
)

type State struct {
	Mode           string `json:"mode"` // local, switching, remote, restoring
	Controller     string `json:"controller,omitempty"`
	Runner         bool   `json:"runner"`
	RestoreService bool   `json:"restoreService,omitempty"`
	RestoreBinary  string `json:"restoreBinary,omitempty"`
	RestorePATH    string `json:"restorePath,omitempty"`
}

func (s State) Remote() bool { return s.Mode != "" && s.Mode != "local" }
func Path(dir string) string { return filepath.Join(dir, "device", "state.json") }
func Load(dir string) (State, error) {
	var s State
	raw, err := os.ReadFile(Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	if s.Mode != "local" && s.Mode != "switching" && s.Mode != "remote" && s.Mode != "restoring" {
		return s, errors.New("unknown device mode; inspect device/state.json")
	}
	if s.Mode != "local" && s.Controller == "" {
		return s, errors.New("remote controller is missing")
	}
	if s.Controller != "" {
		if err = ValidateController(s.Controller); err != nil {
			return s, err
		}
	}
	return s, nil
}
func Save(dir string, s State) error { return clientaccess.WriteJSON(Path(dir), s) }
func ValidateController(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("invalid fixed controller address")
	}
	host := u.Hostname()
	if host == "localhost" {
		return errors.New("controller must be a private network address")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		return errors.New("controller must be a private network address")
	}
	return nil
}

type Credential struct {
	Controller string `json:"controller"`
	ID         string `json:"id"`
	DeviceID   string `json:"deviceId"`
	Token      string `json:"token"`
}

func CredentialPath(dir string) string { return filepath.Join(dir, "device", "client.json") }
func LoadCredential(dir string) (Credential, error) {
	var c Credential
	raw, err := os.ReadFile(CredentialPath(dir))
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if !strings.HasPrefix(c.Token, clientaccess.TokenPrefix) || len(c.Token) != len(clientaccess.TokenPrefix)+64 || c.ID == "" || c.DeviceID == "" {
		return c, errors.New("invalid desktop credential")
	}
	if err = ValidateController(c.Controller); err != nil {
		return c, err
	}
	return c, nil
}
func DeviceID(dir string) (string, error) {
	path := filepath.Join(dir, "device", "id")
	raw, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(raw))
		if len(id) != 64 {
			return "", errors.New("invalid local device identifier")
		}
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	bytes := make([]byte, 32)
	if _, err = rand.Read(bytes); err != nil {
		return "", err
	}
	id := hex.EncodeToString(bytes)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create device identity: %w", err)
	}
	defer f.Close()
	if _, err = f.WriteString(id); err != nil {
		return "", err
	}
	return id, f.Sync()
}
