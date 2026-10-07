package localaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "local-access.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestAProgramIsGivenATokenOnceAndOnlyItsHashIsKept(t *testing.T) {
	s, path := open(t)
	e, token, err := s.Create("A program")
	if err != nil || !strings.HasPrefix(token, TokenPrefix) || e.Name != "A program" || e.ID == "" {
		t.Fatalf("%+v %q %v", e, token, err)
	}
	got, ok := s.Authenticate(token)
	if !ok || got.ID != e.ID {
		t.Fatalf("the token does not authenticate: %+v %v", got, ok)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), strings.TrimPrefix(token, TokenPrefix)) {
		t.Fatal("the token is on disk")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	// It survives a restart.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Authenticate(token); !ok {
		t.Fatal("the token did not survive a restart")
	}
}

func TestWhatIsNotAProgramsTokenDoesNotAuthenticate(t *testing.T) {
	s, _ := open(t)
	_, token, _ := s.Create("A")
	for _, bad := range []string{"", "wba_", TokenPrefix + strings.Repeat("0", 64), token + "x", strings.TrimPrefix(token, TokenPrefix), strings.Repeat("a", 500), "Bearer " + token} {
		if _, ok := s.Authenticate(bad); ok {
			t.Errorf("%q authenticated", bad)
		}
	}
}

func TestReconnectingReplacesAndRevokingIsImmediate(t *testing.T) {
	s, path := open(t)
	e1, t1, _ := s.Create("Team")
	e2, t2, err := s.Create("team") // the same program, by name: one token at a time
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(t1); ok {
		t.Fatal("the replaced token still works")
	}
	if _, ok := s.Authenticate(t2); !ok || e1.ID == e2.ID || len(s.List()) != 1 {
		t.Fatal("the new one does not work or both are listed")
	}
	if ok, err := s.Revoke("la_nothing"); ok || err != nil {
		t.Fatalf("revoking nothing: %v %v", ok, err)
	}
	if ok, err := s.Revoke(e2.ID); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, ok := s.Authenticate(t2); ok {
		t.Fatal("a revoked token works")
	}
	again, _ := Open(path)
	if len(again.List()) != 0 {
		t.Fatal("a revocation did not reach the disk")
	}
}

func TestNamesAreCheckedAndTheNumberOfProgramsIsBounded(t *testing.T) {
	s, _ := open(t)
	for _, bad := range []string{"", "   ", strings.Repeat("x", 61), "a\nb", "a\x00b"} {
		if _, _, err := s.Create(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for i := 0; i < MaxEntries; i++ {
		if _, _, err := s.Create("program " + string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Create("one more"); err != ErrTooMany {
		t.Fatalf("err = %v", err)
	}
}

func TestLastUseIsRecordedWithoutAWritePerRequest(t *testing.T) {
	s, path := open(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	_, token, _ := s.Create("A")
	if _, ok := s.Authenticate(token); !ok {
		t.Fatal()
	}
	st1, _ := os.Stat(path)
	now = now.Add(5 * time.Second)
	s.Authenticate(token)
	st2, _ := os.Stat(path)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("a use five seconds later was written to disk")
	}
	if l := s.List(); l[0].LastUsedAt == nil || !l[0].LastUsedAt.Equal(now) {
		t.Fatalf("last use = %+v", l[0].LastUsedAt)
	}
}

func TestAUnreadableFileIsNotQuietlyEmptied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local-access.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("a damaged file was treated as empty")
	}
}
