package pki

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSecretVault(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewFileSealer(filepath.Join(dir, "secrets", "sealing.key"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := OpenVault(filepath.Join(dir, "pki"), s)
	if err != nil {
		t.Fatal(err)
	}
	return v, dir
}

func TestANamedSecretIsSealedAndComesBackOnlyToTheVaultThatSealedIt(t *testing.T) {
	v, dir := newSecretVault(t)
	if v.HasSecret("bridge") {
		t.Fatal("a secret from nowhere")
	}
	if _, err := v.Secret("bridge"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
	secret := []byte("wba_this-is-a-token-" + strings.Repeat("x", 30))
	if err := v.SaveSecret("bridge", secret); err != nil {
		t.Fatal(err)
	}
	got, err := v.Secret("bridge")
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("%q %v", got, err)
	}
	// On disk it is not the secret, and only its owner can read the file.
	raw, _ := os.ReadFile(filepath.Join(v.Dir(), secretFile("bridge")))
	if bytes.Contains(raw, secret) || bytes.Contains(raw, []byte("wba_")) {
		t.Fatal("the secret is on disk in the clear")
	}
	if fi, _ := os.Stat(filepath.Join(v.Dir(), secretFile("bridge"))); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	// A secret sealed under one name does not open under another (the name is part of what it is sealed to).
	if err := os.Rename(filepath.Join(v.Dir(), secretFile("bridge")), filepath.Join(v.Dir(), secretFile("other"))); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Secret("other"); err == nil {
		t.Fatal("a secret opened under another name")
	}
	// Another vault, with another key, cannot open it.
	_ = os.Rename(filepath.Join(v.Dir(), secretFile("other")), filepath.Join(v.Dir(), secretFile("bridge")))
	s2, _ := NewFileSealer(filepath.Join(dir, "elsewhere", "sealing.key"))
	v2, _ := OpenVault(v.Dir(), s2)
	if _, err := v2.Secret("bridge"); err == nil {
		t.Fatal("another key opened a sealed secret")
	}
	if err := v.DeleteSecret("bridge"); err != nil || v.HasSecret("bridge") {
		t.Fatal(err)
	}
	if err := v.DeleteSecret("bridge"); err != nil {
		t.Fatalf("deleting what is not there: %v", err)
	}
}

func TestASecretsNameIsNotAPath(t *testing.T) {
	v, _ := newSecretVault(t)
	for _, bad := range []string{"", "../x", "a/b", "A", "a b", strings.Repeat("a", 41), "a.b", ".."} {
		if err := v.SaveSecret(bad, []byte("x")); err == nil {
			t.Errorf("%q was accepted", bad)
		}
		if v.HasSecret(bad) {
			t.Errorf("%q exists", bad)
		}
	}
}

func TestAHostsKeysCanBeSealedByTheCallerAndComeBackTheSame(t *testing.T) {
	k, err := NewHostKeys()
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseHostKeys(b)
	if err != nil || back.DeviceID() != k.DeviceID() || !bytes.Equal(back.PublicKey(), k.PublicKey()) || back.SealingPublicKey() != k.SealingPublicKey() {
		t.Fatalf("%v", err)
	}
	if got := back.Sign([]byte("x")); !bytes.Equal(got, k.Sign([]byte("x"))) {
		t.Fatal("the restored key signs differently")
	}
}
