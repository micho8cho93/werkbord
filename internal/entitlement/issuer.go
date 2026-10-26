package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
)

// issuerPublicKey is the public half of the key that signs unlock keys, as unpadded URL base64 (43 characters). The release
// build stamps it (-X werkbord/internal/entitlement.issuerPublicKey=...); a build without it can check no key, so everyone on it
// is on the Free plan. It is a public key: nothing private is ever linked into a build.
var issuerPublicKey string

// IssuerKey is the public key unlock keys are checked against, or nil when this build has none.
func IssuerKey() ed25519.PublicKey {
	b, err := base64.RawURLEncoding.DecodeString(issuerPublicKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}

// VerifyKey checks a pasted unlock key against this build's issuer key.
func VerifyKey(text string) (Claims, error) { return Verify(text, IssuerKey()) }
