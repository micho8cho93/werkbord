package api

import (
	"encoding/base64"
	"net/http"
	"time"

	"devboard/internal/httpkit"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
	qrcode "github.com/skip2/go-qrcode"
)

// The private network and the devices on it. Everything here is metadata the workspace
// keeps about its own machines: addresses, roles, reachability, invitations, requests
// to join. A device's certificate is returned to that device alone, authenticated by
// its own credential; the workspace's signing keys are never in any response.

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	ds, err := s.opt.Service.ListDevices(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, ds)
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	d, err := s.opt.Service.RevokeDevice(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, d)
}

func (s *Server) handleSetDeviceNetwork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BootstrapEndpoints *[]string `json:"bootstrapEndpoints"`
		NetworkEndpoints   *[]string `json:"networkEndpoints"`
		Discovery          *bool     `json:"discovery"`
		Relay              *bool     `json:"relay"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	n, err := s.opt.Service.SetDeviceNetwork(r.Context(), actorOf(r), r.PathValue("id"),
		service.NetworkPatch{BootstrapEndpoints: in.BootstrapEndpoints, NetworkEndpoints: in.NetworkEndpoints, Discovery: in.Discovery, Relay: in.Relay})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, n)
}

func (s *Server) handleDeviceNetwork(w http.ResponseWriter, r *http.Request) {
	n, err := s.opt.Service.DeviceNetwork(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, n)
}

func (s *Server) handleProvisionDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.ProvisionHost(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNetworkHealth(w http.ResponseWriter, r *http.Request) {
	h, err := s.opt.Service.NetworkHealth(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := struct {
		domain.NetworkHealth
		// Node is this host's own network node (the one that answered), which the
		// workspace's records cannot know.
		Node any `json:"node,omitempty"`
	}{NetworkHealth: h}
	if s.opt.NodeStatus != nil {
		out.Node = s.opt.NodeStatus()
	}
	httpkit.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetApproval(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approval domain.ApprovalPolicy `json:"approval"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.opt.Service.SetEnrollmentApproval(r.Context(), actorOf(r), in.Approval); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNetworkConfig(w http.ResponseWriter, r *http.Request) {
	b, err := s.opt.Service.DeviceNetworkConfig(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, b.Wire())
}

func (s *Server) handleRenewCertificate(w http.ResponseWriter, r *http.Request) {
	b, err := s.opt.Service.RenewCertificate(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, b.Wire())
}

func (s *Server) handleNetworkCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"deviceId"`
		Endpoint string `json:"endpoint"`
		OK       bool   `json:"ok"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.opt.Service.RecordReachability(r.Context(), actorOf(r), in.DeviceID, service.ReachReport{Endpoint: in.Endpoint, OK: in.OK}); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCollectProvision hands a device what was sealed to it. It is ciphertext, and
// only the device that holds the matching private key can read it.
func (s *Server) handleCollectProvision(w http.ResponseWriter, r *http.Request) {
	b, err := s.opt.Service.CollectProvision(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, map[string]string{"sealed": base64.StdEncoding.EncodeToString(b)})
}

func (s *Server) handleAckProvision(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.AcknowledgeProvision(r.Context(), actorOf(r)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListEnrollInvitations(w http.ResponseWriter, r *http.Request) {
	l, err := s.opt.Service.ListEnrollInvitations(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, l)
}

func (s *Server) handleCreateEnrollInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label           string   `json:"label"`
		ForMemberID     string   `json:"forMemberId"`
		Role            string   `json:"role"`
		Capabilities    []string `json:"capabilities"`
		ExpiresInHours  float64  `json:"expiresInHours"`
		RequireApproval bool     `json:"requireApproval"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	caps := make([]domain.Capability, 0, len(in.Capabilities))
	for _, c := range in.Capabilities {
		caps = append(caps, domain.Capability(c))
	}
	res, err := s.opt.Service.CreateEnrollInvitation(r.Context(), actorOf(r), service.EnrollInviteInput{Label: in.Label, ForMemberID: in.ForMemberID,
		Role: domain.Role(in.Role), Capabilities: caps, TTL: time.Duration(in.ExpiresInHours * float64(time.Hour)), RequireApproval: in.RequireApproval})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	png, _ := qrcode.Encode(res.Link, qrcode.Medium, 384)
	var qr string
	if len(png) != 0 {
		qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	httpkit.WriteJSON(w, http.StatusCreated, struct {
		service.EnrollInviteResult
		QR string `json:"qr,omitempty"`
	}{res, qr})
}

func (s *Server) handleWithdrawEnrollInvitation(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.WithdrawEnrollInvitation(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListEnrollments(w http.ResponseWriter, r *http.Request) {
	state := domain.EnrollmentState(r.URL.Query().Get("state"))
	if state == "" {
		state = domain.EnrollmentPending
	}
	l, err := s.opt.Service.ListEnrollments(r.Context(), actorOf(r), state)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, l)
}

func (s *Server) handleApproveEnrollment(w http.ResponseWriter, r *http.Request) {
	e, err := s.opt.Service.ApproveEnrollment(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, e)
}

func (s *Server) handleDenyEnrollment(w http.ResponseWriter, r *http.Request) {
	e, err := s.opt.Service.DenyEnrollment(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, e)
}

// handleSetDeviceCapabilities changes what a device does for the workspace: its owner may add or remove the runner capability, and
// someone who manages devices may give or take a host capability.
func (s *Server) handleSetDeviceCapabilities(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Capabilities []string `json:"capabilities"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	caps := make([]domain.Capability, 0, len(in.Capabilities))
	for _, c := range in.Capabilities {
		caps = append(caps, domain.Capability(c))
	}
	d, err := s.opt.Service.SetDeviceCapabilities(r.Context(), actorOf(r), r.PathValue("id"), caps)
	s.respond(w, r, http.StatusOK, d, err)
}

func (s *Server) handleRenameDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	d, err := s.opt.Service.RenameDevice(r.Context(), actorOf(r), r.PathValue("id"), in.Name)
	s.respond(w, r, http.StatusOK, d, err)
}
