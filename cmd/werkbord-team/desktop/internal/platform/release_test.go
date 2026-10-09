package platform

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineDesktopReleaseTrust(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(pub)
	dir := t.TempDir()
	version := "v3.8.0"
	m := DesktopManifest{Tag: "werkbord-team-" + version, Files: map[string]string{}}
	for _, name := range releaseFiles {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0700)
		raw := []byte(name)
		os.WriteFile(p, raw, 0700)
		h := sha256.Sum256(raw)
		m.Files[name] = hex.EncodeToString(h[:])
	}
	raw, _ := json.Marshal(m)
	path := filepath.Join(dir, "Resources", "team-release.json")
	os.WriteFile(path, raw, 0600)
	sign := func(domain string) {
		os.WriteFile(path+".sig", ed25519.Sign(key, append([]byte(domain+"\x00"+m.Tag+"\x00"), raw...)), 0600)
	}
	sign("werkbord-team/desktop-release/v1")
	if err = verifyRelease(dir, version, encoded); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"wrong product domain", func() { sign("werkbord/release/v1") }},
		{"CLI signature cannot authorize desktop", func() { sign("werkbord-team/release/v1") }},
		{"tampered payload", func() { os.WriteFile(filepath.Join(dir, releaseFiles[0]), []byte("changed"), 0700) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mutate()
			if err = verifyRelease(dir, version, encoded); err == nil {
				t.Fatal("invalid release accepted")
			}
			os.WriteFile(filepath.Join(dir, releaseFiles[0]), []byte(releaseFiles[0]), 0700)
			sign("werkbord-team/desktop-release/v1")
		})
	}
	if err = verifyRelease(dir, "v3.9.0", encoded); err == nil {
		t.Fatal("release replay accepted")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if err = verifyRelease(dir, version, base64.RawURLEncoding.EncodeToString(other)); err == nil {
		t.Fatal("wrong key accepted")
	}
	os.Remove(path + ".sig")
	if err = verifyRelease(dir, version, encoded); err == nil {
		t.Fatal("missing signature accepted")
	}
}

func TestReplacementRefusesEveryEnrolledSlotAndTransition(t *testing.T) {
	for _, name := range []string{"workspace", "pending", "leaving", "demoting", "slots/second/workspace", "slots/second/pending"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := CheckReplacement(dir); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, name)
			os.MkdirAll(filepath.Dir(p), 0700)
			os.WriteFile(p, []byte("preserved"), 0600)
			if err := CheckReplacement(dir); err == nil {
				t.Fatal("unsafe service replacement allowed")
			}
			if b, err := os.ReadFile(p); err != nil || string(b) != "preserved" {
				t.Fatal("state changed")
			}
		})
	}
}
