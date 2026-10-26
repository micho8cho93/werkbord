package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"werkbord/internal/httpkit"
	"werkbord/internal/relay/access"
	relayclient "werkbord/internal/relay/client"
	"werkbord/internal/relay/e2e"
	"werkbord/internal/relay/tunnel"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/phone"
)

const phoneBlocked = "Ask your admin to turn on phone access"

type phoneLane struct {
	mu     sync.Mutex
	store  *access.Store
	client *relayclient.Client
	tunnel *tunnel.Server
}

func (d *Daemon) openPhone() error {
	if d.o.Config.PhoneRelayEndpoint == "" {
		return nil
	}
	store, err := access.Open(filepath.Join(d.o.Config.DataDir, "local", "phone-relay.json"))
	if err != nil {
		return err
	}
	p := &phoneLane{store: store}
	p.tunnel = &tunnel.Server{Log: d.o.Log, Handler: http.HandlerFunc(d.servePhone)}
	p.client = relayclient.New(relayclient.Config{BaseURL: d.o.Config.PhoneRelayEndpoint, Room: store.Room(), Identity: store.Identity(), Secrets: store, Handler: p.tunnel})
	p.tunnel.Sender = p.client
	d.phone = p
	return nil
}

func (d *Daemon) phoneStatus(w http.ResponseWriter, r *http.Request) {
	if d.phone == nil {
		httpkit.WriteJSON(w, 200, map[string]any{"enabled": false, "message": phoneBlocked})
		return
	}
	httpkit.WriteJSON(w, 200, map[string]any{"enabled": true, "connected": d.phone.client.Status().Connected, "grants": d.phone.store.Bindings()})
}

// connectionReport counts only this member's current-workspace grants and authenticated
// tunnel peers. A relay connection alone says nothing about a phone being connected.
func (d *Daemon) connectionReport(runnerConnected bool) *domain.DeviceConnections {
	c := &domain.DeviceConnections{RunnerConnected: runnerConnected}
	p, scope := d.phone, d.phoneScope()
	if p == nil || scope == "" {
		return c
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, binding := range p.store.Bindings() {
		raw, ok := strings.CutPrefix(binding, scope+"-")
		if !ok {
			continue
		}
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) != len(e2e.PSKID{}) {
			continue
		}
		var secret e2e.PSKID
		copy(secret[:], decoded)
		c.PhonesPaired++
		if len(p.tunnel.PeersOf(secret)) > 0 {
			c.PhonesConnected++
		}
	}
	return c
}

// Only the authenticated local device API can issue a phone grant. Host credentials never cross the tunnel.
func (d *Daemon) phonePair(w http.ResponseWriter, r *http.Request) {
	p := d.phone
	if p == nil {
		httpkit.WriteError(w, 409, "phone_disabled", phoneBlocked)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.store.Phones() >= 16 {
		httpkit.WriteError(w, 409, "phone_limit", "Revoke a phone before adding another")
		return
	}
	if d.phoneScope() == "" || d.summaryHeader().State != "ready" {
		httpkit.WriteError(w, 409, "workspace_offline", "Connect this member computer to its workspace first")
		return
	}
	pairing, err := p.store.NewPairing(d.o.Config.PhoneRelayEndpoint, "")
	if err != nil {
		daemonFail(w, err)
		return
	}
	raw, _ := base64.RawURLEncoding.DecodeString(pairing.Secret)
	secret := e2e.IDOf(raw)
	id := d.phoneScope() + "-" + hex.EncodeToString(secret[:])
	if err = p.store.Bind(secret, id); err != nil {
		daemonFail(w, err)
		return
	}
	pairing.Lane = "team"
	httpkit.WriteJSON(w, 201, map[string]string{"id": id, "url": strings.TrimSuffix(d.o.Config.PhoneConnectorEndpoint, "/") + "/connect/#" + pairing.Encode()})
}

func (d *Daemon) phoneRevoke(w http.ResponseWriter, r *http.Request) {
	if p := d.phone; p != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		gone, err := p.store.Revoke(r.PathValue("id"))
		for _, secret := range gone {
			for _, peer := range p.tunnel.PeersOf(secret) {
				p.client.Kick(r.Context(), peer)
			}
		}
		if err != nil {
			httpkit.WriteError(w, 500, "revocation_unsaved", "Access ended for this session, but revocation could not be saved. Retry before restarting this computer.")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// servePhone is deliberately independent of Handler: no device API, workspace proxy or execution route is reachable.
// Every request checks the proved secret again, including on a connection opened before revocation.
func (d *Daemon) servePhone(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	secret, ok := tunnel.SecretFrom(r.Context())
	if !ok || d.phone == nil {
		httpkit.WriteError(w, 401, "phone_grant", "Pair this phone on your member computer")
		return
	}
	bound, boundOK := d.phone.store.BoundClient(secret)
	scope := d.phoneScope()
	if _, ok = d.phone.store.Lookup(secret); !ok || !boundOK || scope == "" || !strings.HasPrefix(bound, scope+"-") {
		httpkit.WriteError(w, 401, "phone_grant", "This phone was signed out")
		return
	}
	if r.Method != http.MethodGet {
		httpkit.WriteError(w, 403, "read_only", "Team phone access is read-only")
		return
	}
	switch r.URL.Path {
	case "/api/summary":
		ctx, cancel := context.WithTimeout(r.Context(), phone.SummaryTimeout)
		defer cancel()
		httpkit.WriteJSON(w, 200, d.gatherSummary(ctx))
	case "/", "/phone.js", "/phone.css":
		phone.Handler().ServeHTTP(w, r)
	default:
		httpkit.WriteError(w, 403, "read_only", "This endpoint is not available to phones")
	}
}

// A grant is pinned to the member and workspace at issuance, even if this slot is later reused.
func (d *Daemon) phoneScope() string {
	occ := d.occupancy()
	if !occ.Enrolled || occ.Leaving {
		return ""
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.mat == nil || d.memberID == "" {
		return ""
	}
	h := sha256.Sum256([]byte(d.mat.Meta.WorkspaceID + "\x00" + d.memberID))
	return hex.EncodeToString(h[:])
}
