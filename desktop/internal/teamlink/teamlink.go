// Package teamlink is the one place the Werkbord desktop app knows how to reach Werkbord Team's service on this computer. It
// is a client of that service's loopback API and a launcher of Team's own signed installer, and nothing more: it holds no
// Team code, links none, and gives the service nothing but what the person asks the app to give it.
//
// Team's service is a background program with its own privileges (it creates a network interface). The app never installs
// or runs it itself. When the person asks to use Team, the app shows them what will happen and, if they agree, asks Team's own
// installer (a signed program that ships with Team, found in the places Team puts it) to do it; macOS then asks for an
// administrator's authorization, as it does for Team's own app.
package teamlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"devboard/desktop/internal/workspaces"
)

// Where Team's service listens and where the app's own credential for it is kept (the credential Team's own window has always used).
const (
	DefaultBase = "http://127.0.0.1:7431"
	ListPath    = "/api/device/v1/listing"
	// ServiceDefinition is where macOS keeps the definition of Team's background service when it is installed.
	ServiceDefinition = "/Library/LaunchDaemons/dev.werkbord.team.plist"
)

var slotPattern = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

// KeyFile is where Team's installer put this person's credential for the service.
func KeyFile(home string) string {
	return filepath.Join(home, "Library", "Application Support", "werkbord-team-desktop", "access.key")
}

// ReadKey reads the credential. It never creates one: no file means the person never set Team up on this computer.
func ReadKey(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return "", errors.New("the Team credential is not a private file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	k := strings.TrimSpace(string(b))
	if len(k) != 64 || strings.Trim(k, "0123456789abcdef") != "" {
		return "", errors.New("the Team credential is unreadable")
	}
	return k, nil
}

// Link is the app's connection to Team's service.
type Link struct {
	Source *workspaces.Service
	base   string
	key    func() (string, error)
	hc     *http.Client
	// Installed says whether Team's background service is installed (its definition exists); it tells "not set up" from
	// "installed but stopped".
	Installed func() bool
}

// New makes the link. base is the service's loopback address (DefaultBase), keyPath the credential file.
func New(base, keyPath string, installed func() bool) (*Link, error) {
	key := func() (string, error) { return ReadKey(keyPath) }
	src, err := workspaces.NewService("Team", base, ListPath, key)
	if err != nil {
		return nil, err
	}
	if installed == nil {
		installed = func() bool { _, err := os.Stat(ServiceDefinition); return err == nil }
	}
	return &Link{Source: src, base: src.Base(), key: key, Installed: installed, hc: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Status is the state of Team's service for the person.
type Status struct {
	// State is one of: not_installed (never set up here), stopped (installed, not answering), ready, outdated (answers, too old
	// for this app), refused (answers, does not accept this app).
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// StatusOf turns what listing the service returned into what the person is told.
func (l *Link) StatusOf(err error) Status {
	switch {
	case err == nil:
		return Status{State: "ready"}
	case errors.Is(err, workspaces.ErrOutdated):
		return Status{State: "outdated", Detail: "Team's service is older than this app. Update it to use several Team workspaces."}
	case errors.Is(err, workspaces.ErrRefused):
		return Status{State: "refused", Detail: "Team's service did not accept this app. It may be installed for another user of this Mac."}
	case errors.Is(err, workspaces.ErrNoCredential), errors.Is(err, workspaces.ErrNotRunning):
		if l.Installed() {
			return Status{State: "stopped", Detail: "Team's service is installed but not running."}
		}
		return Status{State: "not_installed", Detail: "Team is not set up on this computer."}
	}
	return Status{State: "stopped", Detail: err.Error()}
}

func (l *Link) call(ctx context.Context, method, path string, in, out any) error {
	key, err := l.key()
	if err != nil {
		return workspaces.ErrNoCredential
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, l.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := l.hc.Do(req)
	if err != nil {
		return workspaces.ErrNotRunning
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return errors.New(e.Error.Message)
		}
		return fmt.Errorf("Team's service answered %d", res.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// AddWorkspace asks the service for a place to create or join a Team workspace and returns its identity (team:<slot>).
func (l *Link) AddWorkspace(ctx context.Context) (string, error) {
	var out struct {
		Slot string `json:"slot"`
	}
	if err := l.call(ctx, http.MethodPost, "/api/device/v1/workspaces", map[string]string{}, &out); err != nil {
		return "", err
	}
	if !slotPattern.MatchString(out.Slot) {
		return "", errors.New("Team's service answered with something that is not a workspace")
	}
	return "team:" + out.Slot, nil
}

// SlotOf is the slot a workspace identity names.
func SlotOf(id string) (string, bool) {
	slot, ok := strings.CutPrefix(id, "team:")
	return slot, ok && slotPattern.MatchString(slot)
}

// ForgetWorkspace asks the service to drop a workspace slot that is empty, or that the person left. The service refuses
// one that is still joined.
func (l *Link) ForgetWorkspace(ctx context.Context, id string) error {
	slot, ok := SlotOf(id)
	if !ok {
		return errors.New("not a Team workspace")
	}
	return l.call(ctx, http.MethodDelete, "/api/device/v1/workspaces/"+url.PathEscape(slot), nil, nil)
}

// DeliverGrant hands one Team workspace's service a narrow execution grant for the person's Werkbord. It is the only thing of
// Werkbord's the service ever receives.
func (l *Link) DeliverGrant(ctx context.Context, id, token, base string) error {
	slot, ok := SlotOf(id)
	if !ok {
		return errors.New("not a Team workspace")
	}
	return l.call(ctx, http.MethodPost, "/w/"+slot+"/api/device/v1/runner/grant", map[string]string{"token": token, "base": base}, nil)
}
