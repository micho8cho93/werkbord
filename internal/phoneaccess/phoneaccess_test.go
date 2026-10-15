package phoneaccess

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "phones.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func enroll(t *testing.T, s *Store, name string) (Entry, string) {
	t.Helper()
	code, _, err := s.NewCode()
	if err != nil {
		t.Fatal(err)
	}
	e, token, err := s.Enroll(code, name)
	if err != nil {
		t.Fatal(err)
	}
	return e, token
}

func TestAPhoneIsGivenATokenOnceAndOnlyItsHashIsKept(t *testing.T) {
	s, path := open(t)
	code, _, _ := s.NewCode()
	e, token, err := s.Enroll(code, "My phone")
	if err != nil || !strings.HasPrefix(token, TokenPrefix) || e.Name != "My phone" || !strings.HasPrefix(e.ID, "ph_") {
		t.Fatalf("%+v %q %v", e, token, err)
	}
	if got, ok := s.Authenticate(token); !ok || got.ID != e.ID {
		t.Fatalf("the token does not authenticate: %+v %v", got, ok)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{token, strings.TrimPrefix(token, TokenPrefix), code} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("a secret is on disk: %s", secret)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Authenticate(token); !ok {
		t.Fatal("the token did not survive a restart")
	}
}

func TestWhatIsNotAPhonesTokenDoesNotAuthenticate(t *testing.T) {
	s, _ := open(t)
	_, token := enroll(t, s, "A")
	for _, bad := range []string{"", "wbp_", TokenPrefix + strings.Repeat("0", 64), token + "x", strings.TrimPrefix(token, TokenPrefix), strings.Repeat("a", 500), "Bearer " + token, "wba_" + strings.TrimPrefix(token, TokenPrefix)} {
		if _, ok := s.Authenticate(bad); ok {
			t.Errorf("%q authenticated", bad)
		}
	}
}

func TestACodeHasAtLeast192BitsAndIsNotReused(t *testing.T) {
	s, _ := open(t)
	seen := map[string]bool{}
	for i := 0; i < MaxPendingCodes; i++ {
		code, _, err := s.NewCode()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(code)
		if err != nil || len(raw)*8 < 192 {
			t.Fatalf("code %q has %d bits (%v)", code, len(raw)*8, err)
		}
		if seen[code] {
			t.Fatal("a code was issued twice")
		}
		seen[code] = true
	}
}

func TestACodeWorksOnceAndOnlyBefore5Minutes(t *testing.T) {
	s, _ := open(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	code, expires, _ := s.NewCode()
	if !expires.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("expires %v", expires)
	}
	if _, _, err := s.Enroll(code, "Phone"); err != nil {
		t.Fatal(err)
	}
	// A replay of a spent code is refused, the same way as any other bad code.
	_, _, replay := s.Enroll(code, "Phone again")
	_, _, wrong := s.Enroll("not-a-code", "Phone")
	if replay != ErrInvalidCode || wrong != ErrInvalidCode {
		t.Fatalf("replay %v, wrong %v", replay, wrong)
	}
	if len(s.List()) != 1 {
		t.Fatal("a replay enrolled a phone")
	}

	late, _, _ := s.NewCode()
	now = now.Add(5 * time.Minute)
	if _, _, err := s.Enroll(late, "Late"); err != ErrInvalidCode {
		t.Fatalf("an expired code: %v", err)
	}
	just, _, _ := s.NewCode()
	now = now.Add(5*time.Minute - time.Second)
	if _, _, err := s.Enroll(just, "Just in time"); err != nil {
		t.Fatalf("a code just before it expires: %v", err)
	}
}

func TestACodeIsOnlyKeptAsAHashAndOnlyInMemory(t *testing.T) {
	s, path := open(t)
	code, _, _ := s.NewCode()
	for _, c := range s.codes {
		if c.hash == code || strings.Contains(c.hash, code) {
			t.Fatal("the code itself is kept")
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("making a code wrote a file")
	}
	// A restart voids it.
	again, _ := Open(path)
	if _, _, err := again.Enroll(code, "Phone"); err != ErrInvalidCode {
		t.Fatalf("a code survived a restart: %v", err)
	}
}

func TestOnlyAFewCodesWaitAtOnce(t *testing.T) {
	s, _ := open(t)
	first, _, _ := s.NewCode()
	for i := 0; i < MaxPendingCodes; i++ {
		s.NewCode()
	}
	if _, _, err := s.Enroll(first, "Phone"); err != ErrInvalidCode {
		t.Fatalf("the oldest code should have been dropped: %v", err)
	}
}

func TestRevokeTakesAPhoneAwayAtOnce(t *testing.T) {
	s, path := open(t)
	a, tokA := enroll(t, s, "A")
	_, tokB := enroll(t, s, "B")
	done, stop := s.Watch(a.ID)
	defer stop()
	select {
	case <-done:
		t.Fatal("closed before revoke")
	default:
	}
	ok, err := s.Revoke(a.ID)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	select {
	case <-done:
	default:
		t.Fatal("revoking did not signal its watchers")
	}
	if _, ok := s.Authenticate(tokA); ok {
		t.Fatal("a revoked token still works")
	}
	if _, ok := s.Authenticate(tokB); !ok {
		t.Fatal("revoking one revoked another")
	}
	if ok, _ := s.Revoke(a.ID); ok {
		t.Fatal("revoked twice")
	}
	again, _ := Open(path)
	if _, ok := again.Authenticate(tokA); ok {
		t.Fatal("the revoked token came back after a restart")
	}
	// Watching a phone that is gone ends at once.
	gone, _ := s.Watch(a.ID)
	select {
	case <-gone:
	default:
		t.Fatal("watching a revoked phone did not end")
	}
}

func TestAtMostSixteenPhones(t *testing.T) {
	s, _ := open(t)
	for i := 0; i < MaxEntries; i++ {
		enroll(t, s, "Phone")
	}
	if _, _, err := s.NewCode(); err != ErrTooMany {
		t.Fatalf("a code when full: %v", err)
	}
	// A code made before the limit was reached stays unspent when the store is full.
	s.mu.Lock()
	s.recs = s.recs[:MaxEntries-1]
	s.mu.Unlock()
	code, _, _ := s.NewCode()
	enroll(t, s, "Last")
	if _, _, err := s.Enroll(code, "One too many"); err != ErrTooMany {
		t.Fatalf("a seventeenth phone: %v", err)
	}
	s.mu.Lock()
	first := s.recs[0].ID
	s.mu.Unlock()
	if _, err := s.Revoke(first); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Enroll(code, "Now there is room"); err != nil {
		t.Fatalf("the unspent code should work once there is room: %v", err)
	}
}

func TestNamesAreBounded(t *testing.T) {
	for _, bad := range []string{"", "   ", strings.Repeat("x", MaxNameLen+1), "bad\x00name", "new\nline"} {
		if _, err := CleanName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if n, err := CleanName("  iPhone  "); err != nil || n != "iPhone" {
		t.Fatalf("%q %v", n, err)
	}
	s, _ := open(t)
	code, _, _ := s.NewCode()
	if _, _, err := s.Enroll(code, ""); err == nil || err == ErrInvalidCode {
		t.Fatalf("an empty name: %v", err)
	}
	if _, _, err := s.Enroll(code, "Fine"); err != nil {
		t.Fatalf("a bad name must not spend the code: %v", err)
	}
}

func TestLastUseIsRememberedAndWrittenAtMostOncePerMinute(t *testing.T) {
	s, path := open(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	e, token := enroll(t, s, "Phone")
	if e.LastUsedAt != nil {
		t.Fatal("used before it was used")
	}
	s.Authenticate(token) // first use writes
	written := func() *time.Time {
		again, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return again.List()[0].LastUsedAt
	}
	first := written()
	if first == nil || !first.Equal(now) {
		t.Fatalf("first use not written: %v", first)
	}
	now = now.Add(20 * time.Second)
	s.Authenticate(token)
	if got := written(); !got.Equal(first.UTC()) {
		t.Fatalf("written again within a minute: %v", got)
	}
	if seen := s.List()[0].LastUsedAt; seen == nil || !seen.Equal(now) {
		t.Fatalf("the list should show the latest use: %v", seen)
	}
	now = now.Add(time.Minute)
	s.Authenticate(token)
	if got := written(); !got.Equal(now) {
		t.Fatalf("not written after a minute: %v", got)
	}
}

func TestAnUnreadableFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phones.json")
	os.WriteFile(path, []byte("{nope"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("opened a broken file")
	}
}

func TestAPhoneCanBeRenamed(t *testing.T) {
	s, path := open(t)
	e, _ := enroll(t, s, "iPhone")
	got, ok, err := s.Rename(e.ID, "  Michel's iPhone ")
	if err != nil || !ok || got.Name != "Michel's iPhone" {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if _, ok, _ := s.Rename("ph_nope", "x"); ok {
		t.Fatal("renamed what is not there")
	}
	if _, _, err := s.Rename(e.ID, ""); err == nil {
		t.Fatal("renamed to nothing")
	}
	again, _ := Open(path)
	if again.List()[0].Name != "Michel's iPhone" {
		t.Fatal("the new name did not survive a restart")
	}
}
