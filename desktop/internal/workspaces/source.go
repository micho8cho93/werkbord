// Package workspaces is the Werkbord desktop app's model of the places work lives: Personal, and any number of Team
// workspaces, which it lists, remembers a choice among, opens, and reads a neutral summary of (internal/workspace).
//
// It is how the app joins two products without joining them. Each product is a program on this computer with its own
// credential, its own data and its own authority. This package talks to each over its own loopback address with the
// credential the app is allowed to hold for it, treats every answer as something to validate, and hands the app's window
// only what the window needs. It starts no process, writes no credential into any page's storage and gives no workspace
// anything of another's: Personal's data is never sent to a Team service, and a Team workspace's is never sent anywhere
// but the window of the person it belongs to.
package workspaces

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"devboard/internal/workspace"
)

// Frame is where a workspace's own interface is, as the window should load it. The URL may carry a credential in its
// fragment (which no server receives and no log records); it is for loading and is never logged, shown or stored.
type Frame struct {
	URL    string `json:"url"`
	Origin string `json:"origin"`
}

// Source is a program that holds workspaces.
type Source interface {
	// Name is for logs and for saying which program a problem is about.
	Name() string
	// List says which workspaces the source holds. It reports an error when the source is not answering; the registry then
	// says so instead of showing its workspaces as if they were fine.
	List(ctx context.Context) ([]workspace.Listed, error)
	// Frame is how to load a workspace's interface. join is an invitation the person opened the app with, if it is for a
	// workspace that takes one.
	Frame(ctx context.Context, l workspace.Listed, join string) (Frame, error)
	// Summary reads a workspace's summary.
	Summary(ctx context.Context, l workspace.Listed) (workspace.Summary, error)
}

// Why a source cannot be used, so the app can say the right thing.
var (
	// ErrNotRunning: nothing answers at the source's address.
	ErrNotRunning = errors.New("the service is not running")
	// ErrRefused: it answers and does not accept this computer's credential.
	ErrRefused = errors.New("the service did not accept this app's credential")
	// ErrOutdated: it answers and does not know what the app asks of it; it needs updating.
	ErrOutdated = errors.New("the service is older than this app and needs updating")
	// ErrNoCredential: there is no credential to ask with, so the service has not been set up for this person.
	ErrNoCredential = errors.New("the service has not been set up for this person")
)

// loopbackClient talks only to literal loopback addresses, with no proxy, no redirect and no name lookup: the address
// a source is given is the address it is spoken to.
func loopbackClient(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: timeout}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
					return nil, fmt.Errorf("%s is not on this computer", address)
				}
				return d.DialContext(ctx, network, address)
			},
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// CheckBase accepts http://127.0.0.1:PORT (or localhost, or [::1]) and returns it as a literal loopback origin.
func CheckBase(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Port() == "" {
		return "", fmt.Errorf("%q is not an address like http://127.0.0.1:7420", s)
	}
	host := u.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("%q is not on this computer", s)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + u.Port(), nil
}

// get reads a path from a loopback service with a bearer credential, bounded.
func get(ctx context.Context, hc *http.Client, base, path, key string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, ErrNotRunning
		}
		if strings.Contains(err.Error(), "connection refused") {
			return nil, ErrNotRunning
		}
		return nil, err
	}
	switch {
	case res.StatusCode == http.StatusOK:
		return res.Body, nil
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		res.Body.Close()
		return nil, ErrRefused
	case res.StatusCode == http.StatusNotFound:
		res.Body.Close()
		return nil, ErrOutdated
	}
	res.Body.Close()
	return nil, fmt.Errorf("the service answered %d", res.StatusCode)
}
