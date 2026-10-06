package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strconv"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
)

// NetworkOptions are the choices made when a workspace gets its private network.
type NetworkOptions struct {
	// Range is the network's address range; empty chooses one (a /16 in 10.0.0.0/8, away
	// from the ranges homes and offices usually use).
	Range string
	// Connectivity: "auto" makes this host a Connectivity Host if an address it advertises
	// could be reached from outside; "yes" requires that; "no" never does.
	Connectivity string
	// Approval is whether an administrator must approve every device that joins.
	Approval domain.ApprovalPolicy
}

// NetworkCreated says what was made, for the person who made it to read.
type NetworkCreated struct {
	Fingerprint        string
	Range              string
	HostDeviceID       string
	HostAddress        string
	ConnectivityHost   bool
	BootstrapEndpoints []string
	NetworkEndpoints   []string
	Warnings           []string
}

// CreateNetwork gives a workspace that was just created its private network, on this
// host, which becomes its first Workspace Host:
//
//   - the workspace's own trust identity and the network's certificate authority are made,
//     and sealed on this host (internal/team/infra/pki) and nowhere else;
//   - this host gets its own keys (application, network, sealing), distinct from those;
//   - it is registered as a device of the owner, with the Workspace Host capability, and,
//     if it can be reached from outside and the owner has not said otherwise, the
//     Connectivity Host capability as well;
//   - the network's first address and certificate are issued to it.
//
// What this host then holds (the workspace's key and the authority's key) is what makes a
// Workspace Host a high-trust machine: docs/TEAM_NETWORK.md.
func CreateNetwork(ctx context.Context, cfg config.Config, log *slog.Logger, svc *service.Service, created service.Created, opt NetworkOptions) (_ NetworkCreated, err error) {
	if err := cfg.Validate(); err != nil {
		return NetworkCreated{}, err
	}
	if _, serr := os.Stat(cfg.PKIDir()); serr == nil {
		return NetworkCreated{}, fmt.Errorf("%s already exists: this data directory already has a workspace's keys", cfg.PKIDir())
	}
	prefix, err := chooseRange(opt.Range)
	if err != nil {
		return NetworkCreated{}, err
	}
	sealer, err := SealerFor(cfg)
	if err != nil {
		return NetworkCreated{}, err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return NetworkCreated{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(cfg.PKIDir()) // a half-made vault would only get in the way of trying again
		}
	}()
	ws := created.Workspace
	now := time.Now()
	mat, err := v.Create(ws.ID, ws.Name, prefix, now)
	if err != nil {
		return NetworkCreated{}, err
	}
	auth, err := newAuthority(mat, apiPortOr(cfg.Addr))
	if err != nil {
		return NetworkCreated{}, err
	}
	svc.SetNetwork(auth)

	var boot, nets []string
	for _, h := range cfg.Endpoints {
		boot = append(boot, net.JoinHostPort(h, strconv.Itoa(cfg.BootstrapPort())))
		nets = append(nets, net.JoinHostPort(h, strconv.Itoa(cfg.NetworkPort)))
	}
	possible := service.ConnectivityPossible(nets)
	conn := false
	var warn []string
	switch opt.Connectivity {
	case "", "auto":
		conn = possible
	case "yes":
		if !possible {
			return NetworkCreated{}, errors.New("this host cannot be made a Connectivity Host: none of its advertised addresses can be reached from outside its own network (set WERKBORD_TEAM_ENDPOINTS to a public address or DNS name that leads here, with the ports forwarded)")
		}
		conn = true
	case "no":
	default:
		return NetworkCreated{}, fmt.Errorf("connectivity is auto, yes or no, not %q", opt.Connectivity)
	}
	if !conn {
		warn = append(warn, "This host is not a Connectivity Host, so remote access cannot be guaranteed: devices on other networks can reach the workspace only if they can reach a machine that you own and that is reachable from the Internet. Add one (a small server or a port-forwarded host), and preferably two. See docs/TEAM_NETWORK.md.")
	}

	hostPub := deviceid.Public{ID: deviceid.ID(mat.Host.DeviceID()), Name: hostName(), PublicKey: deviceid.EncodePublicKey(mat.Host.PublicKey()), CreatedAt: now}
	proof := mat.Host.Sign(deviceid.RegistrationStatement(ws.ID, created.Owner.ID, hostPub.ID, hostPub.Name, mat.Host.PublicKey()))
	_, netPub, err := mat.Host.NetworkKeyPEMs()
	if err != nil {
		return NetworkCreated{}, err
	}
	dn, err := svc.BootstrapNetwork(ctx, ws.ID, created.Owner.ID, service.HostBootstrap{Device: hostPub, Proof: proof, NetworkPublicKey: string(netPub), SealingKey: mat.Host.SealingPublicKey(),
		BootstrapEndpoints: boot, NetworkEndpoints: nets, Connectivity: conn, Approval: opt.Approval})
	if err != nil {
		return NetworkCreated{}, err
	}
	log.Info("the workspace's private network was created", "range", prefix.String(), "fingerprint", mat.Trust.Fingerprint(), "connectivityHost", conn)
	return NetworkCreated{Fingerprint: mat.Trust.Fingerprint(), Range: prefix.String(), HostDeviceID: dn.DeviceID, HostAddress: dn.OverlayAddr, ConnectivityHost: conn,
		BootstrapEndpoints: boot, NetworkEndpoints: nets, Warnings: warn}, nil
}

func hostName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "workspace host"
}

// chooseRange validates a range the owner chose, or picks one: a /16 in 10.0.0.0/8 whose
// second octet is random from 128 to 254, which is rarely taken by a home or an office.
func chooseRange(s string) (netip.Prefix, error) {
	if s != "" {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("the network range %q: %w", s, err)
		}
		return p, pki.CheckNetworkRange(p)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(127))
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(128 + n.Int64()), 0, 0}), 16), nil
}
