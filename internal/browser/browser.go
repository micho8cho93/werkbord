// Package browser opens a web page in the user's default browser.
package browser

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"runtime"
)

// Open opens rawURL in the default browser. Only http and https addresses are
// opened: the address is handed to the operating system's opener, which would act
// on a file path or another scheme just as readily.
func Open(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("only http and https addresses are opened in a browser")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		// rundll32 takes the address as one argument; `start` would hand it to cmd's parser.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("no graphical session to open a browser in")
		}
		opener, err := exec.LookPath("xdg-open")
		if err != nil {
			return errors.New("xdg-open is not installed")
		}
		cmd = exec.Command(opener, rawURL)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
