// Package hostclient is how a device talks to its workspace: the Team API of a Workspace Host, with the device's own
// credential.
//
// A device is reached by the workspace over the workspace's private network, and reaches it the same way. This client
// therefore dials only two kinds of address: the loopback of the computer it runs on (a Workspace Host's own API) and
// addresses inside the workspace's private network. It dials literal addresses only, never a name that could be made to
// point somewhere else; it follows no redirect; and it sends the device's credential to the address it was given and to
// no other. It tries the hosts it knows of in turn, so that no single host is the one way in.
//
// It makes ordinary API requests with the credential the workspace gave this device. It has no way to run anything or to
// reach a computer, and nothing it sends is a command.
package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/envelope"
	"devboard/internal/team/domain"
)

// APIPrefix is where the Team API is.
const APIPrefix = "/api/team/v1"

const maxResponse = 8 << 20

// Client is a device's connection to its workspace.
type Client struct {
	token string
	hc    *http.Client

	mu    sync.Mutex
	bases []string
}

// Options configure a Client.
type Options struct {
	// Bases are the Workspace Hosts' API addresses, "http://127.0.0.1:7430" or "http://10.128.0.1:7430", best first.
	Bases []string
	// Token is the device's own credential.
	Token string
	// Network is the workspace's private network. Addresses inside it may be dialled, as may loopback; nothing else.
	Network netip.Prefix
	// Timeout for one request (30 seconds by default; a long poll adds its own wait).
	Timeout time.Duration
	// DialContext substitutes the transport in tests without substituting API responses.
	// The literal loopback/workspace address check always runs before this dialer.
	DialContext func(context.Context, string, string) (net.Conn, error)
}

// New makes a client. Every base must be a literal address that may be dialled.
func New(o Options) (*Client, error) {
	if o.Token == "" {
		return nil, errors.New("hostclient: a device credential is required")
	}
	if len(o.Bases) == 0 {
		return nil, errors.New("hostclient: no Workspace Host is known")
	}
	var bases []string
	for _, b := range o.Bases {
		nb, err := CheckBase(b, o.Network)
		if err != nil {
			return nil, err
		}
		bases = append(bases, nb)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	d := &net.Dialer{Timeout: 5 * time.Second}
	dial := o.DialContext
	if dial == nil {
		dial = d.DialContext
	}
	network := o.Network
	return &Client{token: o.Token, bases: bases, hc: &http.Client{
		Timeout: timeout + 25*time.Second, // the longest a long poll may wait
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, netw, addr string) (net.Conn, error) {
				if err := checkDial(addr, network); err != nil {
					return nil, err
				}
				return dial(ctx, netw, addr)
			},
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       45 * time.Second,
			ResponseHeaderTimeout: timeout + 25*time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// checkDial allows loopback and the workspace's own network, by literal address.
func checkDial(addr string, network netip.Prefix) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return fmt.Errorf("hostclient: %s is not a literal address", addr)
	}
	if ip.IsLoopback() || (network.IsValid() && network.Contains(ip)) {
		return nil
	}
	return fmt.Errorf("hostclient: %s is not on this computer or in the workspace's network", addr)
}

// CheckBase checks an API address and returns it without a trailing slash.
func CheckBase(s string, network netip.Prefix) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("hostclient: %q is not an http address", s)
	}
	if err := checkDial(u.Host, network); err != nil {
		if u.Port() == "" {
			if err2 := checkDial(net.JoinHostPort(u.Hostname(), "80"), network); err2 == nil {
				return "http://" + u.Host, nil
			}
		}
		return "", err
	}
	return "http://" + u.Host, nil
}

// Bases are the addresses in the order they are tried now.
func (c *Client) Bases() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.bases...)
}

// SetBases replaces the hosts to try (the workspace's own list changes as hosts come and go). Each must be acceptable.
func (c *Client) SetBases(bases []string, network netip.Prefix) error {
	var out []string
	for _, b := range bases {
		nb, err := CheckBase(b, network)
		if err != nil {
			return err
		}
		out = append(out, nb)
	}
	if len(out) == 0 {
		return errors.New("hostclient: no Workspace Host is known")
	}
	c.mu.Lock()
	c.bases = out
	c.mu.Unlock()
	return nil
}

// Error is the workspace refusing or failing a request, in its own words.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// IsStatus reports whether err is a workspace answer with this status.
func IsStatus(err error, status int) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == status
}

// ErrUnreachable means no Workspace Host answered.
var ErrUnreachable = errors.New("no Workspace Host answers")

// Raw makes one request to a Workspace Host and returns its answer as it came. It tries each host in turn when one cannot
// be reached or is not serving (it is read-only without a quorum, or failing); any other answer, including a refusal, is final.
func (c *Client) Raw(ctx context.Context, method, path string, contentType string, body []byte) (status int, header http.Header, out []byte, err error) {
	if !strings.HasPrefix(path, APIPrefix+"/") {
		return 0, nil, nil, fmt.Errorf("hostclient: %q is not part of the Team API", path)
	}
	bases := c.Bases()
	safe := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	var last error
	for i, base := range bases {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, err := c.hc.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil, nil, ctx.Err()
			}
			var dial *net.OpError
			if !safe && !(errors.As(err, &dial) && dial.Op == "dial") {
				return 0, nil, nil, errors.New("the workspace's answer was lost; refresh before trying this change again")
			}
			last = err
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
		res.Body.Close()
		if readErr != nil || len(raw) > maxResponse {
			return 0, nil, nil, errors.New("the workspace's answer was incomplete or too large; refresh before trying again")
		}
		if safe && res.StatusCode >= 500 && i < len(bases)-1 {
			last = fmt.Errorf("%s answered %d", base, res.StatusCode)
			continue
		}
		if i > 0 { // remember the one that answered, so the next request tries it first
			c.mu.Lock()
			if len(c.bases) == len(bases) {
				c.bases = append([]string{base}, without(bases, base)...)
			}
			c.mu.Unlock()
		}
		return res.StatusCode, res.Header, raw, nil
	}
	if last == nil {
		last = ErrUnreachable
	}
	return 0, nil, nil, fmt.Errorf("%w: %v", ErrUnreachable, last)
}

func without(all []string, x string) []string {
	var out []string
	for _, a := range all {
		if a != x {
			out = append(out, a)
		}
	}
	return out
}

// Do makes a JSON request and decodes a successful answer into out.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	ct := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ct = b, "application/json"
	}
	status, _, raw, err := c.Raw(ctx, method, APIPrefix+path, ct, body)
	if err != nil {
		return err
	}
	if status >= 300 {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return &Error{Status: status, Code: e.Error.Code, Message: e.Error.Message}
		}
		return &Error{Status: status, Message: fmt.Sprintf("the workspace answered %d", status)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ---- what a device asks of its workspace ----

// Me is who the credential belongs to.
type Me struct {
	Member    domain.Member    `json:"member"`
	Workspace domain.Workspace `json:"workspace"`
}

// Me says who this device acts for.
func (c *Client) Me(ctx context.Context) (Me, error) {
	var m Me
	err := c.Do(ctx, "GET", "/me", nil, &m)
	return m, err
}

// Heartbeat says this device is here and what kind of machine it is.
func (c *Client) Heartbeat(ctx context.Context, p domain.DeviceProfile) error {
	return c.Do(ctx, "POST", "/device/heartbeat", map[string]any{"platform": p.Platform, "form": string(p.Form), "sleeps": p.Sleeps, "sleepEvents": p.SleepEvents, "version": p.Version}, nil)
}

// Inbox collects the signed requests waiting for this device, waiting up to wait for one.
func (c *Client) Inbox(ctx context.Context, wait time.Duration) ([]domain.DeviceMessage, error) {
	var out []domain.DeviceMessage
	err := c.Do(ctx, "GET", fmt.Sprintf("/device/messages?wait=%d", int(wait/time.Second)), nil, &out)
	return out, err
}

// Ack says what this device did with a request.
func (c *Client) Ack(ctx context.Context, id string, state domain.MessageState, result any) error {
	var raw json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		raw = b
	}
	return c.Do(ctx, "POST", "/device/messages/"+id+"/ack", map[string]any{"state": string(state), "result": raw}, nil)
}

// Send gives the workspace a request this device signed, for another of its owner's devices.
func (c *Client) Send(ctx context.Context, e envelope.Envelope) (domain.DeviceMessage, error) {
	var m domain.DeviceMessage
	err := c.Do(ctx, "POST", "/messages", e, &m)
	return m, err
}

// Message says what happened to a request this device's owner made.
func (c *Client) Message(ctx context.Context, id string) (domain.DeviceMessage, error) {
	var m domain.DeviceMessage
	err := c.Do(ctx, "GET", "/messages/"+id, nil, &m)
	return m, err
}

// Devices lists the devices the credential's owner may see (their own, or all of them for an administrator).
func (c *Client) Devices(ctx context.Context) ([]domain.Device, error) {
	var ds []domain.Device
	err := c.Do(ctx, "GET", "/devices", nil, &ds)
	return ds, err
}

// Handoff is a ticket's context for the person who holds it, as much as the bridge needs.
type Handoff struct {
	Schema string `json:"schema"`
	Ticket struct {
		Key   string `json:"key"`
		Title string `json:"title"`
	} `json:"ticket"`
	Project struct {
		Name string `json:"name"`
	} `json:"project"`
	Git struct {
		BaseBranch string `json:"baseBranch"`
		Repository string `json:"repository"`
		Branch     string `json:"branch"`
	} `json:"git"`
	Prompt string `json:"prompt"`
}

// HandoffSchema is the only handoff this client understands.
const HandoffSchema = "werkbord-team.handoff/v1"

// Handoff fetches the context of a ticket the credential's owner holds.
func (c *Client) Handoff(ctx context.Context, projectID, ticketID string) (Handoff, error) {
	var h Handoff
	if err := c.Do(ctx, "POST", "/projects/"+projectID+"/tickets/"+ticketID+"/handoff", map[string]any{}, &h); err != nil {
		return h, err
	}
	if h.Schema != HandoffSchema {
		return h, fmt.Errorf("this workspace sent a handoff of a kind (%q) that this program does not understand; update Werkbord Team", h.Schema)
	}
	return h, nil
}

// NetworkConfig is what this device's network node should be.
func (c *Client) NetworkConfig(ctx context.Context) (*enrollment.NetworkBundle, error) {
	var b enrollment.NetworkBundle
	if err := c.Do(ctx, "GET", "/network/config", nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// RenewCertificate asks for a new certificate for this device's own network key.
func (c *Client) RenewCertificate(ctx context.Context) (*enrollment.NetworkBundle, error) {
	var b enrollment.NetworkBundle
	if err := c.Do(ctx, "POST", "/network/certificate", map[string]any{}, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// CollectProvision fetches what was sealed to this device when an administrator made it a Workspace Host. It is ciphertext.
func (c *Client) CollectProvision(ctx context.Context) ([]byte, error) {
	var out struct {
		Sealed []byte `json:"sealed"`
	}
	if err := c.Do(ctx, "GET", "/network/provision", nil, &out); err != nil {
		return nil, err
	}
	return out.Sealed, nil
}

// AckProvision says it was stored.
func (c *Client) AckProvision(ctx context.Context) error {
	return c.Do(ctx, "POST", "/network/provision/ack", map[string]any{}, nil)
}
