package daemon

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"text/template"
)

// Launchd runs the controller as a launchd user agent: it starts when the user
// logs in, is restarted if it crashes, and `launchctl bootout` stops it.
type Launchd struct{ Options }

var _ Manager = (*Launchd)(nil)

func (l *Launchd) Name() string { l.fill(); return "launchd" }

func (l *Launchd) plistPath() string {
	return filepath.Join(l.Home, "Library", "LaunchAgents", l.Label+".plist")
}

func (l *Launchd) target() string { return fmt.Sprintf("gui/%d/%s", l.UID, l.Label) }

func (l *Launchd) domain() string { return fmt.Sprintf("gui/%d", l.UID) }

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{"x": xmlEscape}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{x .Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{x .Binary}}</string>
{{- range .Args}}
		<string>{{x .}}</string>
{{- end}}
	</array>
	<key>EnvironmentVariables</key>
	<dict>
{{- range .Env}}
		<key>{{x .Key}}</key>
		<string>{{x .Value}}</string>
{{- end}}
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ExitTimeOut</key>
	<integer>30</integer>
	<key>StandardOutPath</key>
	<string>{{x .Log}}</string>
	<key>StandardErrorPath</key>
	<string>{{x .Log}}</string>
</dict>
</plist>
`))

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type envPair struct{ Key, Value string }

// serviceEnv is the environment the service runs with, in a stable order.
func serviceEnv(spec Spec) []envPair {
	env := map[string]string{"PATH": spec.Path}
	if spec.DataDir != "" {
		env["DEVBOARD_DATA_DIR"] = spec.DataDir
	}
	keys := make([]string, 0, len(env))
	for k, v := range env {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]envPair, 0, len(keys))
	for _, k := range keys {
		out = append(out, envPair{k, env[k]})
	}
	return out
}

// Plist renders the launch agent definition.
func (l *Launchd) Plist(spec Spec) (string, error) {
	l.fill()
	var b bytes.Buffer
	err := plistTmpl.Execute(&b, map[string]any{
		"Label": l.Label, "Binary": spec.Binary, "Args": spec.Args, "Env": serviceEnv(spec), "Log": spec.LogFile,
	})
	return b.String(), err
}

func (l *Launchd) Install(ctx context.Context, spec Spec) error {
	l.fill()
	body, err := l.Plist(spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.plistPath()), 0o755); err != nil {
		return err
	}
	if spec.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(spec.LogFile), 0o700); err != nil {
			return err
		}
		rotateLog(spec.LogFile, LogRotateSize)
	}
	return os.WriteFile(l.plistPath(), []byte(body), 0o644)
}

func (l *Launchd) installed() bool {
	_, err := os.Stat(l.plistPath())
	return err == nil
}

func (l *Launchd) loaded(ctx context.Context) (out string, ok bool) {
	out, err := l.Exec(ctx, "launchctl", "print", l.target())
	return out, err == nil
}

func (l *Launchd) Start(ctx context.Context) error {
	l.fill()
	if !l.installed() {
		return ErrNotInstalled
	}
	if _, ok := l.loaded(ctx); ok {
		// Loaded but perhaps stopped (it exited cleanly): kickstart starts it, and
		// does nothing to one that is running.
		if out, err := l.Exec(ctx, "launchctl", "kickstart", l.target()); err != nil {
			return errorf("launchd", "start", out, err)
		}
		return nil
	}
	if out, err := l.Exec(ctx, "launchctl", "bootstrap", l.domain(), l.plistPath()); err != nil {
		return errorf("launchd", "start", out, err)
	}
	return nil
}

func (l *Launchd) Stop(ctx context.Context) error {
	l.fill()
	if _, ok := l.loaded(ctx); !ok {
		return nil // not loaded: already stopped
	}
	if out, err := l.Exec(ctx, "launchctl", "bootout", l.target()); err != nil {
		return errorf("launchd", "stop", out, err)
	}
	return nil
}

func (l *Launchd) Restart(ctx context.Context) error {
	if err := l.Stop(ctx); err != nil {
		return err
	}
	return l.Start(ctx)
}

func (l *Launchd) Uninstall(ctx context.Context) error {
	l.fill()
	if err := l.Stop(ctx); err != nil {
		return err
	}
	if err := os.Remove(l.plistPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var (
	launchdPID   = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)
	launchdState = regexp.MustCompile(`(?m)^\s*state = (\w+)`)
)

func (l *Launchd) Status(ctx context.Context) (State, error) {
	l.fill()
	st := State{Manager: "launchd", Installed: l.installed()}
	out, ok := l.loaded(ctx)
	if !ok {
		return st, nil
	}
	if m := launchdState.FindStringSubmatch(out); m != nil {
		st.Running = m[1] == "running"
		if !st.Running {
			st.Detail = "launchd says " + m[1]
		}
	}
	if m := launchdPID.FindStringSubmatch(out); m != nil {
		st.PID, _ = strconv.Atoi(m[1])
		st.Running = st.PID > 0
	}
	return st, nil
}
