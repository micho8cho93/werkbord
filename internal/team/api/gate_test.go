package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/deviceid/localidentity"
	"devboard/internal/team/authproof"
	"devboard/internal/team/server"
	"devboard/internal/team/service"
	"devboard/internal/team/store"
)

func TestRemoteAuthenticationRequiresDeviceProofAndReplaySurvivesHandlerRestart(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.New(db)
	created, err := svc.CreateWorkspace(ctx, "Gate", "Owner", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := svc.Authenticate(ctx, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	id, err := localidentity.New("device", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dev, err := svc.RegisterDevice(ctx, owner, service.DeviceInput{Device: id.Public(), Proof: id.ProveRegistration(owner.Workspace.ID, owner.Member.ID)})
	if err != nil {
		t.Fatal(err)
	}
	token, err := svc.IssueLocalDeviceCredential(ctx, owner.Workspace.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := server.Handler(db, svc, nil, "test")
	url := "http://10.128.0.1:7430/api/team/v1/projects"
	body := []byte(`{"name":"One"}`)
	req := func() *http.Request {
		r := httptest.NewRequest("POST", url, bytes.NewReader(body))
		r.RemoteAddr = "10.128.0.7:1234"
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}
	status := func(h http.Handler, r *http.Request) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := status(h, req()); code != 401 {
		t.Fatal("bare device credential crossed network", code)
	}
	memberReq := req()
	memberReq.Header.Set("Authorization", "Bearer "+created.Token)
	if code := status(h, memberReq); code != 401 {
		t.Fatal("member bearer crossed network", code)
	}
	signed := req()
	if err := authproof.Sign(signed, body, id, owner.Workspace.ID, owner.Member.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if code := status(h, signed); code != 201 {
		t.Fatal("valid signed mutation failed", code)
	}
	replay := req()
	replay.Header = signed.Header.Clone()
	h = server.Handler(db, service.New(db), nil, "restart")
	if code := status(h, replay); code != 401 {
		t.Fatal("replayed mutation survived restart", code)
	}
	changed := req()
	_ = authproof.Sign(changed, body, id, owner.Workspace.ID, owner.Member.ID, time.Now())
	changed.Body = http.NoBody
	if code := status(h, changed); code != 401 {
		t.Fatal("changed signed body accepted", code)
	}
	for _, headers := range [][]string{{created.Token}, {"Bearer " + created.Token, "Bearer " + created.Token}} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:7430/api/team/v1/me", nil)
		r.RemoteAddr = "127.0.0.1:2000"
		for _, v := range headers {
			r.Header.Add("Authorization", v)
		}
		if code := status(h, r); code != 401 {
			t.Fatal("ambiguous authorization accepted", code)
		}
	}
}

func FuzzRemoteBearerCannotBecomeDeviceAuthentication(f *testing.F) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(f.TempDir(), "team.db"), nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = db.Close() })
	svc := service.New(db)
	created, err := svc.CreateWorkspace(ctx, "Fuzz", "Owner", "")
	if err != nil {
		f.Fatal(err)
	}
	h := server.Handler(db, svc, nil, "test")
	f.Add("Bearer "+created.Token, "")
	f.Add(created.Token, "e30")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, authorization, proof string) {
		if len(authorization) > 4096 || len(proof) > 8192 {
			return
		}
		r := httptest.NewRequest("GET", "http://10.128.0.1:7430/api/team/v1/me", nil)
		r.RemoteAddr = "10.128.0.9:9876"
		r.Header.Set("Authorization", authorization)
		r.Header.Set(authproof.Header, proof)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("remote member credential bypassed device authentication: %d", w.Code)
		}
	})
}
