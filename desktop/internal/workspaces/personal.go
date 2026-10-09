package workspaces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"devboard/internal/workspace"
)

// Access is where the person's own Werkbord is and the credential this computer uses for it: what the launcher found.
type Access struct {
	// Base is the controller's loopback address.
	Base string
	// Token is the controller's own credential. It never leaves this package except in a request to Base, and in the fragment
	// of the URL that loads the controller's own page.
	Token string
}

// Personal is the person's own Werkbord on this computer: the one workspace that is always there.
type Personal struct {
	// Access finds the controller as it is now (the app may have started it a moment ago, or the token may have been rotated).
	Access func(ctx context.Context) (Access, error)
	hc     *http.Client
}

// NewPersonal makes the Personal source.
func NewPersonal(access func(ctx context.Context) (Access, error)) *Personal {
	return &Personal{Access: access, hc: loopbackClient(4 * time.Second)}
}

// Name implements Source.
func (p *Personal) Name() string { return "Werkbord" }

func (p *Personal) entry(state workspace.State, detail string) workspace.Listed {
	return workspace.Listed{
		Entry: workspace.Entry{ID: workspace.PersonalID, Kind: workspace.KindPersonal, Name: "Personal", State: state, Detail: detail, DeviceRoles: []string{"runner"}},
		Root:  "/", SummaryPath: "/api/workspace/v1/summary",
	}
}

// List implements Source. Personal is always listed: if the controller is not answering, it is listed as unavailable and
// the app says what it can about why, because the person's own work is not something to quietly drop from the screen.
func (p *Personal) List(ctx context.Context) ([]workspace.Listed, error) {
	a, err := p.Access(ctx)
	if err != nil {
		return []workspace.Listed{p.entry(workspace.StateUnavailable, "Werkbord is not running on this computer.")}, nil
	}
	if _, err := get2(ctx, p.hc, a.Base, "/api/health", ""); err != nil {
		return []workspace.Listed{p.entry(workspace.StateUnavailable, "Werkbord is not answering.")}, nil
	}
	return []workspace.Listed{p.entry(workspace.StateReady, "")}, nil
}

func get2(ctx context.Context, hc *http.Client, base, path, key string) ([]byte, error) {
	body, err := get(ctx, hc, base, path, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, workspace.MaxBytes+1))
}

// Frame implements Source: the controller's own web app, signed in as the app has always signed it in.
func (p *Personal) Frame(ctx context.Context, l workspace.Listed, _ string) (Frame, error) {
	a, err := p.Access(ctx)
	if err != nil {
		return Frame{}, err
	}
	base, err := CheckBase(a.Base)
	if err != nil {
		return Frame{}, err
	}
	u := base + "/"
	if a.Token != "" {
		u += "#token=" + url.QueryEscape(a.Token)
	}
	return Frame{URL: u, Origin: base}, nil
}

// Summary implements Source.
func (p *Personal) Summary(ctx context.Context, l workspace.Listed) (workspace.Summary, error) {
	a, err := p.Access(ctx)
	if err != nil {
		return workspace.Summary{}, err
	}
	base, err := CheckBase(a.Base)
	if err != nil {
		return workspace.Summary{}, err
	}
	body, err := get(ctx, p.hc, base, l.SummaryPath, a.Token)
	if err != nil {
		return workspace.Summary{}, err
	}
	defer body.Close()
	s, _, err := workspace.Decode(body, l.Entry.ID)
	return s, err
}

// ExecutionScope is the local-access scope of the per-workspace execution grant: exactly what the Team service uses to ask
// this Werkbord to open, preview and start a task the person approves. It is narrower than the controller's own credential.
const ExecutionScope = "execution-local-v1"

// MintExecutionGrant makes a narrow, revocable local-access grant named name, using the controller's own credential, which
// this program holds because it is the person's own app. Only the grant is returned, to be handed to one Team workspace's
// service; it can be seen and revoked in this Werkbord's settings. Earlier grants with the same name are revoked once the new
// one exists, so reconnecting does not leave old ones behind.
func (p *Personal) MintExecutionGrant(ctx context.Context, name string) (token, base string, err error) {
	a, err := p.Access(ctx)
	if err != nil {
		return "", "", err
	}
	base, err = CheckBase(a.Base)
	if err != nil {
		return "", "", err
	}
	if a.Token == "" {
		return "", "", errors.New("Werkbord's credential is not available")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return "", "", errors.New("a grant needs a short name")
	}
	var before []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if b, err := get2(ctx, p.hc, base, "/api/local-access", a.Token); err == nil {
		_ = json.Unmarshal(b, &before)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := p.send(ctx, base, a.Token, http.MethodPost, "/api/local-access", map[string]string{"name": name, "scope": ExecutionScope}, &out); err != nil {
		return "", "", fmt.Errorf("Werkbord would not make the grant: %w", err)
	}
	if !strings.HasPrefix(out.Token, "wba_") {
		return "", "", errors.New("Werkbord answered with something that is not a grant")
	}
	return out.Token, base, p.revokeOlder(ctx, base, a.Token, name, out.Token, before)
}

// revokeOlder revokes grants that already had name before the new one was made. Failing to is reported, not hidden: an
// old grant is still a grant.
func (p *Personal) revokeOlder(ctx context.Context, base, key, name, keep string, before []struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}) error {
	var failed []string
	for _, e := range before {
		if e.Name != name {
			continue
		}
		if err := p.send(ctx, base, key, http.MethodDelete, "/api/local-access/"+url.PathEscape(e.ID), nil, nil); err != nil {
			failed = append(failed, e.ID)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("the new grant works, but %d earlier one(s) with the same name could not be revoked; revoke them in Werkbord's settings", len(failed))
	}
	return nil
}

func (p *Personal) send(ctx context.Context, base, key, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.hc.Do(req)
	if err != nil {
		return err
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
		return fmt.Errorf("answered %d", res.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
