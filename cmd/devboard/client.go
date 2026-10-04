package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"syscall"
	"time"

	"devboard/internal/config"
	"devboard/internal/doctor"
	"devboard/internal/service"
)

// client is a minimal HTTP client for the local controller API.
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(cfg config.Config) (*client, error) {
	tok, err := cfg.ResolveToken(false)
	if err != nil {
		return nil, err
	}
	return &client{base: "http://" + cfg.ClientAddr(), token: tok, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return fmt.Errorf("controller is not running at %s; start it with: devboard serve", c.base)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct{ Code, Message string }
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error.Message != "" {
			return errors.New(e.Error.Message)
		}
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *client) registerProject(ctx context.Context, path, name string) (*service.ProjectDetail, error) {
	// Resolve relative paths against the CLI's working directory, not the
	// controller's.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var p service.ProjectDetail
	err = c.do(ctx, http.MethodPost, "/api/projects", map[string]string{"path": abs, "name": name}, &p)
	return &p, err
}

func (c *client) listProjects(ctx context.Context) ([]service.ProjectDetail, error) {
	var resp struct {
		Projects []service.ProjectDetail `json:"projects"`
	}
	err := c.do(ctx, http.MethodGet, "/api/projects", nil, &resp)
	return resp.Projects, err
}

// ---- calls the service commands make ----

// health asks whether a controller is answering, without a token: /api/health is
// public. ok is false when nothing answers at all.
func (c *client) health(ctx context.Context) (version string, ok bool) {
	var h struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	hc := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/health", nil)
	if err != nil {
		return "", false
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if json.NewDecoder(resp.Body).Decode(&h) != nil {
		return "", false
	}
	return h.Version, true
}

// networkInfo is /api/network as the CLI reads it.
type networkInfo struct {
	State     string   `json:"state"`
	Enabled   bool     `json:"enabled"`
	AuthURL   string   `json:"authUrl"`
	Hostname  string   `json:"hostname"`
	IPs       []string `json:"ips"`
	URL       string   `json:"url"`
	HTTPS     bool     `json:"https"`
	HTTPSHint string   `json:"httpsHint"`
	Error     string   `json:"error"`
	Choice    string   `json:"choice"`
}

func (c *client) network(ctx context.Context) (networkInfo, error) {
	var n networkInfo
	err := c.do(ctx, http.MethodGet, "/api/network", nil, &n)
	return n, err
}

func (c *client) setNetwork(ctx context.Context, on bool) (networkInfo, error) {
	var n networkInfo
	path := "/api/network/disable"
	if on {
		path = "/api/network/enable"
	}
	err := c.do(ctx, http.MethodPost, path, map[string]string{}, &n)
	return n, err
}

type phoneLink struct {
	URL   string `json:"url"`
	Link  string `json:"link"`
	HTTPS bool   `json:"https"`
}

func (c *client) phone(ctx context.Context) (phoneLink, error) {
	var p phoneLink
	err := c.do(ctx, http.MethodGet, "/api/network/phone", nil, &p)
	return p, err
}

type runnerInfo struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Online bool   `json:"online"`
}

func (c *client) runners(ctx context.Context) ([]runnerInfo, error) {
	var r struct {
		Runners []runnerInfo `json:"runners"`
	}
	err := c.do(ctx, http.MethodGet, "/api/runners", nil, &r)
	return r.Runners, err
}

type agentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	SignIn    string `json:"signIn"`
	Detail    string `json:"detail"`
	Guidance  string `json:"guidance"`
}

func (c *client) agents(ctx context.Context) ([]agentInfo, error) {
	var r struct {
		Agents []agentInfo `json:"agents"`
	}
	err := c.do(ctx, http.MethodGet, "/api/agents", nil, &r)
	return r.Agents, err
}

func (c *client) doctor(ctx context.Context) (doctor.Report, error) {
	var r doctor.Report
	err := c.do(ctx, http.MethodGet, "/api/doctor", nil, &r)
	return r, err
}
