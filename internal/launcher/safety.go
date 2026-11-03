package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
	"werkbord/internal/device"
	"werkbord/internal/domain"
)

// UpdateSafety is a read-only preflight for swapping the desktop shell. The CLI
// installer repeats its authoritative interruption checks before changing a backend.
func (l *Launcher) UpdateSafety(ctx context.Context) error {
	cfg, err := l.Config(ctx)
	if err != nil {
		return err
	}
	if err := device.CheckOperationsSettled(cfg.DataDir); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(cfg.DataDir, "runner", "runs", "*.json"))
	if err != nil {
		return err
	}
	for _, file := range files {
		var journal struct {
			Phase string `json:"phase"`
		}
		raw, err := os.ReadFile(file)
		if err != nil || json.Unmarshal(raw, &journal) != nil {
			return errors.New("update deferred: a runner journal cannot be checked")
		}
		if journal.Phase != "ended" && journal.Phase != "resolved" {
			return errors.New("update deferred: finish or resolve this runner's work first")
		}
	}
	if mode, e := device.Load(cfg.DataDir); e != nil {
		return e
	} else if mode.Remote() {
		return device.OpenTerminals(cfg.DataDir, nil, false)
	}
	if _, ok := l.Health(ctx, cfg.ControllerURL()); !ok {
		return errors.New("update deferred: your own Werkbord cannot be checked")
	}
	tok, err := cfg.ResolveToken(false)
	if err != nil {
		return err
	}
	// The controller knows which of its terminals are alive; the file it keeps only says what it last wrote.
	live, haveLive := device.LiveTerminals(ctx, l.opt.HTTP, cfg.ControllerURL(), tok)
	if err := device.OpenTerminals(cfg.DataDir, live, haveLive); err != nil {
		return err
	}
	check, err := http.NewRequestWithContext(ctx, "GET", cfg.ControllerURL()+"/api/device", nil)
	if err != nil {
		return err
	}
	check.Header.Set("Authorization", "Bearer "+tok)
	status, err := l.opt.HTTP.Do(check)
	if err != nil {
		return err
	}
	var pending struct {
		PendingOperations int `json:"pendingOperations"`
	}
	if status.StatusCode == 200 {
		err = json.NewDecoder(io.LimitReader(status.Body, 1<<20)).Decode(&pending)
	}
	status.Body.Close()
	if status.StatusCode != 200 && status.StatusCode != 404 {
		return fmt.Errorf("update deferred: device safety check returned %d", status.StatusCode)
	}
	if err != nil {
		return err
	}
	if pending.PendingOperations > 0 {
		return errors.New("update deferred: device operations are pending")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", cfg.ControllerURL()+"/api/control-center", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := l.opt.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("update deferred: the safety check of your own Werkbord returned %d", res.StatusCode)
	}
	var view struct {
		Runs []struct {
			Run struct {
				Remote bool            `json:"remote"`
				State  domain.RunState `json:"state"`
			} `json:"run"`
		} `json:"runs"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&view); err != nil {
		return err
	}
	for _, item := range view.Runs {
		if !item.Run.State.Valid() {
			return errors.New("update deferred: controller returned an unknown run state")
		}
		if !item.Run.Remote && item.Run.State.Active() {
			return errors.New("update deferred: coding agents are active on this computer")
		}
	}
	return nil
}

// StopTerminals ends the given native terminals through the running controller, the same way the Stop button does, and
// waits until it reports them gone. It is what lets the person answer "a terminal is open" with one choice instead of a
// trip to the Terminal.
func (l *Launcher) StopTerminals(ctx context.Context, list []device.RunningTerminal) error {
	cfg, err := l.Config(ctx)
	if err != nil {
		return err
	}
	tok, err := cfg.ResolveToken(false)
	if err != nil {
		return err
	}
	base := cfg.ControllerURL()
	for _, t := range list {
		if t.ID == "" || t.ProjectID == "" {
			return errors.New("a terminal could not be identified, so it was not stopped")
		}
		req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/projects/"+url.PathEscape(t.ProjectID)+"/terminals/"+url.PathEscape(t.ID)+"/stop", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := l.opt.HTTP.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode != 200 && res.StatusCode != 404 {
			return fmt.Errorf("could not stop %s: HTTP %d", device.DescribeTerminals([]device.RunningTerminal{t}, time.Now()), res.StatusCode)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		live, ok := device.LiveTerminals(ctx, l.opt.HTTP, base, tok)
		if ok && len(live) == 0 {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return errors.New("the terminals did not stop in time")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
