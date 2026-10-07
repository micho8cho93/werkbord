package enrollment

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"devboard/internal/httpkit"
)

// Signer is the joining device's application identity. The device holds the private
// key (the individual product's localidentity satisfies this); this package never
// does, and never sees it.
type Signer interface {
	DeviceID() string
	PublicKey() ed25519.PublicKey
	Sign(msg []byte) []byte
}

// JoinParams is what a device brings to Join.
type JoinParams struct {
	Signer Signer
	// MemberName is the name of the person, used when the invitation makes a new member.
	MemberName string
	// DeviceName names this device.
	DeviceName string
	// NetworkPublicKeyPEM is the public half of the key this device made for the private
	// network. The workspace signs a certificate for it; the private half stays here.
	NetworkPublicKeyPEM string
	// SealingPublicKey is set by a device that will be a Workspace Host: the public key
	// (X25519, base64 raw URL) workspace secrets are sealed to when handed to it.
	SealingPublicKey string
	// ExpectFingerprint, if set, is a workspace fingerprint the person learned some way
	// other than from this invitation. Join refuses an invitation for any other key.
	ExpectFingerprint string
	// Wait is how long to wait for an administrator's approval, if the workspace wants
	// one. Zero does not wait: Join returns ErrPending with the enrollment's ID in the result.
	Wait time.Duration
	// PollEvery is how often to ask while waiting (DefaultPoll).
	PollEvery time.Duration
	// Now and DialContext are for tests; the defaults are the clock and the network.
	Now         func() time.Time
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Result is a completed (or pending) enrollment, as the device sees it.
type Result struct {
	State        State
	EnrollmentID string
	Response     Response
	// Network is the private-network bundle; nil unless approved.
	Network *NetworkBundle
}

// Join enrolls this device using an invitation: it checks the invitation, reaches the
// workspace at one of the invitation's own endpoints, makes the workspace prove it is
// the one the invitation names before sending anything, presents the one-time
// credential with this device's public keys, waits for approval if the workspace
// requires it, and returns what the workspace issued.
func Join(ctx context.Context, inv Invitation, p JoinParams) (*Result, error) {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	if p.Signer == nil {
		return nil, errors.New("enrollment: a device identity is required")
	}
	if now().After(inv.Expiry()) {
		return nil, ErrExpired
	}
	pin, err := inv.Key()
	if err != nil {
		return nil, err
	}
	if !SameFingerprint(inv.Fingerprint, WorkspaceFingerprint(pin)) {
		return nil, ErrBadSignature
	}
	if p.ExpectFingerprint != "" && !SameFingerprint(p.ExpectFingerprint, inv.Fingerprint) {
		return nil, ErrWrongWorkspace
	}
	c := &client{inv: inv, pin: pin, p: p, now: now}
	req := JoinRequest{
		InviteID: inv.ID, Credential: inv.Credential, MemberName: p.MemberName,
		DeviceID: p.Signer.DeviceID(), DeviceName: p.DeviceName,
		DevicePublicKey:  base64.RawURLEncoding.EncodeToString(p.Signer.PublicKey()),
		NetworkPublicKey: p.NetworkPublicKeyPEM, SealingPublicKey: p.SealingPublicKey,
	}
	resp, endpoint, err := c.firstEndpoint(ctx, func(ctx context.Context, ep string) (Response, error) {
		return c.send(ctx, ep, http.MethodPost, PathJoin, func(ex []byte) (any, error) {
			r := req
			r.Proof = base64.RawURLEncoding.EncodeToString(p.Signer.Sign(JoinStatement(inv.WorkspaceID, r, ex)))
			return r, nil
		})
	})
	if err != nil {
		return nil, err
	}
	if resp.State == StatePending && p.Wait > 0 {
		resp, err = c.waitForApproval(ctx, endpoint, resp)
		if err != nil {
			return nil, err
		}
	}
	return c.finish(resp)
}

type client struct {
	inv Invitation
	pin ed25519.PublicKey
	p   JoinParams
	now func() time.Time
}

// firstEndpoint tries the invitation's endpoints in order and returns the first
// definitive answer. An endpoint that cannot be reached, or that cannot prove it is
// the workspace, is passed over; one that answers (including by refusing) is final.
func (c *client) firstEndpoint(ctx context.Context, do func(context.Context, string) (Response, error)) (Response, string, error) {
	var last error
	for _, ep := range c.inv.Endpoints {
		resp, err := do(ctx, ep)
		switch {
		case err == nil:
			return resp, ep, nil
		case errors.Is(err, ErrRefused), errors.Is(err, ErrDenied), isRejected(err), ctx.Err() != nil:
			return Response{}, "", err
		}
		last = err
	}
	if last == nil {
		last = ErrNoEndpoint
	}
	return Response{}, "", fmt.Errorf("%w: %v", ErrNoEndpoint, last)
}

func (c *client) waitForApproval(ctx context.Context, endpoint string, resp Response) (Response, error) {
	every := c.p.PollEvery
	if every <= 0 {
		every = DefaultPoll
	}
	deadline := time.NewTimer(c.p.Wait)
	defer deadline.Stop()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for resp.State == StatePending {
		select {
		case <-ctx.Done():
			return resp, ctx.Err()
		case <-deadline.C:
			return resp, nil // still pending: finish reports it
		case <-tick.C:
		}
		next, err := c.send(ctx, endpoint, http.MethodPost, PathPoll, func(ex []byte) (any, error) {
			r := PollRequest{EnrollmentID: resp.EnrollmentID, DeviceID: c.p.Signer.DeviceID()}
			r.Proof = base64.RawURLEncoding.EncodeToString(c.p.Signer.Sign(PollStatement(c.inv.WorkspaceID, r, ex)))
			return r, nil
		})
		switch {
		case err == nil:
			next.EnrollmentID = resp.EnrollmentID
			resp = next
		case errors.Is(err, ErrDenied), errors.Is(err, ErrRefused), isRejected(err):
			return resp, err
		default:
			// A bootstrap host that is briefly away is not a refusal: keep asking.
		}
	}
	return resp, nil
}

// finish checks what came back before it is believed.
func (c *client) finish(resp Response) (*Result, error) {
	switch resp.State {
	case StatePending:
		return &Result{State: StatePending, EnrollmentID: resp.EnrollmentID, Response: resp}, ErrPending
	case StateDenied:
		return nil, ErrDenied
	case StateApproved:
	default:
		return nil, fmt.Errorf("enrollment: the workspace answered with state %q", resp.State)
	}
	wk, err := base64.RawURLEncoding.DecodeString(resp.WorkspaceKey)
	switch {
	case err != nil || !ed25519.PublicKey(wk).Equal(c.pin):
		return nil, errNotWorkspace
	case resp.WorkspaceID != c.inv.WorkspaceID:
		return nil, errNotWorkspace
	case resp.DeviceID != c.p.Signer.DeviceID(), resp.MemberID == "", resp.DeviceToken == "":
		return nil, errors.New("enrollment: the workspace's answer is incomplete")
	case resp.Network == nil || resp.Network.NodeCertificate == "" || resp.Network.CACertificate == "" || resp.Network.Config == "":
		return nil, errors.New("enrollment: the workspace's answer has no network credentials")
	}
	return &Result{State: StateApproved, EnrollmentID: resp.EnrollmentID, Response: resp, Network: resp.Network}, nil
}

// send makes one request over one fresh connection to ep, after the server has
// proved it is the workspace, with a body that may depend on the connection, and
// reads the answer as a Response.
func (c *client) send(ctx context.Context, ep, method, path string, body func(exporter []byte) (any, error)) (Response, error) {
	status, data, err := c.exchange(ctx, ep, method, path, body)
	if err != nil {
		return Response{}, err
	}
	switch status {
	case http.StatusOK, http.StatusAccepted:
		var out Response
		if err := json.Unmarshal(data, &out); err != nil {
			return Response{}, fmt.Errorf("enrollment: the workspace's answer is not understood: %v", err)
		}
		return out, nil
	case http.StatusForbidden, http.StatusNotFound, http.StatusUnauthorized, http.StatusGone:
		var out Response
		if json.Unmarshal(data, &out) == nil && out.State == StateDenied {
			return Response{}, ErrDenied
		}
		return Response{}, ErrRefused
	case http.StatusConflict:
		var eb httpkit.ErrorBody
		if json.Unmarshal(data, &eb) == nil && eb.Error.Message != "" {
			return Response{}, &RejectedError{Reason: eb.Error.Message}
		}
		return Response{}, ErrRefused
	case http.StatusTooManyRequests:
		return Response{}, errors.New("enrollment: the workspace is busy; try again in a minute")
	default:
		return Response{}, fmt.Errorf("enrollment: the workspace answered with status %d", status)
	}
}

func (c *client) exchange(ctx context.Context, ep, method, path string, body func(exporter []byte) (any, error)) (int, []byte, error) {
	host, _, err := net.SplitHostPort(ep)
	if err != nil {
		return 0, nil, err
	}
	dial := c.p.DialContext
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second}
		dial = d.DialContext
	}
	raw, err := dial(ctx, "tcp", ep)
	if err != nil {
		return 0, nil, err
	}
	cfg := pinnedTLS(c.pin, host, c.now)
	if net.ParseIP(host) == nil {
		cfg.ServerName = host
	}
	conn := tls.Client(raw, cfg)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := conn.HandshakeContext(ctx); err != nil {
		return 0, nil, err
	}
	ex, err := exporter(conn.ConnectionState())
	if err != nil {
		return 0, nil, err
	}
	var rd io.Reader
	if body != nil {
		v, err := body(ex)
		if err != nil {
			return 0, nil, err
		}
		b, err := json.Marshal(v)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+ep+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	if err := req.Write(conn); err != nil {
		return 0, nil, err
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, data, err
}

// Probe contacts one endpoint of a workspace, verifies that it is the workspace with
// the given public key, and returns what it says about itself. It sends no
// credential and no device key. It is how a device, or an administrator's tool,
// checks that a bootstrap endpoint is reachable from where it stands and is really
// the workspace's: the basis of a connectivity check that needs no outside service.
func Probe(ctx context.Context, endpoint string, pin ed25519.PublicKey, now func() time.Time, dial func(ctx context.Context, network, addr string) (net.Conn, error)) (Hello, error) {
	if now == nil {
		now = time.Now
	}
	c := &client{pin: pin, now: now, p: JoinParams{DialContext: dial}}
	status, data, err := c.exchange(ctx, endpoint, http.MethodGet, PathHello, nil)
	if err != nil {
		return Hello{}, err
	}
	var h Hello
	if status != http.StatusOK || json.Unmarshal(data, &h) != nil || !SameFingerprint(h.Fingerprint, WorkspaceFingerprint(pin)) {
		return Hello{}, errNotWorkspace
	}
	return h, nil
}

func isRejected(err error) bool {
	var r *RejectedError
	return errors.As(err, &r)
}

// Resume asks again about an enrollment that was left waiting for an administrator, and returns what the workspace issued
// once it has been approved. It is Join's waiting step on its own, for a device that was shut or restarted while it waited: the
// invitation was spent when the request was accepted, so only this device, with its own key, can collect the answer. Until
// the administrator decides it returns ErrPending (and the enrollment's ID, to ask again); a refusal is ErrDenied.
func Resume(ctx context.Context, inv Invitation, p JoinParams, enrollmentID string) (*Result, error) {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	if p.Signer == nil {
		return nil, errors.New("enrollment: a device identity is required")
	}
	pin, err := inv.Key()
	if err != nil {
		return nil, err
	}
	if !SameFingerprint(inv.Fingerprint, WorkspaceFingerprint(pin)) {
		return nil, ErrBadSignature
	}
	if p.ExpectFingerprint != "" && !SameFingerprint(p.ExpectFingerprint, inv.Fingerprint) {
		return nil, ErrWrongWorkspace
	}
	c := &client{inv: inv, pin: pin, p: p, now: now}
	resp, _, err := c.firstEndpoint(ctx, func(ctx context.Context, ep string) (Response, error) {
		return c.send(ctx, ep, http.MethodPost, PathPoll, func(ex []byte) (any, error) {
			r := PollRequest{EnrollmentID: enrollmentID, DeviceID: p.Signer.DeviceID()}
			r.Proof = base64.RawURLEncoding.EncodeToString(p.Signer.Sign(PollStatement(inv.WorkspaceID, r, ex)))
			return r, nil
		})
	})
	if err != nil {
		return nil, err
	}
	resp.EnrollmentID = enrollmentID
	return c.finish(resp)
}
