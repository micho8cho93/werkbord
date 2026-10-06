package main

import (
	"context"
	"os/exec"
	goruntime "runtime"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"devboard/desktop/internal/shell"
)

// wailsUI is the window system, as the shell sees it.
type wailsUI struct {
	mu  sync.RWMutex
	ctx context.Context
}

var _ shell.UI = (*wailsUI)(nil)

func (u *wailsUI) startup(ctx context.Context) {
	u.mu.Lock()
	u.ctx = ctx
	u.mu.Unlock()
}

// context is the window's, which exists once the app has started; before that there is nothing to show.
func (u *wailsUI) context() context.Context {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.ctx
}

func (u *wailsUI) Ask(d shell.Dialog) string {
	ctx := u.context()
	if ctx == nil {
		return d.Cancel
	}
	kind := runtime.InfoDialog
	switch d.Kind {
	case shell.Question:
		kind = runtime.QuestionDialog
	case shell.Failure:
		kind = runtime.ErrorDialog
	}
	answer, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: kind, Title: d.Title, Message: d.Message, Buttons: d.Buttons, DefaultButton: d.Default, CancelButton: d.Cancel,
	})
	if err != nil {
		return d.Cancel
	}
	return answer
}

func (u *wailsUI) OpenURL(url string) {
	if ctx := u.context(); ctx != nil {
		runtime.BrowserOpenURL(ctx, url)
	}
}

// Reveal shows path in the Finder (the file manager, elsewhere).
func (u *wailsUI) Reveal(path string) {
	switch goruntime.GOOS {
	case "darwin":
		_ = exec.Command("open", "-R", path).Start()
	case "windows":
		_ = exec.Command("explorer", "/select,", path).Start()
	default:
		_ = exec.Command("xdg-open", path).Start()
	}
}

// OpenFile opens path in whatever opens such files.
func (u *wailsUI) OpenFile(path string) {
	switch goruntime.GOOS {
	case "darwin":
		_ = exec.Command("open", path).Start()
	case "windows":
		_ = exec.Command("cmd", "/c", "start", "", path).Start()
	default:
		_ = exec.Command("xdg-open", path).Start()
	}
}

// Reload sends the window back to the loading screen, which connects again and so also
// picks up the access token as it is now.
func (u *wailsUI) Reload() {
	if ctx := u.context(); ctx != nil {
		runtime.WindowReloadApp(ctx)
	}
}

func (u *wailsUI) Emit(event string, data any) {
	if ctx := u.context(); ctx != nil {
		runtime.EventsEmit(ctx, event, data)
	}
}

// show brings the window back, from the Dock, hidden or minimised.
func (u *wailsUI) show() {
	if ctx := u.context(); ctx != nil {
		runtime.Show(ctx)
		runtime.WindowShow(ctx)
		runtime.WindowUnminimise(ctx)
	}
}
