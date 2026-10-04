package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// Systemd runs the controller as a systemd user service: it starts when the user
// logs in (or at boot, if lingering is on), is restarted if it fails, and
// `systemctl --user stop` stops it. It needs no root.
type Systemd struct{ Options }

var _ Manager = (*Systemd)(nil)

func (s *Systemd) Name() string { s.fill(); return "systemd" }

func (s *Systemd) unitName() string { return "devboard.service" }

func (s *Systemd) unitPath() string {
	dir := filepath.Join(s.Home, ".config")
	if real, _ := os.UserHomeDir(); s.Home == real && os.Getenv("XDG_CONFIG_HOME") != "" {
		dir = os.Getenv("XDG_CONFIG_HOME")
	}
	return filepath.Join(dir, "systemd", "user", s.unitName())
}

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=Dev Board controller
Documentation=https://github.com/micho8cho93/dev-board
After=network-online.target

[Service]
Type=simple
ExecStart={{.Exec}}
{{- range .Env}}
Environment={{.}}
{{- end}}
Restart=on-failure
RestartSec=5
TimeoutStopSec=30
{{- if .Log}}
StandardOutput=append:{{.Log}}
StandardError=append:{{.Log}}
{{- end}}

[Install]
WantedBy=default.target
`))

// quoteUnit quotes a word for ExecStart and Environment lines.
func quoteUnit(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$%") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(s) + `"`
}

// Unit renders the unit file.
func (s *Systemd) Unit(spec Spec) (string, error) {
	s.fill()
	words := []string{quoteUnit(spec.Binary)}
	for _, a := range spec.Args {
		words = append(words, quoteUnit(a))
	}
	var env []string
	for _, kv := range serviceEnv(spec) {
		env = append(env, quoteUnit(kv.Key+"="+kv.Value))
	}
	var b bytes.Buffer
	err := unitTmpl.Execute(&b, map[string]any{"Exec": strings.Join(words, " "), "Env": env, "Log": spec.LogFile})
	return b.String(), err
}

func (s *Systemd) systemctl(ctx context.Context, what string, args ...string) (string, error) {
	out, err := s.Exec(ctx, "systemctl", append([]string{"--user"}, args...)...)
	if err != nil {
		return out, errorf("systemd", what, out, err)
	}
	return out, nil
}

func (s *Systemd) Install(ctx context.Context, spec Spec) error {
	s.fill()
	body, err := s.Unit(spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.unitPath()), 0o755); err != nil {
		return err
	}
	if spec.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(spec.LogFile), 0o700); err != nil {
			return err
		}
		rotateLog(spec.LogFile, LogRotateSize)
	}
	if err := os.WriteFile(s.unitPath(), []byte(body), 0o644); err != nil {
		return err
	}
	if _, err := s.systemctl(ctx, "reload", "daemon-reload"); err != nil {
		return err
	}
	if _, err := s.systemctl(ctx, "enable", "enable", s.unitName()); err != nil {
		return err
	}
	// Without lingering a user service stops when the last session ends and does
	// not start at boot. It is usually allowed for one's own account; where it is
	// not, the service still works while the user is logged in.
	if user := os.Getenv("USER"); user != "" {
		_, _ = s.Exec(ctx, "loginctl", "enable-linger", user)
	}
	return nil
}

func (s *Systemd) installed() bool {
	_, err := os.Stat(s.unitPath())
	return err == nil
}

func (s *Systemd) Start(ctx context.Context) error {
	s.fill()
	if !s.installed() {
		return ErrNotInstalled
	}
	_, err := s.systemctl(ctx, "start", "start", s.unitName())
	return err
}

func (s *Systemd) Stop(ctx context.Context) error {
	s.fill()
	if !s.installed() {
		return nil
	}
	_, err := s.systemctl(ctx, "stop", "stop", s.unitName())
	return err
}

func (s *Systemd) Restart(ctx context.Context) error {
	s.fill()
	if !s.installed() {
		return ErrNotInstalled
	}
	_, err := s.systemctl(ctx, "restart", "restart", s.unitName())
	return err
}

func (s *Systemd) Uninstall(ctx context.Context) error {
	s.fill()
	if s.installed() {
		_, _ = s.systemctl(ctx, "stop", "stop", s.unitName())
		_, _ = s.systemctl(ctx, "disable", "disable", s.unitName())
		if err := os.Remove(s.unitPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		_, _ = s.systemctl(ctx, "reload", "daemon-reload")
	}
	return nil
}

func (s *Systemd) Status(ctx context.Context) (State, error) {
	s.fill()
	st := State{Manager: "systemd", Installed: s.installed()}
	if !st.Installed {
		return st, nil
	}
	out, err := s.Exec(ctx, "systemctl", "--user", "show", s.unitName(), "--property=ActiveState,SubState,MainPID")
	if err != nil {
		return st, fmt.Errorf("systemd: status: %w", err)
	}
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	st.Running = props["ActiveState"] == "active"
	st.PID, _ = strconv.Atoi(props["MainPID"])
	if !st.Running && props["ActiveState"] != "" && props["ActiveState"] != "inactive" {
		st.Detail = "systemd says " + props["ActiveState"] + " (" + props["SubState"] + ")"
	}
	return st, nil
}
