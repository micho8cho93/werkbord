// Package releaseverify authenticates product-neutral distribution manifests.
// It has no runtime key discovery: the trust anchor comes from the trusted build.
package releaseverify

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PublicKey is a raw URL-base64 Ed25519 public key set by the release build.
// An unconfigured build refuses executable downloads rather than trusting a mirror.
var PublicKey string

var tagPattern = regexp.MustCompile(`^(werkbord-v|v)[0-9]+\.[0-9]+\.[0-9]+(-preview\.[1-9][0-9]*)?$`)
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

const MaxManifest = 1 << 20

func SigningBytes(tag string, manifest []byte) ([]byte, error) {
	if !tagPattern.MatchString(tag) {
		return nil, errors.New("invalid release tag")
	}
	if _, err := Parse(manifest); err != nil {
		return nil, err
	}
	return append([]byte("werkbord/release/v1\x00"+tag+"\x00"), manifest...), nil
}

func ConfiguredKey() (ed25519.PublicKey, error) {
	key, err := base64.RawURLEncoding.Strict().DecodeString(PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("no trusted release verification key configured; obtain a signed build from the publisher")
	}
	return ed25519.PublicKey(key), nil
}

func Parse(manifest []byte) (map[string]string, error) {
	if len(manifest) == 0 || len(manifest) > MaxManifest {
		return nil, errors.New("invalid release manifest size")
	}
	entries := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, errors.New("invalid release manifest entry")
		}
		name := strings.TrimPrefix(fields[1], "*")
		hash, err := hex.DecodeString(fields[0])
		if err != nil || len(hash) != 32 || !namePattern.MatchString(name) || name == "." || name == ".." {
			return nil, errors.New("invalid release hash or filename")
		}
		if _, found := entries[name]; found {
			return nil, fmt.Errorf("duplicate release manifest entry: %s", name)
		}
		entries[name] = strings.ToLower(fields[0])
		if len(entries) > 4096 {
			return nil, errors.New("too many release artifacts")
		}
	}
	return entries, nil
}

func Verify(key ed25519.PublicKey, tag string, manifest, signature []byte) (map[string]string, error) {
	message, err := SigningBytes(tag, manifest)
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, message, signature) {
		return nil, errors.New("release manifest signature is not valid")
	}
	return Parse(manifest)
}
