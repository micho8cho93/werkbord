package clientaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPairingExpiryReplayPersistenceAndIndependentRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	code, _, err := s.NewCode()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(CodeTTL)
	if _, _, err = s.Enroll(code, "Laptop", strings.Repeat("a", 64)); err != ErrCode {
		t.Fatalf("expiry: %v", err)
	}
	code, _, _ = s.NewCode()
	entry, token, err := s.Enroll(code, "Laptop", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Enroll(code, "Replay", strings.Repeat("b", 64)); err != ErrCode {
		t.Fatal("code reused")
	}
	for _, bad := range []string{"", strings.Replace(token, "wbc_", "wbp_", 1), "Bearer " + token, token + "a"} {
		if _, ok := s.Authenticate(bad); ok {
			t.Fatal("wrong credential accepted")
		}
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), code) {
		t.Fatal("plaintext secret persisted")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("grant file is not private")
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Authenticate(token); !ok {
		t.Fatal("credential did not survive restart")
	}
	watch, stop := s.Watch(entry.ID)
	defer stop()
	code, _, _ = s.NewCode()
	_, other, err := s.Enroll(code, "Other", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(entry.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watch:
	default:
		t.Fatal("open request not revoked")
	}
	if _, ok := s.Authenticate(token); ok {
		t.Fatal("revoked token accepted")
	}
	if _, ok := s.Authenticate(other); !ok {
		t.Fatal("another grant revoked")
	}
	gone, cleanup := s.Watch(entry.ID)
	defer cleanup()
	select {
	case <-gone:
	default:
		t.Fatal("revocation race")
	}
}
