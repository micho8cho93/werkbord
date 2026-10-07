package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestOnlyAnActiveLicenseFromTheConfiguredIssuerCanActivateTeam(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	claims := Claims{Product: "werkbord-team", ID: "lic_customer", Customer: "Customer", Seats: 5, IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	makeDocument := func(c Claims) []byte {
		raw, _ := json.MarshalIndent(c, "", "  ") // The issuer may sign formatted claims; their bytes must survive import.
		sig := base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, SigningBytes(raw)))
		return []byte(`{"claims":` + string(raw) + `,"signature":"` + sig + `"}`)
	}
	good := makeDocument(claims)
	got, err := Verify(good, pub, now)
	if err != nil || got.ID != claims.ID || got.Seats != 5 {
		t.Fatalf("license: %+v %v", got, err)
	}
	wrongIssuer, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := Verify(good, wrongIssuer, now); err == nil {
		t.Fatal("a different issuer activated Team")
	}
	if _, err := Verify(good, nil, now); err == nil {
		t.Fatal("an unconfigured build activated Team")
	}
	if _, err := Verify(append(good, []byte(` {}`)...), pub, now); err == nil {
		t.Fatal("extra data was accepted")
	}
	for _, mutate := range []func(*Claims){
		func(c *Claims) { c.Product = "werkbord" },
		func(c *Claims) { c.Seats = 0 },
		func(c *Claims) { c.ExpiresAt = now.Add(-time.Minute) },
		func(c *Claims) { c.IssuedAt = now.Add(time.Hour); c.ExpiresAt = now.Add(2 * time.Hour) },
	} {
		bad := claims
		mutate(&bad)
		if _, err := Verify(makeDocument(bad), pub, now); err == nil {
			t.Fatalf("invalid claims accepted: %+v", bad)
		}
	}
	changed := append([]byte(nil), good...)
	for i, b := range changed {
		if b == '5' {
			changed[i] = '6'
			break
		}
	}
	if _, err := Verify(changed, pub, now); err == nil {
		t.Fatal("tampered license activated Team")
	}
}
