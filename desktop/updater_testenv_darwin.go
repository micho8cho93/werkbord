//go:build darwin && updatertest

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// The test build (go build -tags updatertest) is hermetic by construction, because the system, not a script, relaunches it
// after an update, and a relaunched app has none of the environment its test gave it. So it takes its environment from
// the file Info.plist names (WBTestEnvFile), which scripts/test-desktop-update.sh writes for each scenario, uses its own
// bundle identifier and single-instance lock, and REFUSES TO RUN AT ALL unless that environment puts it in a home
// directory that is not the real one. Nothing a test build does can reach the installation of the person who built it.
func init() {
	fail := func(why string) {
		fmt.Fprintln(os.Stderr, "werkbord (updater test build): refusing to run:", why)
		os.Exit(3)
	}
	exe, err := os.Executable()
	if err != nil {
		fail(err.Error())
	}
	plist := filepath.Join(filepath.Dir(exe), "..", "Info.plist")
	out, err := exec.Command("/usr/bin/plutil", "-extract", "WBTestEnvFile", "raw", "-o", "-", plist).Output()
	file := strings.TrimSpace(string(out))
	if err != nil || file == "" {
		fail("its Info.plist names no WBTestEnvFile")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		fail("the environment file " + file + " cannot be read: " + err.Error())
	}
	env := map[string]string{}
	if json.Unmarshal(b, &env) != nil {
		fail("the environment file is not a JSON object of strings")
	}
	for k, v := range env {
		os.Setenv(k, v)
	}
	real, err := user.Current()
	if err != nil || real.HomeDir == "" {
		fail("cannot tell which home directory is the real one")
	}
	home, _ := os.UserHomeDir()
	if home == "" || filepath.Clean(home) == filepath.Clean(real.HomeDir) {
		fail("HOME is the real home directory (" + real.HomeDir + ")")
	}
	data := os.Getenv("WERKBORD_DATA_DIR")
	if data == "" || !strings.HasPrefix(filepath.Clean(data), filepath.Clean(home)+"/") {
		fail("WERKBORD_DATA_DIR is not inside the test home")
	}
	if os.Getenv("WERKBORD_ADDR") == "" || strings.HasSuffix(os.Getenv("WERKBORD_ADDR"), ":7420") {
		fail("WERKBORD_ADDR is not a port of its own")
	}
	instanceID = "dev.werkbord.desktop.test"
}
