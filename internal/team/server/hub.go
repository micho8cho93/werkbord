package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"devboard/internal/httpkit"
)

// A person can belong to several Team workspaces, and a computer can be a device in each of them. The device
// service keeps them apart: every workspace is a slot with a data directory, device identity, sealed keys, license,
// private network and local state of its own, run by a Daemon that knows nothing of the others. The Hub owns the one
// local listener and the registry of slots, and routes a request to the slot it names.
//
//	/w/<slot>/api/device/v1/…   that workspace's device API
//	/w/<slot>/api/team/v1/…     that workspace, through this device's own credential
//	/w/<slot>/                  that workspace's console
//	/api/device/v1/workspaces   the list of slots, creating one, removing an empty one
//	everything else             the first slot, "main", exactly as before there could be several
//
// "main" lives where the single workspace of an earlier installation lives (the data directory itself), so an
// installation upgrades without moving anything. Other slots live under slots/<id>.

// MainSlot is the first workspace on a computer, and the one an installation from before several were possible has.
const MainSlot = "main"

// MaxSlots bounds how many workspaces one computer holds.
const MaxSlots = 16

var slotIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

// slotRecord is one line of the registry (slots.json beside the data).
type slotRecord struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	// Retired: the workspace left this computer and an archive of it was kept, so the directory stays and no service
	// runs for it.
	Retired bool `json:"retired,omitempty"`
}

type slotRegistry struct {
	Version int          `json:"version"`
	Slots   []slotRecord `json:"slots"`
}

type hubSlot struct {
	id     string
	d      *Daemon
	h      http.Handler
	cancel context.CancelFunc
	done   chan struct{}
}

// Hub runs the device service for every workspace on this computer.
type Hub struct {
	o   DaemonOptions
	key string

	mu    sync.RWMutex
	ctx   context.Context
	slots map[string]*hubSlot
	reg   slotRegistry
	wg    sync.WaitGroup
	// hostDial substitutes the transport of every slot's host client (end-to-end tests in one process only).
	hostDial func(context.Context, string, string) (net.Conn, error)
}

// NewHub opens the registry and the device state of every workspace on this computer. Nothing runs until Run.
func NewHub(o DaemonOptions) (*Hub, error) {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.LocalAddr == "" {
		o.LocalAddr = "127.0.0.1:7431"
	}
	if host, _, err := net.SplitHostPort(o.LocalAddr); err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("the device service must listen on a literal loopback address")
	}
	h := &Hub{o: o, slots: map[string]*hubSlot{}}
	main, err := h.makeSlot(MainSlot)
	if err != nil {
		return nil, err
	}
	// One credential for the window, whichever workspace it is looking at.
	h.key = main.d.key()
	h.o.LocalKey = h.key
	h.slots[MainSlot] = main
	reg, err := h.loadRegistry()
	if err != nil {
		return nil, err
	}
	h.reg = reg
	for _, rec := range reg.Slots {
		if rec.Retired || rec.ID == MainSlot {
			continue
		}
		s, err := h.makeSlot(rec.ID)
		if err != nil {
			return nil, fmt.Errorf("workspace %s: %w", rec.ID, err)
		}
		h.slots[rec.ID] = s
	}
	return h, nil
}

func (h *Hub) slotDir(id string) string {
	if id == MainSlot {
		return h.o.Config.DataDir
	}
	return filepath.Join(h.o.Config.DataDir, "slots", id)
}

func (h *Hub) makeSlot(id string) (*hubSlot, error) {
	so := h.o
	so.Slot = id
	so.Config.DataDir = h.slotDir(id)
	so.LocalKey = h.o.LocalKey
	if err := os.MkdirAll(so.Config.DataDir, 0o700); err != nil {
		return nil, err
	}
	so.HostingGuard = func() error { return h.hostingGuard(id) }
	so.NetworkGuard = func(p netip.Prefix) error { return h.networkGuard(id, p) }
	d, err := NewDaemon(so)
	if err != nil {
		return nil, err
	}
	d.hostDial = h.hostDial
	return &hubSlot{id: id, d: d, h: d.Handler()}, nil
}

func (h *Hub) registryPath() string { return filepath.Join(h.o.Config.DataDir, "slots.json") }

func (h *Hub) loadRegistry() (slotRegistry, error) {
	var r slotRegistry
	b, err := os.ReadFile(h.registryPath())
	if errors.Is(err, os.ErrNotExist) {
		return slotRegistry{Version: 1}, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 {
		return r, errors.New("the list of workspaces on this computer is unreadable; it is preserved")
	}
	seen := map[string]bool{}
	for _, s := range r.Slots {
		if !slotIDPattern.MatchString(s.ID) || seen[s.ID] {
			return r, errors.New("the list of workspaces on this computer is unreadable; it is preserved")
		}
		seen[s.ID] = true
	}
	return r, nil
}

func (h *Hub) saveRegistry() error {
	b, err := json.MarshalIndent(h.reg, "", "  ")
	if err != nil {
		return err
	}
	return writeDaemonFile(h.registryPath(), b)
}

// holdsHostRole: the workspace of this slot is, or is becoming, a Workspace Host here.
func (s *hubSlot) holdsHostRole() bool {
	s.d.mu.RLock()
	defer s.d.mu.RUnlock()
	return (s.d.mat != nil && s.d.mat.Meta.Authority) || s.d.operation == "Creating your workspace"
}

func (h *Hub) hostingGuard(self string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, s := range h.slots {
		if id != self && s.holdsHostRole() {
			return errHostingBlocked
		}
	}
	return nil
}

// networkGuard refuses a private network that overlaps one this computer is already on: two workspaces could not
// both be routed.
func (h *Hub) networkGuard(self string, p netip.Prefix) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, s := range h.slots {
		if id == self {
			continue
		}
		s.d.mu.RLock()
		var other netip.Prefix
		if s.d.mat != nil {
			other, _ = netip.ParsePrefix(s.d.mat.Meta.NetworkPrefix)
		}
		s.d.mu.RUnlock()
		if other.IsValid() && other.Overlaps(p) {
			return errors.New("this workspace's private network overlaps one of your other Team workspaces on this computer, so both could not be reached at once; ask its administrator to give it a different network range")
		}
	}
	return nil
}

func (h *Hub) startSlot(s *hubSlot) {
	h.mu.RLock()
	parent := h.ctx
	h.mu.RUnlock()
	if parent == nil || parent.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel, s.done = cancel, make(chan struct{})
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer close(s.done)
		s.d.RunDevice(ctx)
	}()
}

// Run serves the one local address and keeps every workspace's infrastructure alive until ctx ends. No window owns it.
func (h *Hub) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	h.mu.Lock()
	h.ctx = runCtx
	slots := make([]*hubSlot, 0, len(h.slots))
	for _, s := range h.slots {
		slots = append(slots, s)
	}
	h.mu.Unlock()
	ln, err := net.Listen("tcp", h.o.LocalAddr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h.Handler(), BaseContext: func(net.Listener) context.Context { return runCtx }, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: time.Minute}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	for _, s := range slots {
		h.startSlot(s)
	}
	h.o.Log.Info("Team device service is running", "version", h.o.Version, "addr", ln.Addr().String(), "workspaces", len(slots))
	select {
	case err = <-done:
		cancel()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case <-ctx.Done():
		cancel()
		err = nil
	}
	stop, end := context.WithTimeout(context.Background(), 10*time.Second)
	defer end()
	_ = srv.Shutdown(stop)
	h.wg.Wait()
	return err
}

func (h *Hub) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && len(r.Header.Values("Authorization")) == 1 && subtle.ConstantTimeCompare([]byte(token), []byte(h.key)) == 1
}

// Handler routes to the workspace a path names. Only loopback serves it.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/device/v1/workspaces", h.secured(h.listSlots))
	mux.HandleFunc("POST /api/device/v1/workspaces", h.secured(h.addSlot))
	mux.HandleFunc("DELETE /api/device/v1/workspaces/{id}", h.secured(h.removeSlot))
	mux.HandleFunc("/w/{slot}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/w/"+r.PathValue("slot")+"/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/w/{slot}/", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("slot")
		h.mu.RLock()
		s := h.slots[id]
		h.mu.RUnlock()
		if s == nil {
			httpkit.WriteError(w, http.StatusNotFound, "not_found", "no such workspace on this computer")
			return
		}
		http.StripPrefix("/w/"+id, s.h).ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		s := h.slots[MainSlot]
		h.mu.RUnlock()
		s.h.ServeHTTP(w, r)
	})
	return mux
}

func (h *Hub) secured(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !h.authorized(r) {
			httpkit.WriteError(w, http.StatusUnauthorized, "unauthorized", "open Werkbord Team to connect to this device")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if err := sameHostOrigin(origin, r.Host); err != nil {
				httpkit.WriteError(w, http.StatusForbidden, "forbidden_origin", "open Werkbord Team on this computer")
				return
			}
		}
		next(w, r)
	}
}

// SlotSummary is one workspace as the list shows it: enough to choose it, and to say what it is doing.
type SlotSummary struct {
	Slot      string `json:"slot"`
	Enrolled  bool   `json:"enrolled"`
	Pending   bool   `json:"pending,omitempty"`
	Leaving   bool   `json:"leaving,omitempty"`
	Operation string `json:"operation,omitempty"`
	Error     string `json:"error,omitempty"`
	Workspace *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"workspace,omitempty"`
	DeviceID       string `json:"deviceId,omitempty"`
	Role           string `json:"role,omitempty"`
	WorkspaceHost  bool   `json:"workspaceHost,omitempty"`
	Connected      bool   `json:"connected"`
	RunnerOnline   bool   `json:"runnerConnected"`
	HostingBlocked string `json:"hostingBlocked,omitempty"`
	License        bool   `json:"licensed"`
	Archived       bool   `json:"archived,omitempty"`
}

func (h *Hub) summaries() []SlotSummary {
	h.mu.RLock()
	ids := make([]string, 0, len(h.slots))
	byID := make(map[string]*hubSlot, len(h.slots))
	for id, s := range h.slots {
		ids = append(ids, id)
		byID[id] = s
	}
	h.mu.RUnlock()
	sort.Slice(ids, func(i, j int) bool {
		if ids[i] == MainSlot || ids[j] == MainSlot {
			return ids[i] == MainSlot
		}
		return ids[i] < ids[j]
	})
	out := make([]SlotSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, summarize(id, byID[id].d.stateView()))
	}
	return out
}

func summarize(id string, v map[string]any) SlotSummary {
	s := SlotSummary{Slot: id}
	s.Enrolled, _ = v["enrolled"].(bool)
	s.Pending, _ = v["pending"].(bool)
	s.Leaving, _ = v["leaving"].(bool)
	s.Operation, _ = v["operation"].(string)
	s.Error, _ = v["error"].(string)
	s.DeviceID, _ = v["deviceId"].(string)
	s.Role, _ = v["role"].(string)
	s.WorkspaceHost, _ = v["workspaceHost"].(bool)
	s.Connected, _ = v["connected"].(bool)
	s.HostingBlocked, _ = v["hostingBlocked"].(string)
	_, s.License = v["license"]
	if r, ok := v["runner"].(map[string]any); ok {
		s.RunnerOnline, _ = r["connected"].(bool)
	}
	if w, ok := v["workspace"].(map[string]any); ok {
		s.Workspace = &struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{}
		s.Workspace.ID, _ = w["id"].(string)
		s.Workspace.Name, _ = w["name"].(string)
	}
	return s
}

func (h *Hub) listSlots(w http.ResponseWriter, r *http.Request) {
	httpkit.WriteJSON(w, http.StatusOK, map[string]any{"version": h.o.Version, "max": MaxSlots, "workspaces": h.summaries()})
}

// empty: nothing is on this slot, so a new workspace can be created or joined in it.
func (s *hubSlot) empty() bool {
	occ := s.d.occupancy()
	s.d.mu.RLock()
	op := s.d.operation
	s.d.mu.RUnlock()
	return !occ.Enrolled && !occ.Pending && !occ.Leaving && op == ""
}

// addSlot gives the caller a slot to create or join a workspace in: an empty one that already exists, else a new one.
// It is idempotent for the person who opens "add a Team" twice.
func (h *Hub) addSlot(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	ids := make([]string, 0, len(h.slots))
	for id := range h.slots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range append([]string{MainSlot}, ids...) {
		if s := h.slots[id]; s != nil && s.empty() {
			h.mu.Unlock()
			httpkit.WriteJSON(w, http.StatusOK, map[string]any{"slot": id, "created": false})
			return
		}
	}
	if len(h.slots) >= MaxSlots {
		h.mu.Unlock()
		httpkit.WriteError(w, http.StatusConflict, "too_many", fmt.Sprintf("this computer can belong to at most %d Team workspaces; leave one first", MaxSlots))
		return
	}
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		h.mu.Unlock()
		httpkit.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	id := "ws_" + hex.EncodeToString(raw[:])
	s, err := h.makeSlot(id)
	if err == nil {
		h.reg.Slots = append(h.reg.Slots, slotRecord{ID: id, CreatedAt: time.Now().UTC()})
		if err = h.saveRegistry(); err != nil {
			h.reg.Slots = h.reg.Slots[:len(h.reg.Slots)-1]
		}
	}
	if err != nil {
		h.mu.Unlock()
		httpkit.WriteError(w, http.StatusConflict, "device", err.Error())
		return
	}
	h.slots[id] = s
	h.mu.Unlock()
	h.startSlot(s)
	httpkit.WriteJSON(w, http.StatusCreated, map[string]any{"slot": id, "created": true})
}

// removeSlot forgets a workspace that is no longer on this computer: one left safely, or one that never got further than
// the setup screen. A workspace that is still enrolled is never removed here; leaving it (which asks the workspace to
// agree it is safe) comes first, and an archive that person chose to keep stays on disk.
func (h *Hub) removeSlot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == MainSlot {
		httpkit.WriteError(w, http.StatusConflict, "device", "the first workspace slot stays; leave its workspace to empty it")
		return
	}
	h.mu.Lock()
	s := h.slots[id]
	if s == nil {
		h.mu.Unlock()
		httpkit.WriteError(w, http.StatusNotFound, "not_found", "no such workspace on this computer")
		return
	}
	if !s.empty() {
		h.mu.Unlock()
		httpkit.WriteError(w, http.StatusConflict, "device", "leave this workspace safely before removing it from this computer")
		return
	}
	delete(h.slots, id)
	h.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	dir := h.slotDir(id)
	keep := hasRetainedData(dir)
	h.mu.Lock()
	next := h.reg.Slots[:0:0]
	for _, rec := range h.reg.Slots {
		if rec.ID != id {
			next = append(next, rec)
		} else if keep {
			rec.Retired = true
			next = append(next, rec)
		}
	}
	h.reg.Slots = next
	err := h.saveRegistry()
	h.mu.Unlock()
	if err == nil && !keep {
		err = os.RemoveAll(dir)
	}
	if err != nil {
		httpkit.WriteError(w, http.StatusConflict, "device", err.Error())
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, map[string]any{"removed": true, "archiveKept": keep})
}

// hasRetainedData: the person chose to keep an archive or backups of this workspace, which removing the slot must not delete.
func hasRetainedData(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "left-workspace-") || strings.HasPrefix(name, "retired-storage-") {
			return true
		}
		if name == "backups" {
			if sub, err := os.ReadDir(filepath.Join(dir, name)); err == nil && len(sub) > 0 {
				return true
			}
		}
	}
	return false
}
