// Package operator implements vendor-only licensing tools. Customer executables
// must never import this package or receive an issuer's private key.
package operator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"time"

	"devboard/internal/team/license"
	"filippo.io/age"
)

func Fingerprint(key ed25519.PublicKey) string {
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:])
}
func Hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

func ParseKey(raw []byte, expected string) (ed25519.PrivateKey, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("one PKCS#8 Ed25519 private key is required")
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("cannot parse the issuer key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("the issuer key must be Ed25519")
	}
	if Fingerprint(key.Public().(ed25519.PublicKey)) != expected {
		clear(key)
		return nil, errors.New("issuer fingerprint mismatch; signing is blocked")
	}
	return key, nil
}

func Encrypt(raw []byte, passphrase string) ([]byte, error) {
	if len(passphrase) < 14 {
		return nil, errors.New("use a recovery passphrase of at least 14 characters")
	}
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(raw); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func Decrypt(raw []byte, passphrase string, max int64) ([]byte, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	identity.SetMaxWorkFactor(18)
	r, err := age.Decrypt(bytes.NewReader(raw), identity)
	if err != nil {
		return nil, errors.New("the encrypted file could not be unlocked")
	}
	out, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil || int64(len(out)) > max {
		clear(out)
		return nil, errors.New("the encrypted file is damaged or exceeds the size limit")
	}
	return out, nil
}

type Artifact struct {
	LicenseID         string         `json:"license_id"`
	EntitlementID     string         `json:"entitlement_id"`
	Revision          int            `json:"revision"`
	Document          string         `json:"document"`
	SHA256            string         `json:"sha256"`
	IssuerFingerprint string         `json:"issuer_fingerprint"`
	Claims            license.Claims `json:"claims"`
	OperationID       string         `json:"operation_id"`
}
type Operation struct {
	ID            string         `json:"id"`
	Reference     string         `json:"request_reference"`
	EntitlementID string         `json:"entitlement_id"`
	Revision      int            `json:"revision"`
	Kind          string         `json:"kind"`
	State         string         `json:"state"`
	Claims        license.Claims `json:"claims"`
}

// VerifyArtifact verifies history at its issue date, including artifacts that
// have since expired. Runtime activation still uses the normal current clock.
func VerifyArtifact(a Artifact, keys []ed25519.PublicKey) error {
	if len(a.Document) > 16384 || Hash([]byte(a.Document)) != a.SHA256 {
		return errors.New("artifact hash mismatch")
	}
	var envelope struct {
		Claims license.Claims `json:"claims"`
	}
	if json.Unmarshal([]byte(a.Document), &envelope) != nil {
		return errors.New("invalid artifact")
	}
	for _, key := range keys {
		if Fingerprint(key) != a.IssuerFingerprint {
			continue
		}
		c, err := license.Verify([]byte(a.Document), key, envelope.Claims.IssuedAt.Add(time.Second))
		if err != nil {
			return err
		}
		// Verify pins the release ring too; check the actual signer explicitly.
		var doc struct {
			Claims    json.RawMessage `json:"claims"`
			Signature string          `json:"signature"`
		}
		_ = json.Unmarshal([]byte(a.Document), &doc)
		sig, _ := base64.RawURLEncoding.DecodeString(doc.Signature)
		if !ed25519.Verify(key, license.SigningBytes(doc.Claims), sig) {
			return errors.New("artifact issuer fingerprint mismatch")
		}
		if c != a.Claims || c.ID != a.LicenseID || a.Revision < 1 || a.EntitlementID == "" || a.OperationID == "" {
			return errors.New("artifact metadata does not match its signed claims")
		}
		return nil
	}
	return errors.New("artifact uses an unpinned issuer")
}

func VerifySnapshot(raw []byte, keys []ed25519.PublicKey) (int, error) {
	var s struct {
		Schema     int       `json:"schema"`
		Product    string    `json:"product"`
		ExportedAt time.Time `json:"exportedAt"`
		Issuers    []struct {
			Fingerprint string `json:"fingerprint"`
			PublicKey   string `json:"public_key"`
		} `json:"issuers"`
		Entitlements []struct {
			ID              string `json:"id"`
			CurrentRevision int    `json:"current_revision"`
			Status          string `json:"status"`
		} `json:"entitlements"`
		Operations []Operation `json:"operations"`
		Artifacts  []Artifact  `json:"artifacts"`
		Events     []struct {
			ID            int64  `json:"id"`
			EntitlementID string `json:"entitlement_id"`
			OperationID   string `json:"operation_id"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	if s.Schema != 1 || s.Product != "werkbord-licensing-register" || s.ExportedAt.IsZero() {
		return 0, errors.New("unsupported register backup")
	}
	ents := map[string]int{}
	issuerIDs := map[string]bool{}
	for _, issuer := range s.Issuers {
		key, err := base64.RawURLEncoding.DecodeString(issuer.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize || Fingerprint(key) != issuer.Fingerprint || issuerIDs[issuer.Fingerprint] {
			return 0, errors.New("invalid issuer registry")
		}
		trusted := false
		for _, pinned := range keys {
			if bytes.Equal(pinned, key) {
				trusted = true
			}
		}
		if !trusted {
			return 0, errors.New("backup contains an unpinned issuer")
		}
		issuerIDs[issuer.Fingerprint] = true
	}
	if len(issuerIDs) == 0 {
		return 0, errors.New("backup has no issuer registry")
	}
	ops := map[string]Operation{}
	refs := map[string]bool{}
	ids := map[string]bool{}
	revs := map[string]bool{}
	for _, e := range s.Entitlements {
		if e.ID == "" || e.CurrentRevision < 0 || (e.Status != "active" && e.Status != "pending" && e.Status != "closed") {
			return 0, errors.New("invalid entitlement")
		}
		if _, ok := ents[e.ID]; ok {
			return 0, errors.New("duplicate entitlement")
		}
		ents[e.ID] = e.CurrentRevision
	}
	for _, op := range s.Operations {
		if _, ok := ents[op.EntitlementID]; !ok {
			return 0, errors.New("orphaned operation")
		}
		if op.ID == "" || op.Reference == "" || refs[op.Reference] || ops[op.ID].ID != "" {
			return 0, errors.New("duplicate operation or request reference")
		}
		refs[op.Reference] = true
		ops[op.ID] = op
	}
	for _, a := range s.Artifacts {
		if !issuerIDs[a.IssuerFingerprint] {
			return 0, errors.New("artifact issuer is missing from the registry")
		}
		if err := VerifyArtifact(a, keys); err != nil {
			return 0, fmt.Errorf("%s: %w", a.LicenseID, err)
		}
		op, ok := ops[a.OperationID]
		if !ok || op.EntitlementID != a.EntitlementID || op.Revision != a.Revision || op.State != "completed" {
			return 0, errors.New("orphaned artifact")
		}
		rev := fmt.Sprintf("%s/%d", a.EntitlementID, a.Revision)
		if ids[a.LicenseID] || revs[rev] {
			return 0, errors.New("duplicate artifact or revision")
		}
		ids[a.LicenseID] = true
		revs[rev] = true
	}
	for id, revision := range ents {
		for n := 1; n <= revision; n++ {
			if !revs[fmt.Sprintf("%s/%d", id, n)] {
				return 0, errors.New("missing entitlement revision")
			}
		}
	}
	events := map[int64]bool{}
	for _, ev := range s.Events {
		op, ok := ops[ev.OperationID]
		if ev.ID < 1 || events[ev.ID] || !ok || op.EntitlementID != ev.EntitlementID {
			return 0, errors.New("invalid audit event")
		}
		events[ev.ID] = true
	}
	return len(s.Artifacts), nil
}

func RecoveryProof(key ed25519.PrivateKey) string {
	fp := Fingerprint(key.Public().(ed25519.PublicKey))
	message := []byte("werkbord-licensing/recovery/v1\x00" + fp)
	sig := ed25519.Sign(key, message)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, sig) {
		panic("Ed25519 recovery verification failed")
	}
	return base64.RawURLEncoding.EncodeToString(sig)
}
