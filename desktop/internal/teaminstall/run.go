package teaminstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Handles says whether args (the program's arguments, without its name) ask for the installer rather than the window. It is
// checked before anything else starts, so a request for the installer never opens a window or touches the web view.
func Handles(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--activate", "--service", "--verify-release", "--team-service":
		return true
	}
	return false
}

// Run does what args ask and returns the program's exit status. version is the app's own version, which is the version the
// service must report. Output is one line of JSON on out, except for the privileged step, which reports on errOut.
//
//	--activate            install the Team service, with no path to the person's own Werkbord, or bring it up to date
//	--service start|stop|uninstall
//	--verify-release      succeed only in a build signed by the release's Apple Developer team (for the release checks)
//	--team-service ACTION UID   the privileged step, which macOS runs as root after the administrator's authorization; it
//	                            accepts a real user and three fixed operations (see PrivilegedService)
func Run(version string, args []string, out, errOut io.Writer) int {
	nativeVersion = version
	if len(args) == 3 && args[0] == "--team-service" {
		if err := PrivilegedService(args[1], args[2]); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	say := func(ok bool, detail string) int {
		line, _ := json.Marshal(map[string]any{"ok": ok, "version": version, "detail": detail})
		fmt.Fprintln(out, string(line))
		if ok {
			return 0
		}
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	switch {
	case len(args) == 1 && args[0] == "--verify-release":
		exe, err := os.Executable()
		if err == nil {
			err = RequireRelease(filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "..")))
		}
		if err != nil {
			return say(false, err.Error())
		}
		return say(true, "signed by the Werkbord release team")
	case len(args) == 1 && args[0] == "--activate":
		key, err := AccessKey()
		if err != nil {
			return say(false, err.Error())
		}
		installed, isolated, err := ServiceIsolated()
		if err != nil {
			return say(false, err.Error())
		}
		running := Probe(ctx, key, version) == nil
		if !(installed && isolated && running) {
			// A service that belongs to a workspace is never replaced here. Say so now, not after an administrator's
			// password has been asked for.
			if err := PreflightReplacement(ctx, key); err != nil {
				return say(false, err.Error())
			}
			if err := AuthorizeService(ctx, ActionInstallIsolated); err != nil {
				return say(false, err.Error())
			}
		}
		deadline := time.Now().Add(60 * time.Second)
		for Probe(ctx, key, version) != nil {
			if time.Now().After(deadline) {
				return say(false, "the Team service is starting; try again in a moment")
			}
			select {
			case <-ctx.Done():
				return say(false, ctx.Err().Error())
			case <-time.After(500 * time.Millisecond):
			}
		}
		return say(true, "the Team service is running")
	case len(args) == 2 && args[0] == "--service" && (args[1] == ActionStart || args[1] == ActionStop || args[1] == ActionUninstall):
		if err := AuthorizeService(ctx, args[1]); err != nil {
			return say(false, err.Error())
		}
		return say(true, "done")
	}
	return say(false, "unknown request")
}
