package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/entitlement"
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

func TestKeygenAndPremiumMakeAKeyTheBuildCanCheck(t *testing.T) {
	dir := t.TempDir()
	priv, pub := filepath.Join(dir, "issuer.pem"), filepath.Join(dir, "issuer.pub")
	if err := run([]string{"keygen", "--private-out", priv, "--public-out", pub}, nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{priv, pub} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s must be owner-only: %v", p, err)
		}
	}
	if err := run([]string{"keygen", "--private-out", priv, "--public-out", filepath.Join(dir, "other.pub")}, nil); err == nil {
		t.Fatal("an existing private key was overwritten")
	}
	pemBytes, _ := os.ReadFile(priv)
	rawPub, _ := os.ReadFile(pub)
	issuer, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(rawPub)))
	if err != nil || len(issuer) != ed25519.PublicKeySize {
		t.Fatalf("the public key is 43 characters of raw URL base64: %v", err)
	}
	out := filepath.Join(dir, "unlock.txt")
	if err := run([]string{"premium", "--out", out, "--id", "pk_one", "--holder", "Ada"}, bytes.NewReader(pemBytes)); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(out)
	c, err := entitlement.Verify(string(text), ed25519.PublicKey(issuer))
	if err != nil || c.ID != "pk_one" || c.Holder != "Ada" {
		t.Fatalf("the key does not check: %+v, %v", c, err)
	}
	if err := run([]string{"premium", "--out", out}, bytes.NewReader(pemBytes)); err == nil {
		t.Fatal("an existing key file was overwritten")
	}
	second := filepath.Join(dir, "second.txt")
	if err := run([]string{"premium", "--out", second}, bytes.NewReader(pemBytes)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(second)
	if c, err := entitlement.Verify(string(b), ed25519.PublicKey(issuer)); err != nil || !strings.HasPrefix(c.ID, "pk_") {
		t.Fatalf("a key made without an ID gets a random one: %+v, %v", c, err)
	}
}
