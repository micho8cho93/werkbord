package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"devboard/cmd/werkbord-team/desktop/internal/platform"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed frontend
var assets embed.FS

var version = "dev"

// App exposes only native, locally initiated setup and lifecycle actions. No arbitrary process, path or request method.
type App struct {
	ctx        context.Context
	mu         sync.Mutex
	pending    string
	connecting sync.Mutex
}

func (a *App) context() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

func (a *App) Info() map[string]string {
	return map[string]string{"version": version, "platform": runtime.GOOS}
}

func (a *App) PendingInvitation() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	link := a.pending
	a.pending = ""
	return link
}

func (a *App) OpenExternal(address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("only secure web links can open in your browser")
	}
	wr.BrowserOpenURL(a.context(), u.String())
	return nil
}

func (a *App) Connect() (string, error) {
	a.connecting.Lock()
	defer a.connecting.Unlock()
	key, err := platform.AccessKey()
	if err != nil {
		return "", err
	}
	if err := platform.Probe(a.context(), key, version); err != nil {
		if err := platform.PreflightReplacement(a.context(), key); err != nil {
			return "", err
		}
		if err := platform.AuthorizeService(a.context(), "install"); err != nil {
			return "", err
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			if err = platform.Probe(a.context(), key, version); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return "", errors.New("the Team service is starting; choose Try again in a moment")
			}
			select {
			case <-a.context().Done():
				return "", a.context().Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	a.mu.Lock()
	link := a.pending
	a.pending = ""
	a.mu.Unlock()
	address := platform.BaseURL + "/#token=" + url.QueryEscape(key)
	if link != "" {
		address += "&join=" + url.QueryEscape(link)
	}
	return address, nil
}

func (a *App) SetupRunner() error {
	choice, err := wr.MessageDialog(a.context(), wr.MessageDialogOptions{Type: wr.QuestionDialog, Title: "Set up your free Werkbord runner", Message: "Werkbord runs your work with your own credentials and approvals. It will keep running when either app closes. This installs or opens the free Werkbord on this computer.", Buttons: []string{"Set up runner", "Cancel"}, DefaultButton: "Set up runner", CancelButton: "Cancel"})
	if err != nil {
		return err
	}
	if choice != "Set up runner" {
		return nil
	}
	return platform.SetupRunner(a.context())
}

func (a *App) Service(action string) error {
	if action != "stop" && action != "start" && action != "uninstall" {
		return errors.New("choose start, stop or uninstall")
	}
	if action == "uninstall" {
		choice, err := wr.MessageDialog(a.context(), wr.MessageDialogOptions{Type: wr.QuestionDialog, Title: "Remove the Team service?", Message: "First leave your workspace in Team. This removes the Team service, its local settings and connection secrets. Your free runner is unaffected. Any archives or backups you chose to keep are preserved. To remove the GUI too, move Werkbord Team.app to the Trash.", Buttons: []string{"Remove Team service", "Cancel"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
		if err != nil {
			return err
		}
		if choice != "Remove Team service" {
			return errors.New("service removal was cancelled")
		}
	}
	if action == "stop" {
		choice, err := wr.MessageDialog(a.context(), wr.MessageDialogOptions{Type: wr.QuestionDialog, Title: "Stop Team on this Mac?", Message: "This stops this device's Team service, Workspace Host and Connectivity Host. Your runner continues running. Workspace data and secrets are kept. If this is the only online host, Team will be unavailable until you start it again.", Buttons: []string{"Stop Team", "Keep running"}, DefaultButton: "Keep running", CancelButton: "Keep running"})
		if err != nil {
			return err
		}
		if choice != "Stop Team" {
			return nil
		}
	}
	return platform.AuthorizeService(a.context(), action)
}

func (a *App) receive(link string) {
	if !strings.HasPrefix(link, "werkbord://join/") || len(link) > 16384 {
		return
	}
	a.mu.Lock()
	a.pending = link
	a.mu.Unlock()
	if a.context() != nil {
		wr.WindowShow(a.context())
		wr.WindowUnminimise(a.context())
	}
}

// headless runs one explicit service operation for the Werkbord desktop app, which has no installer of its own: it shows
// the person what is about to happen and asks them, and this program, which is Team's own signed installer, performs it
// (macOS asks for an administrator's authorization, as it does when this app's own window installs the service). It opens no
// window, takes no argument that names a path or a program, and prints one line of JSON.
//
//	--activate            install the Team service, with no path to the person's own Werkbord, or bring it up to date
//	--service start|stop|uninstall
func headless(args []string) int {
	say := func(ok bool, detail string) int {
		out, _ := json.Marshal(map[string]any{"ok": ok, "version": version, "detail": detail})
		fmt.Println(string(out))
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
			err = platform.RequireRelease(filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "..")))
		}
		if err != nil {
			return say(false, err.Error())
		}
		return say(true, "signed by the Werkbord release team")
	case len(args) == 1 && args[0] == "--activate":
		key, err := platform.AccessKey()
		if err != nil {
			return say(false, err.Error())
		}
		installed, isolated, err := platform.ServiceIsolated()
		if err != nil {
			return say(false, err.Error())
		}
		running := platform.Probe(ctx, key, version) == nil
		if !(installed && isolated && running) {
			// A service that belongs to a workspace is never replaced here. Say so now, not after an administrator's
			// password has been asked for.
			if err := platform.PreflightReplacement(ctx, key); err != nil {
				return say(false, err.Error())
			}
			if err := platform.AuthorizeService(ctx, platform.ActionInstallIsolated); err != nil {
				return say(false, err.Error())
			}
		}
		deadline := time.Now().Add(60 * time.Second)
		for platform.Probe(ctx, key, version) != nil {
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
	case len(args) == 2 && args[0] == "--service" && (args[1] == platform.ActionStart || args[1] == platform.ActionStop || args[1] == platform.ActionUninstall):
		if err := platform.AuthorizeService(ctx, args[1]); err != nil {
			return say(false, err.Error())
		}
		return say(true, "done")
	}
	return say(false, "unknown request")
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--activate" || os.Args[1] == "--service" || os.Args[1] == "--verify-release") {
		os.Exit(headless(os.Args[1:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "--team-service" {
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "invalid native service request")
			os.Exit(1)
		}
		if err := platform.PrivilegedService(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	a := &App{}
	for _, arg := range os.Args[1:] {
		a.receive(arg)
	}
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	file := appMenu.AddSubmenu("File")
	file.AddText("Open Team", nil, func(*menu.CallbackData) { wr.WindowShow(a.context()); wr.WindowUnminimise(a.context()) })
	file.AddSeparator()
	file.AddText("Quit", nil, func(*menu.CallbackData) { wr.Quit(a.context()) })
	err := wails.Run(&options.App{Title: "Werkbord Team", Width: 1280, Height: 840, MinWidth: 600, MinHeight: 560,
		BackgroundColour: &options.RGBA{R: 245, G: 245, B: 242, A: 255}, AssetServer: &assetserver.Options{Assets: assets}, Bind: []any{a}, BindingsAllowedOrigins: platform.BaseURL,
		HideWindowOnClose: true, Menu: appMenu, EnableDefaultContextMenu: true,
		OnStartup: func(ctx context.Context) { a.mu.Lock(); a.ctx = ctx; a.mu.Unlock() },
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "dev.werkbord.team.desktop", OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
			for _, arg := range data.Args {
				a.receive(arg)
			}
			if a.context() != nil {
				wr.WindowShow(a.context())
				wr.WindowUnminimise(a.context())
			}
		}},
		Mac: &mac.Options{TitleBar: mac.TitleBarDefault(), Appearance: mac.DefaultAppearance, OnUrlOpen: a.receive, About: &mac.AboutInfo{Title: "Werkbord Team " + version, Message: "Your team's workspace on your own machines.\nThe Team service and your runner keep working when this window closes."}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
