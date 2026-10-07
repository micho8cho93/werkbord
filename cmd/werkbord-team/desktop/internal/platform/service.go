package platform

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
)

// LaunchDaemon is fixed to Team's own program and sidecars. Data and keys stay in a root-owned directory.
// The runner connection is still loopback, scoped and authenticated; this service cannot execute developer work.
func LaunchDaemon(root, runnerConfig string) ([]byte, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(runnerConfig) {
		return nil, errors.New("service paths must be absolute")
	}
	esc := func(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	args := []string{filepath.Join(root, "Helpers", "werkbord-team"), "daemon", "--data-dir", filepath.Join(root, "data"), "--local-key-file", filepath.Join(root, "access.key"), "--runner-config", runnerConfig}
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
