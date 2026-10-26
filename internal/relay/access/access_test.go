package access

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"werkbord/internal/relay/e2e"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "relay.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func idOf(t *testing.T, secret string) e2e.PSKID {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return e2e.IDOf(raw)
}

func TestTheIdentityAndRoomSurviveARestartAndTheFileIsPrivate(t *testing.T) {
	s, path := open(t)
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Room() != s.Room() || string(again.Identity().Public()) != string(s.Identity().Public()) {
		t.Fatal("a restart changed who this computer is: every phone would have to scan again")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("relay.json is %v: it holds keys and must be private", info.Mode().Perm())
	}
}

func TestAPendingSecretWorksForFiveMinutesAndThenNotAtAll(t *testing.T) {
	s, _ := open(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	p, err := s.NewPairing("https://relay.example", "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "the-code" || p.Room != s.Room().ID {
		t.Fatalf("pairing = %+v", p)
	}
	id := idOf(t, p.Secret)
	if _, ok := s.Lookup(id); !ok {
		t.Fatal("a fresh code does not work")
	}
	now = now.Add(PendingTTL - time.Second)
	if _, ok := s.Lookup(id); !ok {
		t.Fatal("a code just inside its time does not work")
	}
	now = now.Add(2 * time.Second)
	if _, ok := s.Lookup(id); ok {
		t.Fatal("a code past its time still opens a session")
	}
	if err := s.Bind(id, "ph_1"); err == nil {
		t.Fatal("an expired code was bound to a phone")
	}
}

func TestBindingKeepsASecretUntilItsPhoneIsRevokedAndSurvivesARestart(t *testing.T) {
	s, path := open(t)
	p, _ := s.NewPairing("https://relay.example", "c")
	id := idOf(t, p.Secret)
	if err := s.Bind(id, "ph_1"); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if _, ok := s.Lookup(id); !ok {
		t.Fatal("an enrolled phone cannot come back the next day")
	}
	again, _ := Open(path)
	if _, ok := again.Lookup(id); !ok {
		t.Fatal("an enrolled phone cannot come back after a restart")
	}
	if gone := again.Unbind("ph_other"); len(gone) != 0 {
		t.Fatal("another phone's revocation took this one's secret")
	}
	if gone := again.Unbind("ph_1"); len(gone) != 1 || gone[0] != id {
		t.Fatalf("unbind = %v", gone)
	}
	if _, ok := again.Lookup(id); ok {
		t.Fatal("a revoked phone's secret still works")
	}
	third, _ := Open(path)
	if _, ok := third.Lookup(id); ok {
		t.Fatal("a revoked phone's secret came back after a restart")
	}
}

func TestPruneForgetsPhonesThatNoLongerExist(t *testing.T) {
	s, path := open(t)
	a, _ := s.NewPairing("https://relay.example", "")
	b, _ := s.NewPairing("https://relay.example", "")
	_ = s.Bind(idOf(t, a.Secret), "ph_a")
	_ = s.Bind(idOf(t, b.Secret), "ph_b")
	s.Prune(func(id string) bool { return id == "ph_a" })
	again, _ := Open(path)
	if _, ok := again.Lookup(idOf(t, a.Secret)); !ok {
		t.Fatal("a phone that exists lost its secret")
	}
	if _, ok := again.Lookup(idOf(t, b.Secret)); ok {
		t.Fatal("a phone that was revoked while the controller was off kept its secret")
	}
}

func TestCodesWaitingAreBounded(t *testing.T) {
	s, _ := open(t)
	for i := 0; i < maxPending; i++ {
		if _, err := s.NewPairing("https://relay.example", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.NewPairing("https://relay.example", ""); err == nil {
		t.Fatal("unbounded codes")
	}
}

func TestCheckedRevocationReportsSaveFailureAndRetryPersistsIt(t *testing.T) {
	s, path := open(t)
	pairing, err := s.NewPairing("https://relay.company.example", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, pairing.Secret)
	if err = s.Bind(id, "client"); err != nil {
		t.Fatal(err)
	}
	original := s.path
	s.path = filepath.Join(path, "not-a-directory")
	if gone, err := s.Revoke("client"); err == nil || len(gone) != 1 {
		t.Fatal("failed persistence was reported as success")
	}
	if _, ok := s.Lookup(id); ok {
		t.Fatal("failed save kept session access")
	}
	s.path = original
	if _, err = s.Revoke("client"); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Lookup(id); ok {
		t.Fatal("retry resurrected a revoked grant")
	}
}
