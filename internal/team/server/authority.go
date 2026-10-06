package server

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
)

// authority is service.NetworkAuthority made of this host's key vault: it signs
// invitations with the workspace key, certificates with the network authority's key and
// renders node configurations with the policy in infra/overlay. It exists only on a
// Workspace Host that holds those keys.
//
// This file is the one place where the workspace's own vocabulary (capabilities,
// devices, endpoints) meets the infrastructure's (groups, certificates, firewall rules),
// so that neither the service nor the infrastructure has to know the other's.
type authority struct {
	mat   *pki.Material
	ports overlay.Ports
}

var _ service.NetworkAuthority = (*authority)(nil)

func newAuthority(mat *pki.Material, apiPort int) (*authority, error) {
	if mat == nil || !mat.Meta.Authority || mat.Trust == nil || mat.CA == nil {
		return nil, errors.New("this host does not hold the workspace's keys")
	}
	ports := overlay.DefaultPorts()
	if apiPort > 0 {
		ports.API = apiPort
	}
	return &authority{mat: mat, ports: ports}, nil
}

func (a *authority) Info() service.NetworkInfo {
	ca, _ := a.mat.CA.CertificatePEM()
	return service.NetworkInfo{WorkspaceKey: a.mat.Trust.PublicKeyText(), Fingerprint: a.mat.Trust.Fingerprint(), Prefix: a.mat.CA.Prefix(), CACertificate: string(ca)}
}

func (a *authority) SignInvitation(inv enrollment.Invitation) (string, error) {
	return a.mat.Trust.SignInvitation(inv)
}

func (a *authority) IssueCertificate(r service.CertificateRequest, now time.Time) (service.IssuedCertificate, error) {
	is, err := a.mat.CA.Issue(pki.NodeRequest{Name: r.DeviceID, Addr: r.Addr, Groups: domain.GroupsFor(r.Capabilities), PublicKeyPEM: []byte(r.NetworkPublicKey), TTL: r.TTL}, now)
	if err != nil {
		return service.IssuedCertificate{}, err
	}
	return service.IssuedCertificate{CertificatePEM: string(is.CertPEM), Fingerprint: is.Fingerprint, NotAfter: is.NotAfter}, nil
}

func (a *authority) RenderConfig(cfg domain.NodeConfig) (string, error) {
	spec, err := nodeSpec(cfg, a.ports, enrollment.ConfigDirToken+"/"+enrollment.FileCA, enrollment.ConfigDirToken+"/"+enrollment.FileCert, enrollment.ConfigDirToken+"/"+enrollment.FileKey)
	if err != nil {
		return "", err
	}
	out, err := overlay.Render(spec)
	return string(out), err
}

func (a *authority) SealSecrets(sealingKey, deviceID string) ([]byte, error) {
	ca, err := a.mat.CA.CertificatePEM()
	if err != nil {
		return nil, err
	}
	return pki.SealSecrets(sealingKey, a.mat.Meta.WorkspaceID, deviceID, pki.Secrets{
		WorkspaceName: a.mat.Meta.WorkspaceName, TrustSeed: a.mat.Trust.Seed(), NetworkCACertificate: ca, NetworkCAKey: a.mat.CA.PrivateKeyPEM()})
}

// nodeSpec turns the workspace's description of a node into the network's.
func nodeSpec(cfg domain.NodeConfig, ports overlay.Ports, caPath, certPath, keyPath string) (overlay.NodeSpec, error) {
	addr, err := netip.ParseAddr(cfg.OverlayAddr)
	if err != nil {
		return overlay.NodeSpec{}, fmt.Errorf("the device's address %q: %w", cfg.OverlayAddr, err)
	}
	groups := domain.GroupsFor(cfg.Capabilities)
	spec := overlay.NodeSpec{
		Name: cfg.DeviceID, Addr: addr, Bits: cfg.PrefixBits, Discovery: cfg.Discovery, Relay: cfg.Relay, Port: cfg.ListenPort,
		Advertise: cfg.Advertise, UseRelays: true, Blocklist: cfg.Blocklist, Policy: overlay.PolicyFor(groups, ports),
		CA: caPath, Cert: certPath, Key: keyPath,
		// A machine that only connects others (finds them, relays for them) carries no
		// traffic of its own, so it needs no network interface, and so no privileges.
		TunDisabled: onlyConnectivity(groups),
	}
	for _, p := range cfg.DiscoveryHosts {
		pa, err := netip.ParseAddr(p.Addr)
		if err != nil {
			return overlay.NodeSpec{}, err
		}
		spec.Lighthouses = append(spec.Lighthouses, overlay.Peer{Addr: pa, Endpoints: p.Endpoints})
	}
	for _, r := range cfg.RelayAddrs {
		ra, err := netip.ParseAddr(r)
		if err != nil {
			return overlay.NodeSpec{}, err
		}
		spec.RelayedBy = append(spec.RelayedBy, ra)
	}
	return spec, spec.Validate()
}

func onlyConnectivity(groups []string) bool {
	for _, g := range groups {
		if g != pki.GroupConnectivityHost {
			return false
		}
	}
	return len(groups) > 0
}

// hostsOf is the hosts (without ports) of "host:port" endpoints.
func hostsOf(endpoints []string) []string {
	var out []string
	for _, e := range endpoints {
		if i := strings.LastIndex(e, ":"); i > 0 {
			out = append(out, strings.Trim(e[:i], "[]"))
		}
	}
	return out
}
