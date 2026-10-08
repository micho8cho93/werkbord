package pki

import (
	"bytes"
	"testing"
)

func TestAuthorityWrappingKeyIsSeparateFromDeviceWrappingKey(t *testing.T) {
	s := &SecureSealer{device: &FileSealer{key: bytes.Repeat([]byte{1}, 32)}, authority: &FileSealer{key: bytes.Repeat([]byte{2}, 32)}}
	for _, label := range []string{labelTrust, labelCA, labelStorage} {
		sealed, err := s.Seal(label, []byte("authority secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.device.Open(label, sealed); err == nil {
			t.Fatalf("device wrapping key opened %s", label)
		}
		got, err := s.Open(label, sealed)
		if err != nil || string(got) != "authority secret" {
			t.Fatal("authority could not recover its key")
		}
	}
}
