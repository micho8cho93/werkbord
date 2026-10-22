package operator

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConsoleSignsOnlyReservedClaimsAndChecksBrowserOrigin(t *testing.T) {
	pub, key, a, _ := fixture(t)
	defer clear(key)
	a.Claims.ExpiresAt = time.Time{}
	cfg := Config{SupabaseURL: "https://fixture.supabase.co", PublishableKey: "sb_publishable_fixture", IssuerPublicKey: base64.RawURLEncoding.EncodeToString(pub), IssuerFingerprint: Fingerprint(pub)}
	s, err := NewServer(cfg, key, "http://127.0.0.1:9999")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls := 0
	s.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		var body []byte
		if r.Header.Get("Authorization") != "Bearer fixture-authenticated-operator-token" {
			t.Fatal("lost caller's identity")
		}
		if strings.HasSuffix(r.URL.Path, "licensing_operation") {
			body, _ = json.Marshal(Operation{ID: "op_fixture", Kind: "issue", State: "pending", Claims: a.Claims})
		} else {
			var in struct {
				Document string `json:"document"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			var doc struct {
				Claims struct {
					Seats int `json:"seats"`
				} `json:"claims"`
			}
			_ = json.Unmarshal([]byte(in.Document), &doc)
			if doc.Claims.Seats != 3 {
				t.Fatal("unreserved terms signed")
			}
			body = []byte(`{"recorded":true}`)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header), Request: r}, nil
	})
	send := func(origin, host, body string) int {
		r := httptest.NewRequest("POST", "http://127.0.0.1:9999/api/sign", strings.NewReader(body))
		r.Host = host
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Werkbord-Operator", "1")
		r.Header.Set("Authorization", "Bearer fixture-authenticated-operator-token")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if code := send("https://attacker.example", "127.0.0.1:9999", `{"operationId":"op_fixture"}`); code != 403 {
		t.Fatal(code)
	}
	if code := send(s.Origin, "attacker.example", `{"operationId":"op_fixture"}`); code != 403 {
		t.Fatal(code)
	}
	if calls != 0 {
		t.Fatal("cross-origin request reached register")
	}
	if code := send(s.Origin, "127.0.0.1:9999", `{"operationId":"op_fixture","seats":100}`); code != 400 {
		t.Fatal(code)
	}
	if code := send(s.Origin, "127.0.0.1:9999", `{"operationId":"op_fixture"}`); code != 200 {
		t.Fatal(code)
	}
	s.Now = func() time.Time { return time.Now().Add(time.Hour) }
	if code := send(s.Origin, "127.0.0.1:9999", `{"operationId":"op_fixture"}`); code != 400 {
		t.Fatal("expired signing session accepted", code)
	}
}
func TestConfigurationRejectsAdministrativeKeysAndNonTLS(t *testing.T) {
	pub, key, _, _ := fixture(t)
	defer clear(key)
	cfg := Config{SupabaseURL: "https://fixture.supabase.co", PublishableKey: "sb_publishable_fixture", IssuerPublicKey: base64.RawURLEncoding.EncodeToString(pub), IssuerFingerprint: Fingerprint(pub)}
	for _, url := range []string{"http://fixture.supabase.co", "https://attacker.example", "https://fixture.supabase.co/extra", "https://fixture.supabase.co?token=secret", "https://user:password@fixture.supabase.co"} {
		c := cfg
		c.SupabaseURL = url
		if c.Validate() == nil {
			t.Fatal(url)
		}
	}
	cfg.PublishableKey = "sb_secret_should-never-enter-browser"
	if cfg.Validate() == nil {
		t.Fatal("administrative key accepted")
	}
}
