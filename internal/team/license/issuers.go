package license

import (
	"crypto/ed25519"
	"encoding/base64"
)

// Public releases retain the previous issuer during the October 2026 key
// recovery. Only builds configured with one of these release keys trust the
// pair; development and customer-owned issuers remain isolated. These are
// public verification keys. Private issuance keys never enter the build.
const currentReleaseIssuer = "ChctbT2EmgTYU37lb0e1_KkzSO9GR32pkRdcTR-UoYU"
const previousReleaseIssuer = "rhJg3oK5JB8QrUn-Lflsrl0qPmxdOEgDCAwknqFBgXw"

func trustedIssuers(configured ed25519.PublicKey) []ed25519.PublicKey {
	keys := []ed25519.PublicKey{configured}
	var other string
	switch base64.RawURLEncoding.EncodeToString(configured) {
	case currentReleaseIssuer:
		other = previousReleaseIssuer
	case previousReleaseIssuer:
		other = currentReleaseIssuer
	default:
		return keys
	}
	key, err := base64.RawURLEncoding.DecodeString(other)
	if err == nil && len(key) == ed25519.PublicKeySize {
		keys = append(keys, ed25519.PublicKey(key))
	}
	return keys
}

func verifyIssuerSignature(keys []ed25519.PublicKey, message, signature []byte) bool {
	for _, key := range keys {
		if len(key) == ed25519.PublicKeySize && ed25519.Verify(key, message, signature) {
			return true
		}
	}
	return false
}
