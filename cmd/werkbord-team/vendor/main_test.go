package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineSignerUsesExplicitInputAndNeverOverwrites(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	secret := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	dir := t.TempDir()
	manifest := filepath.Join(dir, "checksums.txt")
	_ = os.WriteFile(manifest, []byte("public fixture hashes\n"), 0600)
	sigPath := filepath.Join(dir, "sig")
	if err := run([]string{"release", "--input", manifest, "--out", sigPath, "--tag", "werkbord-v9.0.0"}, bytes.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	sig, _ := os.ReadFile(sigPath)
	message := []byte("werkbord-team/release/v1\x00werkbord-v9.0.0\x00public fixture hashes\n")
	if !ed25519.Verify(pub, message, sig) {
		t.Fatal("release signature did not bind product/tag")
	}
	if ed25519.Verify(pub, bytes.Replace(message, []byte("v9.0.0"), []byte("v9.0.1"), 1), sig) {
		t.Fatal("tag substitution accepted")
	}
	if info, _ := os.Stat(sigPath); info.Mode().Perm() != 0600 {
		t.Fatal("output is not private")
	}
	if err := run([]string{"release", "--input", manifest, "--out", sigPath, "--tag", "werkbord-v9.0.0"}, bytes.NewReader(secret)); err == nil {
		t.Fatal("existing signature overwritten")
	}
	if err := run([]string{"release", "--input", manifest, "--out", filepath.Join(dir, "bad"), "--tag", "werkbord-v9.0.0"}, bytes.NewReader([]byte("bad"))); err == nil {
		t.Fatal("malformed key accepted")
	}
	// The retired Team tag series and the desktop-manifest commands are gone.
	for _, args := range [][]string{
		{"release", "--input", manifest, "--out", filepath.Join(dir, "sig2"), "--tag", "werkbord-team-v9.0.0"},
		{"release", "--input", manifest, "--out", filepath.Join(dir, "sig3")},
		{"license", "--input", manifest, "--out", filepath.Join(dir, "sig5")},
		{"desktop-release", "--input", manifest, "--out", filepath.Join(dir, "sig4"), "--tag", "werkbord-v9.0.0"},
		{"verify-desktop-release", "--contents", dir, "--version", "v9.0.0", "--public-key", "x"},
	} {
		if err := run(args, bytes.NewReader(secret)); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}
