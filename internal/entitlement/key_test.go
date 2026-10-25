package entitlement

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func pair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, key
}

func TestAKeyRoundTripsAndNeverExpires(t *testing.T) {
	pub, key := pair(t)
	long := time.Now().AddDate(-30, 0, 0)
	text, err := Issue(Claims{ID: "pk_test-1", Holder: "Ada Lovelace", IssuedAt: long}, key)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, KeyPrefix) {
		t.Fatalf("a key starts with %q: %s", KeyPrefix, text)
	}
	c, err := Verify("  \n"+text+"\n ", pub)
	if err != nil {
		t.Fatal("a pasted key with spaces round it was refused: ", err)
	}
	if c.ID != "pk_test-1" || c.Holder != "Ada Lovelace" || c.Product != Product || c.Schema != Schema {
		t.Fatalf("claims: %+v", c)
	}
}

func TestAKeyFromAnotherIssuerOrChangedIsRefused(t *testing.T) {
	pub, key := pair(t)
	other, _ := pair(t)
	text, _ := Issue(Claims{ID: "pk_a", IssuedAt: time.Now()}, key)
	if _, err := Verify(text, other); !errors.Is(err, ErrBadSignature) {
		t.Fatal("a key checked against another issuer: ", err)
	}
	body := strings.TrimPrefix(text, KeyPrefix)
	p, s, _ := strings.Cut(body, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(p)
	changed := strings.Replace(string(payload), "pk_a", "pk_b", 1)
	forged := KeyPrefix + base64.RawURLEncoding.EncodeToString([]byte(changed)) + "." + s
	if _, err := Verify(forged, pub); !errors.Is(err, ErrBadSignature) {
		t.Fatal("a changed key was accepted: ", err)
	}
}

func TestOnlyExactlyWhatASignerMakesIsAccepted(t *testing.T) {
	pub, key := pair(t)
	sign := func(payload string) string {
		sig := ed25519.Sign(key, signedBytes([]byte(payload)))
		return KeyPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	good := `{"schema":1,"product":"werkbord","id":"pk_x","issuedAt":"2026-01-01T00:00:00Z"}`
	if _, err := Verify(sign(good), pub); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"an unknown field":    `{"schema":1,"product":"werkbord","id":"pk_x","issuedAt":"2026-01-01T00:00:00Z","seats":5}`,
		"another product":     `{"schema":1,"product":"werkbord-team","id":"pk_x","issuedAt":"2026-01-01T00:00:00Z"}`,
		"another schema":      `{"schema":2,"product":"werkbord","id":"pk_x","issuedAt":"2026-01-01T00:00:00Z"}`,
		"fields out of order": `{"product":"werkbord","schema":1,"id":"pk_x","issuedAt":"2026-01-01T00:00:00Z"}`,
		"a bad id":            `{"schema":1,"product":"werkbord","id":"pk x","issuedAt":"2026-01-01T00:00:00Z"}`,
		"no date":             `{"schema":1,"product":"werkbord","id":"pk_x","issuedAt":"0001-01-01T00:00:00Z"}`,
		"trailing data":       good + ` {}`,
	} {
		if _, err := Verify(sign(payload), pub); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestNotAKeyAndNoIssuer(t *testing.T) {
	pub, key := pair(t)
	text, _ := Issue(Claims{ID: "pk_a", IssuedAt: time.Now()}, key)
	for _, bad := range []string{"", "hello", KeyPrefix, KeyPrefix + "abc", KeyPrefix + "abc.def", strings.Repeat("x", 5000)} {
		if _, err := Verify(bad, pub); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := Verify(text, nil); !errors.Is(err, ErrNoIssuer) {
		t.Fatal("a build with no issuer key checked a key: ", err)
	}
	if _, err := VerifyKey(text); !errors.Is(err, ErrNoIssuer) {
		t.Fatal("a build with no stamped key checked a key: ", err)
	}
	if _, err := Issue(Claims{ID: "bad id", IssuedAt: time.Now()}, key); err == nil {
		t.Fatal("issued a key with an invalid ID")
	}
}
