package pki

import (
	"crypto/sha256"
	"encoding/hex"
)

// SecureSealer uses two OS protected wrapping keys. Ordinary device keys cannot
// unwrap the workspace identity, CA or database credentials, even if their own wrapping key leaks.
type SecureSealer struct{ device, authority *FileSealer }

func NewSecureSealer(identity string) (*SecureSealer, error) {
	h := sha256.Sum256([]byte(identity))
	service := "werkbord-team/" + hex.EncodeToString(h[:])
	d, err := secureKey(service + "/device")
	if err != nil {
		return nil, err
	}
	a, err := secureKey(service + "/authority")
	if err != nil {
		return nil, err
	}
	return &SecureSealer{device: &FileSealer{key: d}, authority: &FileSealer{key: a}}, nil
}

func (s *SecureSealer) forLabel(label string) *FileSealer {
	switch label {
	case labelTrust, labelCA, labelStorage:
		return s.authority
	default:
		return s.device
	}
}
func (s *SecureSealer) Seal(label string, raw []byte) ([]byte, error) {
	return s.forLabel(label).Seal(label, raw)
}
func (s *SecureSealer) Open(label string, sealed []byte) ([]byte, error) {
	return s.forLabel(label).Open(label, sealed)
}
