package netprivate

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/envknob"
	"tailscale.com/tsnet"
)

// TailscaleOptions configures the embedded Tailscale node.
type TailscaleOptions struct {
	// Dir holds the node's state: its keys, so it keeps its identity (and its
	// address) across restarts. It is created with mode 0700.
	Dir string
	// Hostname is the node's name on the tailnet; see DefaultHostname.
	Hostname string
	// AuthKey, if set, signs the node in without a browser: for headless
	// computers, where the user made a key in their Tailscale admin console. It
	// is only read from the environment, never stored by Dev Board.
	AuthKey string
	// ControlURL points at a self-hosted coordination server (Headscale) instead
	// of Tailscale's own. Empty means Tailscale's.
	ControlURL string
	Log        *slog.Logger
}

// NewTailscale returns a Backend that runs a Tailscale node inside this process,
// in userspace: it needs no root, no TUN device and no system Tailscale.
func NewTailscale(o TailscaleOptions) Backend {
	return &tsBackend{opt: o}
}

type tsBackend struct {
	opt TailscaleOptions
	srv *tsnet.Server
	lc  *local.Client
}

func (b *tsBackend) Start(context.Context) error {
	if err := os.MkdirAll(b.opt.Dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", b.opt.Dir, err)
	}
	// Tailscale's client uploads diagnostic logs to Tailscale unless told not to.
	// Dev Board's stance is that nothing leaves this computer that the user did not
	// choose, so they are off, unless the user has set the variable themselves.
	if _, set := os.LookupEnv("TS_NO_LOGS_NO_SUPPORT"); !set {
		envknob.SetNoLogsNoSupport()
	}
	log := b.opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	b.srv = &tsnet.Server{
		Dir:        b.opt.Dir,
		Hostname:   b.opt.Hostname,
		AuthKey:    b.opt.AuthKey,
		ControlURL: b.opt.ControlURL,
		// What tsnet says to the user (including the sign-in link) is shown by the
		// app, not logged: the link is a one-time credential for the user.
		UserLogf: func(string, ...any) {},
		Logf:     func(f string, a ...any) { log.Debug("tailscale: " + strings.TrimSpace(fmt.Sprintf(f, a...))) },
	}
	if err := b.srv.Start(); err != nil {
		return err
	}
	lc, err := b.srv.LocalClient()
	if err != nil {
		_ = b.srv.Close()
		return err
	}
	b.lc = lc
	return nil
}

func (b *tsBackend) Status(ctx context.Context) (BackendStatus, error) {
	st, err := b.lc.StatusWithoutPeers(ctx)
	if err != nil {
		return BackendStatus{}, err
	}
	out := BackendStatus{State: st.BackendState, AuthURL: st.AuthURL, IPs: st.TailscaleIPs, Health: st.Health}
	magicDNS := true
	if st.CurrentTailnet != nil {
		out.Tailnet = st.CurrentTailnet.Name
		magicDNS = st.CurrentTailnet.MagicDNSEnabled
		// HTTPS needs the tailnet to have MagicDNS and certificates turned on.
		out.HTTPS = magicDNS && len(st.CertDomains) > 0
	}
	// Without MagicDNS the node's name does not resolve on other devices, so it is
	// not offered: the address is. A bare host name (no tailnet suffix) does not
	// resolve either.
	if st.Self != nil && magicDNS && strings.Contains(strings.TrimSuffix(st.Self.DNSName, "."), ".") {
		out.DNSName = st.Self.DNSName
	}
	return out, nil
}

func (b *tsBackend) Listen(addr string, tls bool) (net.Listener, error) {
	if tls {
		return b.srv.ListenTLS("tcp", addr)
	}
	return b.srv.Listen("tcp", addr)
}

func (b *tsBackend) Close() error {
	if b.srv == nil {
		return nil
	}
	return b.srv.Close()
}

var nonHostname = regexp.MustCompile(`[^a-z0-9]+`)

// DefaultHostname is the node's name on the tailnet when the user did not pick
// one: "devboard-" and this computer's name, so several computers each get their
// own and the name says what it is.
func DefaultHostname(machine string) string {
	machine = strings.TrimSuffix(strings.ToLower(machine), ".local")
	machine = strings.Trim(nonHostname.ReplaceAllString(machine, "-"), "-")
	name := "devboard"
	if machine != "" && machine != "localhost" {
		name += "-" + machine
	}
	if len(name) > 63 { // a DNS label
		name = strings.TrimRight(name[:63], "-")
	}
	return name
}

// StateDir is where the node's state lives under a Dev Board data directory.
func StateDir(dataDir string) string { return filepath.Join(dataDir, "tailscale") }

// Dial reaches a controller from an embedded runner node without a system VPN.
func (b *tsBackend) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	return b.srv.Dial(ctx, network, address)
}
