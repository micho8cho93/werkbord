package releaseverify

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

func TestSignatureBindsProductTagAndExactManifest(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	manifest := []byte(strings.Repeat("a", 64) + "  Werkbord_4.89.0-preview.1_linux_amd64.deb\n")
	tag := "werkbord-v4.89.0-preview.1"
	message, err := SigningBytes(tag, manifest)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(key, message)
	if _, err := Verify(pub, tag, manifest, sig); err != nil {
		t.Fatal(err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	for _, c := range []struct {
		key       ed25519.PublicKey
		tag       string
		data, sig []byte
	}{
		{other, tag, manifest, sig}, {pub, "werkbord-v4.89.0-preview.2", manifest, sig},
		{pub, tag, append(append([]byte{}, manifest...), '\n'), sig}, {pub, tag, manifest, nil},
		{pub, tag, manifest, ed25519.Sign(key, append([]byte("werkbord-team/release/v1\x00"+tag+"\x00"), manifest...))},
	} {
		if _, err := Verify(c.key, c.tag, c.data, c.sig); err == nil {
			t.Fatal("substituted release accepted")
		}
	}
}

func TestManifestRejectsAmbiguousOrEscapingEntries(t *testing.T) {
	h := strings.Repeat("0", 64)
	for _, s := range []string{"", h + "  ../app\n", h + "  /app\n", h + "  app\n" + h + "  app\n", "bad  app\n", h + "  app extra\n"} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
