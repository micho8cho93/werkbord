package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func firstEnv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// parseBase checks an http(s) base address and returns it without a trailing slash.
func parseBase(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not an http or https address", s)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// doJSON sends a request with a bearer token and decodes the JSON answer. The
// token goes only to the address it was given for; redirects are not followed, so
// it cannot be handed on.
func doJSON(ctx context.Context, hc *http.Client, method, addr, token string, body, into any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, addr, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return fmt.Errorf("%s (%d)", e.Error.Message, res.StatusCode)
		}
		return fmt.Errorf("answered %d", res.StatusCode)
	}
	if into == nil || len(raw) == 0 { // an answer with no body (204), or one the caller does not want
		return nil
	}
	return json.Unmarshal(raw, into)
}

// findTicket resolves --project and --ticket to ids using the member's own view.
