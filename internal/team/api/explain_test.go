package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid/localidentity"
	"devboard/internal/team/authproof"
	"devboard/internal/team/server"
	"devboard/internal/team/service"
	"devboard/internal/team/store"
)

// A person who is refused for a reason that is not their token must be told the reason: a member token used from another
// computer, a device whose clock disagrees, and a project code offered to a host by someone with no device.
func TestRefusalsSayWhyWhenTheReasonIsNotTheToken(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.New(db)
	created, err := svc.CreateWorkspace(ctx, "Why", "Owner", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := svc.Authenticate(ctx, created.Token)
	id, _ := localidentity.New("device", time.Now())
	dev, err := svc.RegisterDevice(ctx, owner, service.DeviceInput{Device: id.Public(), Proof: id.ProveRegistration(owner.Workspace.ID, owner.Member.ID)})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := svc.IssueLocalDeviceCredential(ctx, owner.Workspace.ID, dev.ID)
	h := server.Handler(db, svc, nil, "test")

	do := func(method, path, bearer, body string, sign time.Time) (int, string, string) {
		r := httptest.NewRequest(method, "http://10.128.0.1:7430"+path, strings.NewReader(body))
		r.RemoteAddr = "10.128.0.7:1234"
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if !sign.IsZero() {
			if err := authproof.Sign(r, []byte(body), id, owner.Workspace.ID, owner.Member.ID, sign); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var eb struct {
			Error struct{ Code, Message string }
		}
		_ = json.Unmarshal(bytes.TrimSpace(w.Body.Bytes()), &eb)
		return w.Code, eb.Error.Code, eb.Error.Message
	}

	if code, c, msg := do("GET", "/api/team/v1/me", created.Token, "", time.Time{}); code != 401 || c != "device_required" || !strings.Contains(msg, "invitation") {
		t.Errorf("a member token from another computer = %d %s %q", code, c, msg)
	}
	if code, c, msg := do("POST", "/api/team/v1/invites/redeem", "", `{"code":"wbi_x","name":"Bo"}`, time.Time{}); code != 403 || c != "device_required" || !strings.Contains(msg, "already in this workspace") {
		t.Errorf("a project code from someone with no device = %d %s %q", code, c, msg)
	}
	if code, c, _ := do("GET", "/api/team/v1/me", token, "", time.Now()); code != 200 {
		t.Errorf("a device with the right clock = %d %s", code, c)
	}
	code, c, msg := do("GET", "/api/team/v1/me", token, "", time.Now().Add(-10*time.Minute))
	if code != 401 || c != "clock_skew" || !strings.Contains(msg, "behind") || !strings.Contains(msg, "Date & Time") {
		t.Errorf("a device ten minutes behind = %d %s %q", code, c, msg)
	}
	if code, c, msg := do("GET", "/api/team/v1/me", token, "", time.Now().Add(10*time.Minute)); code != 401 || c != "clock_skew" || !strings.Contains(msg, "ahead of") {
		t.Errorf("a device ten minutes ahead = %d %s %q", code, c, msg)
	}
	// A device credential with no proof at all is still just refused.
	if code, c, _ := do("GET", "/api/team/v1/me", token, "", time.Time{}); code != 401 || c == "clock_skew" {
		t.Errorf("a device with no proof = %d %s", code, c)
	}
	_ = http.StatusOK
}
