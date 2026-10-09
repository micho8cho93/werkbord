package platform

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonHasAServiceLifetimeAndFixedArguments(t *testing.T) {
	root := `/Library/Application Support/Team "test" & copy`
	runner := `/Users/A & B/Library/Application Support/werkbord/config.json`
	b, err := LaunchDaemon(root, runner)
	if err != nil {
		t.Fatal(err)
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	var stringsSeen []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "string" {
			var s string
			if err := d.DecodeElement(&s, &start); err != nil {
				t.Fatal(err)
			}
			stringsSeen = append(stringsSeen, s)
		}
	}
	if stringsSeen[0] != ServiceLabel || stringsSeen[1] != filepath.Join(root, "Helpers", "werkbord-team") || stringsSeen[2] != "daemon" || stringsSeen[8] != runner {
		t.Fatalf("arguments changed or XML injection: %#v", stringsSeen)
	}
	for _, want := range []string{"<key>KeepAlive</key><true/>", "<key>RunAtLoad</key><true/>", "<key>Umask</key><integer>63</integer>"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Fatal("GUI lifetime or unsafe permissions", want)
		}
	}
	if _, err := LaunchDaemon("relative", runner); err == nil {
		t.Fatal("relative privileged path accepted")
	}
}

func TestUninstallSeparatesGUIFromServiceAndData(t *testing.T) {
	for _, p := range []InstallationPlan{{GUIOnly: true}, {StopService: true}, {LeaveWorkspace: true}, {LeaveWorkspace: true, RemoveLocalData: true}} {
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []InstallationPlan{{GUIOnly: true, StopService: true}, {GUIOnly: true, RemoveLocalData: true}, {RemoveLocalData: true}} {
		if err := p.Validate(); err == nil {
			t.Fatal("ambiguous/destructive uninstall plan accepted", p)
		}
	}
}

func TestInstallingAFileDoesNotFollowAnExistingDestinationSymlink(t *testing.T) {
	dir := t.TempDir()
	src, target, dest := filepath.Join(dir, "source"), filepath.Join(dir, "retained"), filepath.Join(dir, "installed")
	_ = os.WriteFile(src, []byte("new"), 0600)
	_ = os.WriteFile(target, []byte("keep"), 0600)
	_ = os.Symlink(target, dest)
	if err := copyFile(src, dest, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "keep" {
		t.Fatal("destination symlink was followed")
	}
	b, _ = os.ReadFile(dest)
	if string(b) != "new" {
		t.Fatal("file not installed")
	}
	fi, _ := os.Lstat(dest)
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0700 {
		t.Fatal(fi.Mode())
	}
}

func TestLicenseIssuerNeverAppearsInServiceArguments(t *testing.T) {
	b, _ := LaunchDaemon(SystemDir, "/Users/test/config.json")
	if strings.Contains(string(b), "issuer") || strings.Contains(string(b), "--shell") || strings.Contains(string(b), "--command") {
		t.Fatal("service acquired generic execution or runtime license override")
	}
}

func TestAWindowRequiresItsMatchingDeviceService(t *testing.T) {
	for _, body := range []string{`{"daemon":true,"version":"2.8.0"}`, `{"daemon":true,"version":"v2.8.0"}`} {
		if err := serviceVersion(strings.NewReader(body), "v2.8.0"); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"daemon":true,"version":"2.7.0"}`, `{"daemon":true}`, `{"version":"2.8.0"}`, `<html>another app</html>`} {
		if err := serviceVersion(strings.NewReader(body), "v2.8.0"); err == nil {
			t.Fatal("an older or unidentified service prevented its update", body)
		}
	}
}

func TestAnIsolatedServiceIsToldNothingOfThePersonsWerkbord(t *testing.T) {
	b, err := LaunchDaemon(SystemDir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"--runner-config", "werkbord/config.json", "/Users/"} {
		if strings.Contains(string(b), banned) {
			t.Fatalf("an isolated service was given %q", banned)
		}
	}
	for _, want := range []string{"--local-key-file", "--data-dir", "<key>KeepAlive</key><true/>"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("lost %q", want)
		}
	}
	// A relative runner path is still refused when one is given.
	if _, err := LaunchDaemon(SystemDir, "relative/config.json"); err == nil {
		t.Fatal("relative runner path accepted")
	}
}

func TestOnlyTheFixedOperationsAreInstallerActions(t *testing.T) {
	for _, a := range []string{"install", "install-isolated", "start", "stop", "uninstall"} {
		if !ValidAction(a) {
			t.Errorf("%s refused", a)
		}
	}
	for _, a := range []string{"", "run", "sh", "install ", "install; rm -rf /", "Install"} {
		if ValidAction(a) {
			t.Errorf("%q accepted", a)
		}
	}
}
