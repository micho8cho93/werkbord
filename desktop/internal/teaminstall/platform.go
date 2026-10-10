// Package teaminstall is Werkbord's own installer for the Team service on a Mac: native, locally initiated, and never linked
// into a Workspace Host. The app's executable is the installer. Run with --activate or --service it does one fixed thing and
// prints one line of JSON; macOS asks for an administrator's authorization, and the privileged step runs this same executable
// as root with --team-service (it does so before any window exists). It takes no argument that names a path or a program.
package teaminstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// nativeVersion is the version the service must report once it is installed: the app's own, set by Run.
var nativeVersion = "dev"

const ServiceLabel = "dev.werkbord.team"
const BaseURL = "http://127.0.0.1:7431"
const SystemDir = "/Library/Application Support/Werkbord Team"
const ServicePlist = "/Library/LaunchDaemons/dev.werkbord.team.plist"

// AccessKey is the window's own credential, shared with the native service at installation.
func AccessKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "Library", "Application Support", "werkbord-team-desktop", "access.key")
	if b, err := os.ReadFile(path); err == nil {
		key := string(b)
		if _, err := hex.DecodeString(key); err != nil || len(key) != 64 {
			return "", errors.New("the desktop access key is unreadable; preserve it and contact support")
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw[:])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(key)
	cerr := f.Close()
	if err != nil {
		return "", err
	}
	return key, cerr
}

// ServiceIsolated reports whether the installed service was installed without a path to the person's own Werkbord, which is
// what keeps it from ever reading that credential. A service from before this existed carries one in its definition.
// The definition is world-readable by design; an absent one means no service is installed.
func ServiceIsolated() (installed, isolated bool, err error) {
	b, err := os.ReadFile(ServicePlist)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, !strings.Contains(string(b), "--runner-config"), nil
}

// Probe contacts only the local device service, without proxies or redirects.
func Probe(ctx context.Context, key, expectedVersion string) error {
	return probe(ctx, key, expectedVersion, false)
}

func probe(ctx context.Context, key, expectedVersion string, installing bool) error {
	cl := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", BaseURL+"/api/device/v1/state", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if !installing && res.Header.Get("X-Werkbord-Installation-Pending") != "" {
		return errors.New("Team installation was interrupted; activate Team again to recover it")
	}
	if res.StatusCode != 200 {
		return errors.New("a different device service is already using this computer; contact its owner")
	}
	return serviceVersion(res.Body, expectedVersion)
}

// A running older service must be updated before loading its UI. A 200 response
// alone would let the new window keep using outdated daemon code indefinitely.
func serviceVersion(body io.Reader, expected string) error {
	var state struct {
		Daemon  bool   `json:"daemon"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&state); err != nil || !state.Daemon || state.Version == "" {
		return errors.New("the Team service could not be verified; choose Try again")
	}
	if expected != "dev" && strings.TrimPrefix(state.Version, "v") != strings.TrimPrefix(expected, "v") {
		return errors.New("the Team background service needs to be updated")
	}
	return nil
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", from)
	}
	f, err := os.CreateTemp(filepath.Dir(to), ".install-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = io.Copy(f, in)
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
	return os.Rename(f.Name(), to)
}
