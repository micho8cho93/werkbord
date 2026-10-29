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
	"runtime"
	"strconv"
	"strings"
	"time"

	"werkbord/desktop/internal/workspaces"
)

// Where Team's service listens and where the app's own credential for it is kept (the credential Team's own window has always used).
const (
	DefaultBase = "http://127.0.0.1:7431"
	ListPath    = "/api/device/v1/listing"
)

var slotPattern = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

// KeyFile is where Team's installer put this person's credential for the service.
func KeyFile(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "werkbord-team-desktop", "access.key")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "werkbord-team-desktop", "access.key")
	default:
		return filepath.Join(home, ".config", "werkbord-team-desktop", "access.key")
	}
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

// ResponseError keeps the Team API status attached to its user-facing message so the assistant can distinguish a
// definite permission or validation refusal from an unknown transport outcome.
type ResponseError struct {
	StatusCode int
	Message    string
}

func (e *ResponseError) Error() string { return e.Message }

// New makes the link. base is the service's loopback address (DefaultBase), keyPath the credential file.
func New(base, keyPath string, installed func() bool) (*Link, error) {
	key := func() (string, error) { return ReadKey(keyPath) }
	src, err := workspaces.NewService("Team", base, ListPath, key)
	if err != nil {
		return nil, err
	}
	if installed == nil {
		installed = serviceInstalled
	}
	return &Link{Source: src, base: src.Base(), key: key, Installed: installed, hc: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// RequireVersion prevents a newly updated app from silently serving an older Team interface.
// Development builds can talk to development services without a release version fence.
func (l *Link) RequireVersion(version string) {
	if version == "" || version == "dev" {
		return
	}
	l.Source.Check = func(ctx context.Context) error {
		var state struct {
			Version string `json:"version"`
		}
		if err := l.call(ctx, http.MethodGet, "/api/device/v1/state", nil, &state); err != nil {
			return err
		}
		if strings.TrimPrefix(state.Version, "v") != strings.TrimPrefix(version, "v") {
			return workspaces.ErrOutdated
		}
		return nil
	}
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
		return Status{State: "outdated", Detail: "Team's background service does not match this app. Update it in Devices & Hosting to load the current Team interface."}
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
	limit := int64(1 << 20)
	if strings.Contains(path, "/product-map") {
		limit = 16 << 20
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if int64(len(raw)) > limit {
		return errors.New("workspace response exceeds its size limit")
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return &ResponseError{StatusCode: res.StatusCode, Message: e.Error.Message}
		}
		return &ResponseError{StatusCode: res.StatusCode, Message: fmt.Sprintf("Team's service answered %d", res.StatusCode)}
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

// Invitation is what an invitation link says before it is used: which team it is for, and until when.
type Invitation struct {
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// InspectInvitation has the service read an invitation link, offline, and say which team it is for. It changes nothing.
func (l *Link) InspectInvitation(ctx context.Context, link string) (Invitation, error) {
	var out Invitation
	err := l.call(ctx, http.MethodPost, "/api/device/v1/invitation", map[string]string{"link": link}, &out)
	return out, err
}

// SetupState is how far one workspace slot has got in being created or joined.
type SetupState struct {
	// Operation is what the service is doing now ("" when nothing); Error is why the last attempt failed.
	Operation string `json:"operation"`
	Error     string `json:"error"`
	// Enrolled: the slot holds a workspace. Pending: a join waits for an administrator to approve this computer.
	Enrolled  bool   `json:"enrolled"`
	Pending   bool   `json:"pending"`
	Workspace string `json:"workspace"`
}

// SetupOf reads how far a slot has got.
func (l *Link) SetupOf(ctx context.Context, id string) (SetupState, error) {
	slot, ok := SlotOf(id)
	if !ok {
		return SetupState{}, errors.New("not a Team workspace")
	}
	var raw struct {
		Operation string `json:"operation"`
		Error     string `json:"error"`
		Enrolled  bool   `json:"enrolled"`
		Pending   bool   `json:"pending"`
		Workspace struct {
			Name string `json:"name"`
		} `json:"workspace"`
	}
	if err := l.call(ctx, http.MethodGet, "/w/"+slot+"/api/device/v1/state", nil, &raw); err != nil {
		return SetupState{}, err
	}
	return SetupState{Operation: raw.Operation, Error: raw.Error, Enrolled: raw.Enrolled, Pending: raw.Pending, Workspace: raw.Workspace.Name}, nil
}

// CreateWorkspace has the service make a new team on this computer in the slot. It answers once the work has started; SetupOf
// says when it is done.
func (l *Link) CreateWorkspace(ctx context.Context, id, name, owner string) error {
	slot, ok := SlotOf(id)
	if !ok {
		return errors.New("not a Team workspace")
	}
	return l.call(ctx, http.MethodPost, "/w/"+slot+"/api/device/v1/create", map[string]string{"name": name, "owner": owner}, nil)
}

// JoinWorkspace has the service enrol this computer in the team an invitation is for. premium is the person's unlock key, or "".
func (l *Link) JoinWorkspace(ctx context.Context, id, link, name, device, premium string) error {
	slot, ok := SlotOf(id)
	if !ok {
		return errors.New("not a Team workspace")
	}
	return l.call(ctx, http.MethodPost, "/w/"+slot+"/api/device/v1/join", map[string]string{"link": link, "name": name, "deviceName": device, "premium": premium}, nil)
}

// CancelJoin withdraws an enrolment that waits for an administrator.
func (l *Link) CancelJoin(ctx context.Context, id string) error {
	slot, ok := SlotOf(id)
	if !ok {
		return errors.New("not a Team workspace")
	}
	return l.call(ctx, http.MethodPost, "/w/"+slot+"/api/device/v1/join/cancel", map[string]string{}, nil)
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

// Analytics reads a team's coordination analytics through this person's own credential: what the team's own page shows them, and no
// more (the service decides which projects they may see). project may be "" for every project they can see.
func (l *Link) Analytics(ctx context.Context, id, project string, days int) (json.RawMessage, error) {
	slot, ok := SlotOf(id)
	if !ok {
		return nil, errors.New("not a Team workspace")
	}
	q := url.Values{"days": {strconv.Itoa(days)}}
	if project != "" {
		q.Set("project", project)
	}
	var out json.RawMessage
	if err := l.call(ctx, http.MethodGet, "/w/"+slot+"/api/team/v1/analytics?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeliverPremium gives one Team workspace's service the person's Premium unlock key. The service checks it, keeps it for this
// person on this computer, and gives it to its team, which checks it again and keeps only its ID. It is what lets a team of more
// than two people include them.
func (l *Link) DeliverPremium(ctx context.Context, id, key string) error {
	slot, ok := SlotOf(id)
	if !ok {
		return errors.New("not a Team workspace")
	}
	return l.call(ctx, http.MethodPost, "/w/"+slot+"/api/device/v1/premium", map[string]string{"key": key}, nil)
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
