package license

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestPublicIssuerRecoveryRetainsBothReleaseKeys(t *testing.T) {
	current, err := base64.RawURLEncoding.DecodeString(currentReleaseIssuer)
	if err != nil || len(current) != ed25519.PublicKeySize {
		t.Fatal("invalid replacement release issuer")
	}
	previous, err := base64.RawURLEncoding.DecodeString(previousReleaseIssuer)
	if err != nil || len(previous) != ed25519.PublicKeySize {
		t.Fatal("invalid previous release issuer")
	}
	if previousReleaseIssuer != "rhJg3oK5JB8QrUn-Lflsrl0qPmxdOEgDCAwknqFBgXw" || bytes.Equal(previous, current) {
		t.Fatal("the existing public DMG's issuer must remain trusted")
	}
	for _, pair := range [][2]ed25519.PublicKey{{current, previous}, {previous, current}} {
		keys := trustedIssuers(pair[0])
		if len(keys) != 2 || !bytes.Equal(keys[0], pair[0]) || !bytes.Equal(keys[1], pair[1]) {
			t.Fatal("a release configured with either issuer must retain both verification keys")
		}
	}
}

func TestPublicIssuerRecoveryDoesNotChangeOtherBuildsTrust(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("a development license must not activate a release build")
	signature := ed25519.Sign(key, message)
	keys := trustedIssuers(pub)
	if len(keys) != 1 || !bytes.Equal(keys[0], pub) || !verifyIssuerSignature(keys, message, signature) {
		t.Fatal("a development issuer lost its own signature verification")
	}
	for _, issuer := range []string{currentReleaseIssuer, previousReleaseIssuer} {
		release, _ := base64.RawURLEncoding.DecodeString(issuer)
		if verifyIssuerSignature(trustedIssuers(release), message, signature) {
			t.Fatal("a development signature activated a public release")
		}
	}
	if verifyIssuerSignature(trustedIssuers(nil), message, signature) {
		t.Fatal("an unconfigured build accepted a development license")
	}
}

func TestIssuerRotationStillRequiresAnIntactSignatureFromATrustedKey(t *testing.T) {
	first, firstKey, _ := ed25519.GenerateKey(rand.Reader)
	second, secondKey, _ := ed25519.GenerateKey(rand.Reader)
	_, strangerKey, _ := ed25519.GenerateKey(rand.Reader)
	message := append([]byte("werkbord-team/license/v2\x00"), []byte(`{"seats":10}`)...)
	keys := []ed25519.PublicKey{first, second}
	for _, key := range []ed25519.PrivateKey{firstKey, secondKey} {
		signature := ed25519.Sign(key, message)
		if !verifyIssuerSignature(keys, message, signature) {
			t.Fatal("a trusted issuer's signature was rejected")
		}
		changed := append([]byte(nil), message...)
		changed[len(changed)-2] ^= 1
		if verifyIssuerSignature(keys, changed, signature) {
			t.Fatal("issuer rotation accepted altered license claims")
		}
		if verifyIssuerSignature(keys, message, signature[:len(signature)-1]) {
			t.Fatal("issuer rotation accepted a truncated signature")
		}
	}
	if verifyIssuerSignature(keys, message, ed25519.Sign(strangerKey, message)) {
		t.Fatal("issuer rotation accepted an unknown issuer")
	}
	if verifyIssuerSignature([]ed25519.PublicKey{first}, message, ed25519.Sign(secondKey, message)) {
		t.Fatal("an issuer not in the trusted set activated a license")
	}
}
