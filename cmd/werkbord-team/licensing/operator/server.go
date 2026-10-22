package operator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"devboard/internal/team/license"
)

//go:embed assets/*
var assets embed.FS

type Config struct {
	SupabaseURL       string `json:"supabaseURL"`
	PublishableKey    string `json:"publishableKey"`
	IssuerPublicKey   string `json:"issuerPublicKey"`
	IssuerFingerprint string `json:"issuerFingerprint"`
}

func (c Config) Validate() error {
	u, err := url.Parse(c.SupabaseURL)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".supabase.co") || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("configure the HTTPS URL of the dedicated Supabase project")
	}
	key, err := base64.RawURLEncoding.DecodeString(c.IssuerPublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || Fingerprint(key) != c.IssuerFingerprint {
		return errors.New("public issuer fingerprint mismatch")
	}
	if !strings.HasPrefix(c.PublishableKey, "sb_publishable_") {
		return errors.New("a Supabase publishable key is required; never use an administrative key")
	}
	return nil
}

type Server struct {
	Config   Config
	Origin   string
	Client   *http.Client
	Now      func() time.Time
	mu       sync.Mutex
	key      ed25519.PrivateKey
	expires  time.Time
	keyTimer *time.Timer
}

func NewServer(c Config, key ed25519.PrivateKey, origin string) (*Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" {
		return nil, errors.New("operator console must listen on an explicit IPv4 loopback port")
	}
	if len(key) > 0 && (len(key) != ed25519.PrivateKeySize || Fingerprint(key.Public().(ed25519.PublicKey)) != c.IssuerFingerprint) {
		return nil, errors.New("wrong signing key")
	}
	s := &Server{Config: c, Origin: origin, Client: &http.Client{Timeout: 25 * time.Second}, Now: time.Now, key: append(ed25519.PrivateKey(nil), key...), expires: time.Now().Add(30 * time.Minute)}
	// Credentials and customer data must never follow an HTTP redirect.
	s.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s.keyTimer = time.AfterFunc(30*time.Minute, s.Close)
	return s, nil
}
func (s *Server) Close() { s.mu.Lock(); defer s.mu.Unlock(); clear(s.key); s.key = nil }
func (s *Server) activeKey() (ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.key) == 0 || !s.Now().Before(s.expires) {
		clear(s.key)
		s.key = nil
		if s.keyTimer != nil {
			s.keyTimer.Stop()
			s.keyTimer = nil
		}
		return nil, errors.New("signing is locked; restart the console and unlock the encrypted issuer key")
	}
	return append(ed25519.PrivateKey(nil), s.key...), nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self' %s; frame-ancestors 'none'; base-uri 'none'; form-action 'self'", s.Config.SupabaseURL))
	expected, _ := url.Parse(s.Origin)
	if r.Host != expected.Host {
		http.Error(w, "Unexpected host", http.StatusForbidden)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method == http.MethodGet && r.URL.Path == "/api/config" {
			s.mu.Lock()
			unlocked := len(s.key) > 0 && s.Now().Before(s.expires)
			s.mu.Unlock()
			writeJSON(w, map[string]any{"config": s.Config, "signingUnlocked": unlocked, "signingExpiresAt": s.expires})
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Origin") != s.Origin || r.Header.Get("X-Werkbord-Operator") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "Same-origin JSON requests are required", http.StatusForbidden)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || len(auth) > 8192 || len(auth) < 32 {
			http.Error(w, "Sign in and verify your authenticator code", http.StatusUnauthorized)
			return
		}
		var err error
		switch r.URL.Path {
		case "/api/sign":
			err = s.sign(w, r, auth)
		case "/api/import":
			err = s.importArtifact(w, r, auth)
		case "/api/backup":
			err = s.backup(w, r, auth)
		case "/api/restore":
			err = s.restore(w, r, auth)
		case "/api/lock":
			// Verify the live operator session before changing the signer's state.
			_, err = s.call(r.Context(), "/rest/v1/rpc/licensing_export", auth, map[string]any{})
			if err == nil {
				s.Close()
				writeJSON(w, map[string]bool{"locked": true})
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	files, _ := fs.Sub(assets, "assets")
	http.FileServer(http.FS(files)).ServeHTTP(w, r)
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request, auth string) error {
	var in struct {
		Encrypted    string `json:"encrypted"`
		Passphrase   string `json:"passphrase"`
		ConfirmEmpty bool   `json:"confirmEmpty"`
	}
	if err := decode(r, &in, 15<<20); err != nil {
		return err
	}
	if !in.ConfirmEmpty {
		return errors.New("confirm that the destination register is empty")
	}
	encrypted, err := base64.StdEncoding.DecodeString(in.Encrypted)
	if err != nil || len(encrypted) > 10<<20 {
		return errors.New("invalid encrypted backup")
	}
	raw, err := Decrypt(encrypted, in.Passphrase, 10<<20)
	if err != nil {
		return err
	}
	defer clear(raw)
	pub, _ := base64.RawURLEncoding.DecodeString(s.Config.IssuerPublicKey)
	if _, err = VerifySnapshot(raw, []ed25519.PublicKey{pub}); err != nil {
		return err
	}
	out, err := s.call(r.Context(), "/functions/v1/licensing-artifact", auth, map[string]any{"action": "restore", "snapshot": json.RawMessage(raw), "sourceSHA256": Hash(encrypted)})
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(out)
	return err
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any, max int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, max+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid request")
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("unexpected request data")
	}
	return nil
}
func (s *Server) call(ctx context.Context, path, auth string, input any) ([]byte, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.SupabaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", s.Config.PublishableKey)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, errors.New("Supabase could not be reached; refresh the register before retrying an uncertain operation")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		message := apiErr.Message
		if message == "" {
			message = apiErr.Error
		}
		if message == "" || len(message) > 500 {
			message = "The register rejected the operation; check your operator session and refresh before retrying"
		}
		return nil, errors.New(message)
	}
	return body, nil
}
func (s *Server) sign(w http.ResponseWriter, r *http.Request, auth string) error {
	var in struct {
		OperationID string `json:"operationId"`
	}
	if err := decode(r, &in, 4096); err != nil {
		return err
	}
	raw, err := s.call(r.Context(), "/rest/v1/rpc/licensing_operation", auth, map[string]any{"p_id": in.OperationID})
	if err != nil {
		return err
	}
	var op Operation
	if json.Unmarshal(raw, &op) != nil || op.ID != in.OperationID || op.ID == "" || (op.Kind != "issue" && op.Kind != "expand") || (op.State != "pending" && op.State != "completed") {
		return errors.New("no signable operator request was found")
	}
	key, err := s.activeKey()
	if err != nil {
		return err
	}
	defer clear(key)
	artifact, err := license.Issue(op.Claims, key)
	if err != nil {
		return err
	}
	artifact = append(artifact, '\n')
	if _, err := license.Verify(artifact, key.Public().(ed25519.PublicKey), s.Now()); err != nil {
		return err
	}
	out, err := s.call(r.Context(), "/functions/v1/licensing-artifact", auth, map[string]any{"action": "complete", "operationId": op.ID, "document": string(artifact)})
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(out)
	return err
}
func (s *Server) importArtifact(w http.ResponseWriter, r *http.Request, auth string) error {
	var in struct {
		Document string `json:"document"`
		Source   string `json:"source"`
	}
	if err := decode(r, &in, 32768); err != nil {
		return err
	}
	if len(in.Document) > 16384 {
		return errors.New("license file is too large")
	}
	var envelope struct {
		Claims license.Claims `json:"claims"`
	}
	if json.Unmarshal([]byte(in.Document), &envelope) != nil {
		return errors.New("choose an original signed license JSON file")
	}
	pub, _ := base64.RawURLEncoding.DecodeString(s.Config.IssuerPublicKey)
	if err := VerifyArtifact(Artifact{LicenseID: envelope.Claims.ID, EntitlementID: "import", OperationID: "import", Revision: 1, Claims: envelope.Claims, Document: in.Document, SHA256: Hash([]byte(in.Document)), IssuerFingerprint: s.Config.IssuerFingerprint}, []ed25519.PublicKey{pub}); err != nil {
		return err
	}
	out, err := s.call(r.Context(), "/functions/v1/licensing-artifact", auth, map[string]any{"action": "import", "document": in.Document, "source": in.Source})
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(out)
	return err
}
func (s *Server) backup(w http.ResponseWriter, r *http.Request, auth string) error {
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if err := decode(r, &in, 4096); err != nil {
		return err
	}
	raw, err := s.call(r.Context(), "/rest/v1/rpc/licensing_export", auth, map[string]any{})
	if err != nil {
		return err
	}
	defer clear(raw)
	key, _ := base64.RawURLEncoding.DecodeString(s.Config.IssuerPublicKey)
	if _, err = VerifySnapshot(raw, []ed25519.PublicKey{key}); err != nil {
		return fmt.Errorf("backup verification failed: %w", err)
	}
	encrypted, err := Encrypt(raw, in.Passphrase)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="werkbord-register-%s.json.age"`, s.Now().UTC().Format("20060102-150405")))
	_, err = w.Write(encrypted)
	return err
}
