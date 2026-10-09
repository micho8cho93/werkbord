package platform

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// LaunchDaemon is fixed to Team's own program and sidecars. Data and keys stay in a root-owned directory.
// The runner connection is still loopback, scoped and authenticated; this service cannot execute developer work.
//
// With an empty runnerConfig the service is told nothing about the person's own Werkbord: it never reads its settings or its
// credential. A runner is then connected only by the person's own app handing it a narrow, revocable grant (the Werkbord
// desktop app does this); that is the arrangement that keeps the privileged service away from the person's credentials.
func LaunchDaemon(root, runnerConfig string) ([]byte, error) {
	if !filepath.IsAbs(root) || (runnerConfig != "" && !filepath.IsAbs(runnerConfig)) {
		return nil, errors.New("service paths must be absolute")
	}
	esc := func(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	args := []string{filepath.Join(root, "Helpers", "werkbord-team"), "daemon", "--data-dir", filepath.Join(root, "data"), "--local-key-file", filepath.Join(root, "access.key")}
	if runnerConfig != "" {
		args = append(args, "--runner-config", runnerConfig)
	}
	var argv bytes.Buffer
	for _, a := range args {
		fmt.Fprintf(&argv, "<string>%s</string>", esc(a))
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array>%s</array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>ThrottleInterval</key><integer>10</integer><key>ExitTimeOut</key><integer>90</integer>
<key>WorkingDirectory</key><string>%s</string>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
<key>Umask</key><integer>63</integer>
</dict></plist>`, ServiceLabel, argv.String(), esc(root), esc(filepath.Join(root, "service.log")), esc(filepath.Join(root, "service.log")))), nil
}

// Actions the native installer performs with administrator rights, and only these.
const (
	ActionInstall = "install"
	// ActionInstallIsolated installs the service the way the Werkbord desktop app wants it: with no path to the person's
	// own Werkbord, so that the service cannot read its credential. See LaunchDaemon.
	ActionInstallIsolated = "install-isolated"
	ActionStart           = "start"
	ActionStop            = "stop"
	ActionUninstall       = "uninstall"
)

// ValidAction reports whether a is one of the installer's fixed operations.
func ValidAction(a string) bool {
	switch a {
	case ActionInstall, ActionInstallIsolated, ActionStart, ActionStop, ActionUninstall:
		return true
	}
	return false
}

// InstallationPlan is platform independent, so Windows and Linux service implementations can share semantics.
type InstallationPlan struct {
	GUIOnly         bool
	StopService     bool
	LeaveWorkspace  bool
	RemoveLocalData bool
}

func (p InstallationPlan) Validate() error {
	if p.GUIOnly && (p.StopService || p.LeaveWorkspace || p.RemoveLocalData) {
		return errors.New("removing only the GUI leaves its service and data intact")
	}
	if p.RemoveLocalData && !p.LeaveWorkspace {
		return errors.New("leave the workspace safely before removing local secrets or data")
	}
	return nil
}

// osascriptFraming is what AppleScript wraps around a failed "do shell script": a source position in front and the exit status
// behind ("0:279: execution error: <what the program printed> (1)"). It says nothing the person can use.
var osascriptFraming = regexp.MustCompile(`^\d+:\d+: execution error: |\s*\(\d+\)$`)

// installerDetail is what the installer itself said, without AppleScript's framing.
func installerDetail(out string) string {
	return strings.TrimSpace(osascriptFraming.ReplaceAllString(strings.TrimSpace(out), ""))
}

// installerRefusal turns what a failed installer run printed into the error the person reads. A refusal to touch a service
// that belongs to a workspace is the installer working as designed, so it is given as itself; anything else keeps what the
// installer said, under a name for what was being done. A refusal joined with another failure (a rollback that did not
// complete, say) is not hidden behind the refusal.
func installerRefusal(action, out string) error {
	detail := installerDetail(out)
	if detail == ErrReplacementDeferred.Error() {
		return ErrReplacementDeferred
	}
	what := map[string]string{ActionInstall: "install or update", ActionInstallIsolated: "install or update", ActionStart: "start", ActionStop: "stop", ActionUninstall: "remove"}[action]
	if what == "" {
		what = action
	}
	return fmt.Errorf("macOS could not %s the Team service: %s", what, detail)
}
