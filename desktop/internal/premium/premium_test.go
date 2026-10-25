package premium

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/entitlement"
)

func fixture(t *testing.T) (*Store, ed25519.PrivateKey, string) {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	path := filepath.Join(t.TempDir(), "config", "premium.key")
	s := New(path)
	s.verify = func(text string) (entitlement.Claims, error) { return entitlement.Verify(text, pub) }
	s.canCheck = func() bool { return true }
	return s, key, path
}

func TestAKeyIsCheckedKeptPrivatelyAndShownWithoutItsText(t *testing.T) {
	s, key, path := fixture(t)
	if st := s.Status(); st.Active || !st.CanCheck {
		t.Fatalf("no key yet: %+v", st)
	}
	text, _ := entitlement.Issue(entitlement.Claims{ID: "pk_one", Holder: "Ada", IssuedAt: time.Now()}, key)
	st, err := s.Unlock("  " + text + " \n")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Active || st.ID != "pk_one" || st.Holder != "Ada" {
		t.Fatalf("status = %+v", st)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key file must be private: %v %v", info, err)
	}
	if s.Key() != text {
		t.Fatal("the key is not kept")
	}
	// A new store on the same file still knows it (a restart).
	again := New(path)
	again.verify, again.canCheck = s.verify, s.canCheck
	if !again.Status().Active {
		t.Fatal("the key did not survive a restart")
	}
}

func TestARefusedKeyKeepsNothingAndSaysWhy(t *testing.T) {
	s, key, path := fixture(t)
	for _, bad := range []string{"", "hello", "wbpremium-abc.def"} {
		if _, err := s.Unlock(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a refused key was kept")
	}
	forged, _ := entitlement.Issue(entitlement.Claims{ID: "pk_x", IssuedAt: time.Now()}, func() ed25519.PrivateKey { _, k, _ := ed25519.GenerateKey(rand.Reader); return k }())
	if _, err := s.Unlock(forged); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("a key from another issuer: %v", err)
	}
	// A key that was valid and is later damaged on disk is not used.
	text, _ := entitlement.Issue(entitlement.Claims{ID: "pk_ok", IssuedAt: time.Now()}, key)
	if _, err := s.Unlock(text); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("wbpremium-damaged.x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Key() != "" || s.Status().Active {
		t.Fatal("a damaged key file counted as Premium")
	}
	// And one anyone else can read is not trusted.
	_ = os.WriteFile(path, []byte(text+"\n"), 0o600)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if s.Key() != "" {
		t.Fatal("a world-readable key file was used")
	}
}

func TestABuildThatCannotCheckKeysSaysSo(t *testing.T) {
	s, key, _ := fixture(t)
	s.canCheck = func() bool { return false }
	text, _ := entitlement.Issue(entitlement.Claims{ID: "pk_a", IssuedAt: time.Now()}, key)
	if _, err := s.Unlock(text); err == nil || !strings.Contains(err.Error(), "cannot check") {
		t.Fatalf("err = %v", err)
	}
	if st := s.Status(); st.CanCheck || st.Active {
		t.Fatalf("status = %+v", st)
	}
}
