package launcher

import (
	"context"
	"devboard/internal/domain"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// UpdateSafety is a read-only preflight for swapping the desktop shell. The CLI
// installer repeats its authoritative interruption checks before changing a backend.
func (l *Launcher) UpdateSafety(ctx context.Context) error {
	cfg, err := l.Config(ctx)
	if err != nil {
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
	if _, ok := l.Health(ctx, cfg.ControllerURL()); !ok {
		return errors.New("update deferred: the Individual controller cannot be checked")
	}
	tok, err := cfg.ResolveToken(false)
	if err != nil {
		return err
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
		return fmt.Errorf("update deferred: Individual safety check returned %d", res.StatusCode)
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
