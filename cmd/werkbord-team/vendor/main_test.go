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
	"time"

	"devboard/internal/team/license"
)

func TestOfflineIssuerUsesExplicitInputAndNeverOverwrites(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	secret := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	dir := t.TempDir()
	input := filepath.Join(dir, "claims.json")
	output := filepath.Join(dir, "license.json")
	raw, _ := license.CanonicalClaims(license.Claims{Schema: 2, Product: "werkbord-team", ID: "lic_fixture", Customer: "org_fixture", Edition: "team", Seats: 5, IssuedAt: time.Now().Add(-time.Hour)})
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"license", "--input", input, "--out", output}
	if err := run(args, bytes.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	signed, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := license.Verify(signed, pub, time.Now()); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(signed, secret) || bytes.Contains(signed, der) {
		t.Fatal("private key leaked")
	}
	if info, _ := os.Stat(output); info.Mode().Perm() != 0600 {
		t.Fatal("output is not private")
	}
	if err := run(args, bytes.NewReader(secret)); err == nil {
		t.Fatal("existing license overwritten")
	}
	if err := run([]string{"license", "--input", input, "--out", filepath.Join(dir, "bad")}, bytes.NewReader([]byte("bad"))); err == nil {
		t.Fatal("malformed key accepted")
	}
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
	// The retired Team tag series and the desktop-manifest commands are gone.
	for _, args := range [][]string{
		{"release", "--input", manifest, "--out", filepath.Join(dir, "sig2"), "--tag", "werkbord-team-v9.0.0"},
		{"release", "--input", manifest, "--out", filepath.Join(dir, "sig3")},
		{"desktop-release", "--input", manifest, "--out", filepath.Join(dir, "sig4"), "--tag", "werkbord-v9.0.0"},
		{"verify-desktop-release", "--contents", dir, "--version", "v9.0.0", "--public-key", "x"},
	} {
		if err := run(args, bytes.NewReader(secret)); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}
