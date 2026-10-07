// Package platform owns native, locally initiated installation. It is never linked into a Workspace Host.
package platform

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

// Probe contacts only the local device service, without proxies or redirects.
func Probe(ctx context.Context, key, expectedVersion string) error {
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
