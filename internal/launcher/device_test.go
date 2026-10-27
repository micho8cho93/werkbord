package launcher

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/device"
)

func TestRemoteDesktopStartsOnlyDeviceRuntimeAndOpensWhileControllerOffline(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v5.5.3-preview.1")
	state := device.State{Mode: "remote", Controller: "http://home.test", Runner: true}
	if err := device.Save(w.cfg.DataDir, state); err != nil {
		t.Fatal(err)
	}
	token, err := w.cfg.ResolveToken(true)
	if err != nil {
		t.Fatal(err)
	}
	l := w.launcher(Options{Version: "v5.5.3-preview.1"})
	original := l.opt.Run
	started := 0
	var server *http.Server
	l.opt.Run = func(ctx context.Context, env []string, name string, args ...string) (string, error) {
		if strings.Join(args, " ") == "device start" {
			started++
			ln, err := net.Listen("tcp", w.cfg.Addr)
			if err != nil {
				return "", err
			}
			server = &http.Server{Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/health":
					_ = json.NewEncoder(rw).Encode(map[string]string{"status": "ok", "version": "v5.5.3-preview.1"})
				case "/api/device":
					if r.Header.Get("Authorization") != "Bearer "+token {
						rw.WriteHeader(401)
						return
					}
					_ = json.NewEncoder(rw).Encode(map[string]any{"remote": true, "controller": state.Controller})
				default:
					t.Errorf("launcher contacted remote controller route %s", r.URL.Path)
					rw.WriteHeader(503)
				}
			})}
			go server.Serve(ln)
			return "", nil
		}
		return original(ctx, env, name, args...)
	}
	defer func() {
		if server != nil {
			server.Close()
		}
	}()
	for range 2 {
		conn, err := l.Connect(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(conn.SignInURL, "#token=") {
			t.Fatal("gateway was not authenticated")
		}
	}
	if started != 1 || w.ctrl != nil {
		t.Fatal("unexpected controller or duplicate runtime")
	}
	if err := l.UpdateSafety(bg); err != nil {
		t.Fatal("remote controller availability affected local update guard", err)
	}
}
