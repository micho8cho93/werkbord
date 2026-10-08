package license

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestCanonicalPerpetualLicenseAndSchemaFailClosed(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Unix(1800000000, 0)
	c := Claims{Schema: Schema, Product: "werkbord-team", Edition: EditionTeam, ID: "lic_one", Customer: "org_one", Seats: 3, IssuedAt: at.Add(-time.Hour), SupportEndsAt: at}
	raw, err := Issue(c, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(raw, pub, at.AddDate(20, 0, 0)); err != nil {
		t.Fatalf("support expiry prevented offline runtime: %v", err)
	}
	canonical, _ := CanonicalClaims(c)
	if bytes.Contains(canonical, []byte("expiresAt")) {
		t.Fatal("perpetual expiry was serialized")
	}
	zone := c
	zone.IssuedAt = c.IssuedAt.In(time.FixedZone("elsewhere", 3600))
	again, _ := CanonicalClaims(zone)
	if !bytes.Equal(again, canonical) {
		t.Fatal("timezone changes signed payload")
	}
	for _, payload := range [][]byte{
		append(append([]byte(nil), canonical[:len(canonical)-1]...), []byte(`,"seats":99}`)...),
		append(append([]byte(nil), canonical[:len(canonical)-1]...), []byte(`,"unknown":true}`)...),
		bytes.Replace(canonical, []byte(`"schema":2`), []byte(`"schema":3`), 1),
		bytes.Replace(canonical, []byte(`"edition":"team"`), []byte(`"edition":"unlimited"`), 1),
		append([]byte(" "), canonical...),
	} {
		doc := []byte(`{"claims":` + string(payload) + `,"signature":"` + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, SigningBytes(payload))) + `"}`)
		if _, err := Verify(doc, pub, at); err == nil {
			t.Fatalf("noncanonical/unsupported payload accepted: %s", payload)
		}
	}
}

func FuzzLicenseParser(f *testing.F) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Unix(1800000000, 0)
	good, _ := Issue(Claims{Schema: Schema, Product: "werkbord-team", Edition: EditionTeam, ID: "one", Customer: "org", Seats: 1, IssuedAt: at}, key)
	f.Add(good, []byte(pub))
	f.Add([]byte(`{}`), []byte(pub))
	f.Add([]byte(`null`), []byte(pub))
	f.Fuzz(func(t *testing.T, b, key []byte) {
		c, err := Verify(b, ed25519.PublicKey(key), at)
		if err == nil {
			var d document
			if json.Unmarshal(b, &d) != nil {
				t.Fatal("invalid JSON accepted")
			}
			sig, _ := base64.RawURLEncoding.DecodeString(d.Signature)
			if c.Seats < 1 || !ed25519.Verify(ed25519.PublicKey(key), SigningBytes(d.Claims), sig) {
				t.Fatal("unlicensed acceptance")
			}
		}
	})
}
