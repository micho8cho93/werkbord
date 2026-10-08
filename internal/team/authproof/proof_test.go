package authproof

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type testSigner struct{ key ed25519.PrivateKey }

func (s testSigner) DeviceID() string     { return "dev_a" }
func (s testSigner) Sign(b []byte) []byte { return ed25519.Sign(s.key, b) }

func TestProofBindsEveryRequestAndIdentityField(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Unix(1800000000, 0)
	body := []byte(`{"name":"one"}`)
	request := func() *http.Request {
		r, _ := http.NewRequest("POST", "http://10.128.0.1:7430/api/team/v1/projects?x=1", nil)
		return r
	}
	good := request()
	if err := Sign(good, body, testSigner{key}, "ws_a", "user_a", now); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(good, body, pub, "ws_a", "user_a", "dev_a", now); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		alter         func(*http.Request)
		body          []byte
		ws, user, dev string
		at            time.Time
	}{
		{"method", func(r *http.Request) { r.Method = "DELETE" }, body, "ws_a", "user_a", "dev_a", now},
		{"target", func(r *http.Request) { r.Host = "10.128.0.2:7430" }, body, "ws_a", "user_a", "dev_a", now},
		{"path", func(r *http.Request) { r.URL.Path += "/other" }, body, "ws_a", "user_a", "dev_a", now},
		{"query", func(r *http.Request) { r.URL.RawQuery = "x=2" }, body, "ws_a", "user_a", "dev_a", now},
		{"body", nil, []byte(`{"name":"two"}`), "ws_a", "user_a", "dev_a", now},
		{"workspace", nil, body, "ws_b", "user_a", "dev_a", now},
		{"user", nil, body, "ws_a", "user_b", "dev_a", now},
		{"device", nil, body, "ws_a", "user_a", "dev_b", now},
		{"expired", nil, body, "ws_a", "user_a", "dev_a", now.Add(Lifetime + Skew + time.Millisecond)},
		{"future", nil, body, "ws_a", "user_a", "dev_a", now.Add(-Skew - time.Millisecond)},
		{"duplicate", func(r *http.Request) { r.Header.Add(Header, r.Header.Get(Header)) }, body, "ws_a", "user_a", "dev_a", now},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := request()
			r.Header = good.Header.Clone()
			if test.alter != nil {
				test.alter(r)
			}
			if _, err := Verify(r, test.body, pub, test.ws, test.user, test.dev, test.at); err == nil {
				t.Fatal("altered request authorized")
			}
		})
	}
	raw, _ := base64.RawURLEncoding.DecodeString(good.Header.Get(Header))
	var p Proof
	_ = json.Unmarshal(raw, &p)
	for _, alter := range []func(*Proof){func(p *Proof) { p.Version = 2 }, func(p *Proof) { p.Issued = -1; p.Expires = 1<<63 - 1 }, func(p *Proof) { p.Expires = p.Issued + Lifetime.Milliseconds() + 1 }, func(p *Proof) { p.Nonce = "bad" }} {
		q := p
		alter(&q)
		q.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, q.signingBytes()))
		b, _ := json.Marshal(q)
		r := request()
		r.Header.Set(Header, base64.RawURLEncoding.EncodeToString(b))
		if _, err := Verify(r, body, pub, "ws_a", "user_a", "dev_a", now); err == nil {
			t.Fatal("invalid signed proof accepted")
		}
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := Verify(good, body, other, "ws_a", "user_a", "dev_a", now); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func FuzzProofParser(f *testing.F) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Unix(1800000000, 0)
	r, _ := http.NewRequest("GET", "http://10.128.0.1:7430/api/team/v1/me", nil)
	_ = Sign(r, nil, testSigner{key}, "ws", "user", at)
	f.Add(r.Header.Get(Header), []byte(pub))
	f.Add("", []byte(pub))
	f.Add("e30", []byte(pub))
	f.Fuzz(func(t *testing.T, s string, key []byte) {
		r, _ := http.NewRequest("GET", "http://10.128.0.1:7430/api/team/v1/me", nil)
		r.Header.Set(Header, s)
		p, err := Verify(r, nil, ed25519.PublicKey(key), "ws", "user", "dev_a", at)
		if err == nil {
			sig, _ := base64.RawURLEncoding.DecodeString(p.Signature)
			if !ed25519.Verify(ed25519.PublicKey(key), p.signingBytes(), sig) {
				t.Fatal("accepted unsigned proof")
			}
		}
	})
}
