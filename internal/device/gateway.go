package device

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"werkbord/internal/clientpolicy"
	"werkbord/internal/httpkit"
)

type GatewayOptions struct {
	PrepareSwitch func() error
	ResumeSwitch  func()

	Credential  Credential
	Token       string            // this computer's loopback credential, never forwarded
	TokenSource func() string     // live local-token rotation; errors must return an empty value
	Transport   http.RoundTripper // tsnet dial transport, shared with the worker
	Web         http.Handler
	Version     string
	RunnerID    string
}

func Gateway(o GatewayOptions) (http.Handler, error) {
	if o.Transport == nil {
		return nil, errors.New("desktop gateway requires the private network transport")
	}
	currentToken := func() string {
		if o.TokenSource != nil {
			return o.TokenSource()
		}
		return o.Token
	}
	if err := ValidateController(o.Credential.Controller); err != nil {
		return nil, err
	}
	target, _ := url.Parse(o.Credential.Controller)
	proxy := &httputil.ReverseProxy{
		Transport:     o.Transport,
		FlushInterval: -1,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.Host = target.Host
			p.Out.Header.Del("Cookie")
			p.Out.Header.Del("Origin")
			p.Out.Header.Del("Referer")
			p.Out.Header.Del("X-Forwarded-For")
			p.Out.Header.Del("X-Forwarded-Host")
			p.Out.Header.Del("X-Forwarded-Proto")
			p.Out.Header.Set("Authorization", "Bearer "+o.Credential.Token)
			q := p.Out.URL.Query()
			q.Del("access_token")
			p.Out.URL.RawQuery = q.Encode()
		},
		ModifyResponse: func(r *http.Response) error {
			r.Header.Del("Set-Cookie")
			r.Header.Del("Location")
			if r.StatusCode >= 300 && r.StatusCode < 400 {
				r.StatusCode = 502
				r.Status = "502 Bad Gateway"
			}
			r.Header.Set("Cache-Control", "no-store")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			gatewayJSON(w, 503, map[string]any{"error": map[string]string{"code": "controller_offline", "message": "The controller is unavailable. Already-running agents continue on their execution machines. Reconnect and check the outcome before repeating an action."}})
		},
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "loopback host required", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request refused", 403)
				return
			}
		}
		if r.Method == "GET" && r.URL.Path == "/api/health" {
			gatewayJSON(w, 200, map[string]string{"status": "ok", "version": o.Version, "role": "client"})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method == "GET" && o.Web != nil {
				o.Web.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.Path == "/api/events" && token == "" {
			token = r.URL.Query().Get("access_token")
		}
		if want := currentToken(); want == "" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
			gatewayJSON(w, 401, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "local device credential required"}})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/device/switch/resume" {
			if o.ResumeSwitch != nil {
				o.ResumeSwitch()
			}
			gatewayJSON(w, 200, map[string]bool{"ready": true})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/device/switch/prepare" {
			if o.PrepareSwitch != nil {
				if err := o.PrepareSwitch(); err != nil {
					gatewayJSON(w, 409, map[string]any{"error": map[string]string{"code": "conflict", "message": err.Error()}})
					return
				}
			}
			gatewayJSON(w, 200, map[string]bool{"ready": true})
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/device" {
			gatewayJSON(w, 200, map[string]any{"remote": true, "controller": o.Credential.Controller, "clientId": o.Credential.ID, "runnerId": o.RunnerID, "workspaceControl": true, "gitWrites": true, "terminals": true, "assistant": false})
			return
		}
		if !clientpolicy.Allowed(r) {
			gatewayJSON(w, 403, map[string]any{"error": map[string]string{"code": "forbidden", "message": "This operation is not available to this desktop. Manage it on the controller."}})
			return
		}
		if r.URL.Path == "/api/events" && o.TokenSource != nil {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			go func() {
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if currentToken() != token {
							cancel()
							return
						}
					}
				}
			}()
			r = r.WithContext(ctx)
		}
		proxy.ServeHTTP(w, r)
	})
	return httpkit.SecurityHeadersFramed(httpkit.FramedCSP(httpkit.DefaultCSP, nil), h), nil
}
func gatewayJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
