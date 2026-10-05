// Package netprivate is Werkbord's own way onto a private network, so that a
// phone can reach the controller without anyone setting up a VPN, a tunnel,
// certificates or port forwarding.
//
// It embeds a Tailscale node (tailscale.com/tsnet) in the controller process.
// That node joins the user's own tailnet, listens only on the tailnet, and
// serves the same app and API the controller serves on this computer. Nothing
// here is exposed to the Internet: tsnet listeners are reachable only from
// devices in the user's tailnet, and Funnel (Tailscale's public ingress) is
// never used. Werkbord runs no relay, coordination server or account of its
// own; the only third party is Tailscale's, which the user signs in to with
// their own identity.
//
// The network is optional. Without it the controller listens on loopback only,
// exactly as before.
package netprivate

import (
	"context"
	"net"
	"net/netip"
)

// State is where bringing the private network up has got to.
type State string

const (
	// StateOff: not enabled. Nothing listens on a private network.
	StateOff State = "off"
	// StateStarting: the node is starting, or reconnecting.
	StateStarting State = "starting"
	// StateNeedsLogin: the user has to sign in; Status.AuthURL is where.
	StateNeedsLogin State = "needs_login"
	// StateNeedsApproval: signed in, but the tailnet's admin has to approve the
	// device before it is let in.
	StateNeedsApproval State = "needs_approval"
	// StateConnected: the node is on the tailnet and the controller is reachable
	// at Status.URL.
	StateConnected State = "connected"
	// StateError: it could not be brought up; Status.Error says why.
	StateError State = "error"
)

// Status is what the app shows about the private network. It never contains a
// secret: the Werkbord access token is not part of it, and neither is any
// Tailscale key. AuthURL is a one-time sign-in link for the user, shown to them
// and never logged.
type Status struct {
	State   State `json:"state"`
	Enabled bool  `json:"enabled"`
	// AuthURL is where to sign in, while State is StateNeedsLogin.
	AuthURL string `json:"authUrl,omitempty"`
	// Hostname is this controller's name on the tailnet (a MagicDNS name).
	Hostname string `json:"hostname,omitempty"`
	// IPs are its tailnet addresses.
	IPs []string `json:"ips,omitempty"`
	// URL is the address to open on a phone or another computer on the tailnet:
	// https when the tailnet can issue certificates, http otherwise (the traffic
	// is encrypted by the tailnet either way). It carries no token.
	URL string `json:"url,omitempty"`
	// HTTPS says whether URL is https. When it is not, HTTPSHint says what to do
	// to get it, which matters because browsers only install an app, and only
	// allow notifications, from a secure address.
	HTTPS     bool   `json:"https"`
	HTTPSHint string `json:"httpsHint,omitempty"`
	// Tailnet is the name of the network the node joined.
	Tailnet string `json:"tailnet,omitempty"`
	// Health lists problems the network reports about itself.
	Health []string `json:"health,omitempty"`
	// Error says why StateError.
	Error string `json:"error,omitempty"`
}

// Backend is the network node: the real one is tsnet, and tests use a fake.
type Backend interface {
	// Start initialises the node. It returns quickly; whether the user must sign
	// in is learned from Status.
	Start(ctx context.Context) error
	// Status is a snapshot of the node.
	Status(ctx context.Context) (BackendStatus, error)
	// Listen listens on the tailnet only. With tls set it serves TLS with a
	// certificate for the node's name, which needs HTTPS enabled for the tailnet.
	Listen(addr string, tls bool) (net.Listener, error)
	// Close stops the node.
	Close() error
}

// BackendStatus is what a Backend reports.
type BackendStatus struct {
	// State is Tailscale's own: NoState, NeedsLogin, NeedsMachineAuth, Stopped,
	// Starting or Running.
	State   string
	AuthURL string
	// DNSName is the node's MagicDNS name, with or without a trailing dot.
	DNSName string
	IPs     []netip.Addr
	Tailnet string
	// HTTPS is whether the tailnet can issue this node a certificate.
	HTTPS  bool
	Health []string
}

// Tailscale's backend states.
const (
	backendRunning          = "Running"
	backendNeedsLogin       = "NeedsLogin"
	backendNeedsMachineAuth = "NeedsMachineAuth"
)
