package localidentity

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid"
)

var t0 = time.Date(2026, 10, 6, 12, 0, 0, 123456789, time.UTC)

func TestANewIdentityHasEverythingADeviceNeeds(t *testing.T) {
	i, err := New("  Ada's laptop ", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !deviceid.ValidID(string(i.ID())) || i.Name() != "Ada's laptop" || !i.CreatedAt().Equal(t0.Truncate(time.Millisecond)) {
		t.Fatalf("%+v", i.Public())
	}
	if len(i.PublicKey()) != ed25519.PublicKeySize {
		t.Fatal("no public key")
	}
	p := i.Public()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	// IDs and keys are random.
	j, _ := New("x", t0)
	if i.ID() == j.ID() || string(i.PublicKey()) == string(j.PublicKey()) {
		t.Fatal("two identities share an ID or a key")
	}
	// What it signs, the public half verifies; nothing else does.
	sig := i.Sign([]byte("hello"))
	if !deviceid.Verify(i.PublicKey(), []byte("hello"), sig) || deviceid.Verify(i.PublicKey(), []byte("hellp"), sig) || deviceid.Verify(j.PublicKey(), []byte("hello"), sig) {
		t.Fatal("signature checks are wrong")
	}
}

func TestBadNamesAreRefused(t *testing.T) {
	for _, n := range []string{"", "   ", "a\x00b", "bell\a", strings.Repeat("x", deviceid.MaxNameLen+1)} {
		if _, err := New(n, t0); !errors.Is(err, deviceid.ErrInvalid) {
			t.Errorf("%q: %v", n, err)
		}
	}
}

func TestTheIdentityPersistsAcrossRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "id")
	s := FileStore{Dir: dir}
	if _, err := s.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty store: %v", err)
	}
	first, created, err := LoadOrCreate(s, "Ada's laptop", t0)
	if err != nil || !created {
		t.Fatalf("%v %v", created, err)
	}
	second, created, err := LoadOrCreate(FileStore{Dir: dir}, "a different name that must be ignored", t0.Add(time.Hour))
	if err != nil || created {
		t.Fatalf("%v %v", created, err)
	}
	if first.ID() != second.ID() || first.Name() != second.Name() || !first.CreatedAt().Equal(second.CreatedAt()) ||
		string(first.PublicKey()) != string(second.PublicKey()) {
		t.Fatalf("the identity changed across a restart:\n%+v\n%+v", first.Public(), second.Public())
	}
	// The reloaded key signs what the first verifies.
	if !deviceid.Verify(first.PublicKey(), []byte("m"), second.Sign([]byte("m"))) {
		t.Fatal("the reloaded private key is not the same key")
	}
}

func TestTheStoredFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not POSIX")
	}
	dir := filepath.Join(t.TempDir(), "id")
	i, _ := New("d", t0)
	s := FileStore{Dir: dir}
	if err := s.Save(i); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("directory is %o", st.Mode().Perm())
	}
	st, _ = os.Stat(filepath.Join(dir, FileName))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("file is %o", st.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a temporary file was left behind: %v", entries)
	}

	// A key that someone loosened is refused, and says what to do.
	if err := os.Chmod(filepath.Join(dir, FileName), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrInsecure) {
		t.Fatalf("a world-readable key was accepted: %v", err)
	}
	// And such an identity is never silently replaced by a new one.
	if _, _, err := LoadOrCreate(s, "d", t0); !errors.Is(err, ErrInsecure) {
		t.Fatalf("LoadOrCreate replaced an unreadable identity: %v", err)
	}
	// Saving over a file someone loosened leaves the new one private again.
	if err := s.Save(i); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
}

func TestADamagedFileIsAnErrorNotANewIdentity(t *testing.T) {
	dir := t.TempDir()
	i, _ := New("d", t0)
	s := FileStore{Dir: dir}
	if err := s.Save(i); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	good, _ := os.ReadFile(path)

	mutate := func(f func(m map[string]any)) {
		var m map[string]any
		_ = json.Unmarshal(good, &m)
		f(m)
		b, _ := json.Marshal(m)
		_ = os.WriteFile(path, b, 0o600)
	}
	other, _ := New("other", t0)
	for name, f := range map[string]func(map[string]any){
		"another key's public half": func(m map[string]any) { m["publicKey"] = deviceid.EncodePublicKey(other.PublicKey()) },
		"a damaged key":             func(m map[string]any) { m["privateKey"] = "AAAA" },
		"a bad device ID":           func(m map[string]any) { m["id"] = "dev_nope" },
		"a bad name":                func(m map[string]any) { m["name"] = "" },
		"a version from the future": func(m map[string]any) { m["version"] = 2 },
	} {
		mutate(f)
		if _, err := s.Load(); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: loaded (%v)", name, err)
		}
		if _, _, err := LoadOrCreate(s, "d", t0); err == nil {
			t.Errorf("%s: LoadOrCreate made a new identity over it", name)
		}
	}
	_ = os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := s.Load(); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("garbage: %v", err)
	}
}

func TestThePrivateKeyNeverAppearsInPrintsOrPublicRecords(t *testing.T) {
	i, _ := New("d", t0)
	seed := fmt.Sprintf("%x", i.priv.Seed())
	for _, s := range []string{fmt.Sprint(i), fmt.Sprintf("%v", i), fmt.Sprintf("%+v", i), fmt.Sprintf("%#v", i), i.String()} {
		if strings.Contains(s, seed) || strings.Contains(s, fmt.Sprintf("%v", []byte(i.priv.Seed()))) {
			t.Fatalf("a print of the identity shows the key: %s", s)
		}
	}
	b, _ := json.Marshal(i.Public())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for k := range m {
		if strings.Contains(strings.ToLower(k), "private") || strings.Contains(strings.ToLower(k), "secret") || strings.Contains(strings.ToLower(k), "seed") {
			t.Errorf("the public record has a field %q", k)
		}
	}
}

func TestRenameKeepsTheIdentity(t *testing.T) {
	i, _ := New("old", t0)
	id, key := i.ID(), string(i.PublicKey())
	if err := i.Rename("new"); err != nil || i.Name() != "new" || i.ID() != id || string(i.PublicKey()) != key {
		t.Fatalf("%v %+v", err, i.Public())
	}
	if err := i.Rename(""); err == nil || i.Name() != "new" {
		t.Fatal("renamed to nothing")
	}
}

func TestARegistrationProofIsBoundToWhatIsBeingRegistered(t *testing.T) {
	i, _ := New("d", t0)
	pub := i.Public()
	proof := i.ProveRegistration("tws_a", "tmb_a")
	if err := deviceid.VerifyRegistration("tws_a", "tmb_a", pub, proof); err != nil {
		t.Fatal(err)
	}
	other, _ := New("e", t0)
	cases := map[string]func() error{
		"another workspace": func() error { return deviceid.VerifyRegistration("tws_b", "tmb_a", pub, proof) },
		"another member":    func() error { return deviceid.VerifyRegistration("tws_a", "tmb_b", pub, proof) },
		"another name": func() error {
			p := pub
			p.Name = "renamed"
			return deviceid.VerifyRegistration("tws_a", "tmb_a", p, proof)
		},
		"another device ID": func() error {
			p := pub
			p.ID = NewID()
			return deviceid.VerifyRegistration("tws_a", "tmb_a", p, proof)
		},
		"a key the signer does not hold": func() error {
			p := pub
			p.PublicKey = deviceid.EncodePublicKey(other.PublicKey())
			return deviceid.VerifyRegistration("tws_a", "tmb_a", p, proof)
		},
		"no proof": func() error { return deviceid.VerifyRegistration("tws_a", "tmb_a", pub, nil) },
	}
	for name, f := range cases {
		if err := f(); err == nil {
			t.Errorf("%s: the proof was accepted", name)
		}
	}
}

func TestFingerprintsAreStableAndDistinct(t *testing.T) {
	a, _ := New("a", t0)
	b, _ := New("b", t0)
	fa := deviceid.Fingerprint(a.PublicKey())
	if fa != deviceid.Fingerprint(a.PublicKey()) || fa == deviceid.Fingerprint(b.PublicKey()) || len(fa) != 19 {
		t.Fatalf("%q", fa)
	}
}

func TestAnIDMustHaveTheRightShape(t *testing.T) {
	if !deviceid.ValidID(string(NewID())) {
		t.Fatal("a generated ID is not valid")
	}
	for _, s := range []string{"", "dev_", "dev_abc", "tmb_aaaaaaaaaaaaaaaa", "dev_AAAAAAAAAAAAAAAA", "dev_aaaaaaaaaaaaaaa1", "dev_aaaaaaaaaaaaaaaaa", "dev_aaaaaaaaaaaaaaa/"} {
		if deviceid.ValidID(s) {
			t.Errorf("%q accepted", s)
		}
	}
}
