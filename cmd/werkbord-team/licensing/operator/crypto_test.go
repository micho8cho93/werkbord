package operator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	"devboard/internal/team/license"
)

func fixture(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, Artifact, []byte) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := license.Claims{Schema: 2, Product: "werkbord-team", Edition: "team", ID: "lic_fixture", Customer: "Research & <Development>", Seats: 3, IssuedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	raw, err := license.Issue(c, key)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	a := Artifact{LicenseID: c.ID, EntitlementID: "ent_fixture", Revision: 1, Document: string(raw), SHA256: Hash(raw), IssuerFingerprint: Fingerprint(pub), Claims: c, OperationID: "op_fixture"}
	snapshot := map[string]any{"schema": 1, "product": "werkbord-licensing-register", "exportedAt": time.Now().UTC(), "issuers": []any{map[string]any{"fingerprint": Fingerprint(pub), "public_key": base64.RawURLEncoding.EncodeToString(pub), "can_issue": true}}, "entitlements": []any{map[string]any{"id": a.EntitlementID, "current_revision": 1, "status": "active"}}, "operations": []Operation{{ID: a.OperationID, Reference: "manual-fixture", EntitlementID: a.EntitlementID, Revision: 1, State: "completed", Kind: "import", Claims: c}}, "artifacts": []Artifact{a}, "events": []any{map[string]any{"id": 1, "entitlement_id": a.EntitlementID, "operation_id": a.OperationID}}}
	backup, _ := json.Marshal(snapshot)
	return pub, key, a, backup
}
func TestEncryptedRecoveryAndHistoricalArtifacts(t *testing.T) {
	pub, key, a, raw := fixture(t)
	defer clear(key)
	if err := VerifyArtifact(a, []ed25519.PublicKey{pub}); err != nil {
		t.Fatal("expired historical license should verify:", err)
	}
	encrypted, err := Encrypt(raw, "independent recovery password")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(a.Document)) {
		t.Fatal("plaintext license leaked into encrypted backup")
	}
	restored, err := Decrypt(encrypted, "independent recovery password", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := VerifySnapshot(restored, []ed25519.PublicKey{pub}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if !bytes.Equal(raw, restored) {
		t.Fatal("backup bytes changed")
	}
	if _, err := Decrypt(encrypted, "a wrong recovery password", 1<<20); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := Decrypt(encrypted, "independent recovery password", 1<<20); err == nil {
		t.Fatal("altered backup accepted")
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	restoredKey, err := ParseKey(keyPEM, Fingerprint(pub))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(restoredKey)
	if RecoveryProof(restoredKey) == "" {
		t.Fatal("missing recovery proof")
	}
	if _, err := ParseKey(keyPEM, "wrong fingerprint"); err == nil {
		t.Fatal("wrong issuer accepted")
	}
}
func TestSnapshotRejectsAlteredAndMissingHistory(t *testing.T) {
	pub, key, _, raw := fixture(t)
	defer clear(key)
	for _, mutate := range []func(map[string]any){
		func(s map[string]any) { s["artifacts"].([]any)[0].(map[string]any)["document"] = "altered" },
		func(s map[string]any) { s["artifacts"] = []any{} },
		func(s map[string]any) { s["operations"] = []any{} },
		func(s map[string]any) {
			s["artifacts"].([]any)[0].(map[string]any)["claims"].(map[string]any)["seats"] = 10
		},
		func(s map[string]any) { s["operations"] = append(s["operations"].([]any), s["operations"].([]any)[0]) },
	} {
		var data map[string]any
		_ = json.Unmarshal(raw, &data)
		mutate(data)
		bad, _ := json.Marshal(data)
		if _, err := VerifySnapshot(bad, []ed25519.PublicKey{pub}); err == nil {
			t.Fatal("corrupt snapshot accepted")
		}
	}
}
