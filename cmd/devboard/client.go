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
