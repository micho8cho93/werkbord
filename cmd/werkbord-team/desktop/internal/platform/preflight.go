package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// PreflightReplacement asks the service that is already running, with the person's own credential, whether it belongs to a
// workspace, so that the person is told before an administrator's password is asked for rather than after. It returns
// ErrReplacementDeferred when the service holds, is joining, is leaving or is creating a workspace.
//
// It is advice and never permission: a service that cannot be asked (not running, a different credential, a version that
// answers differently) gives nil, and CheckReplacement, repeated by the installer under administrator authority and over the
// data on disk, decides. Nothing here can waive that check.
func PreflightReplacement(ctx context.Context, key string) error {
	return preflightReplacement(ctx, BaseURL, key)
}

func preflightReplacement(ctx context.Context, base, key string) error {
	// The services this app ships list every workspace slot on this computer.
	var slots struct {
		Workspaces *[]struct {
			Enrolled  bool   `json:"enrolled"`
			Pending   bool   `json:"pending"`
			Leaving   bool   `json:"leaving"`
			Operation string `json:"operation"`
		} `json:"workspaces"`
	}
	if askLocal(ctx, base+"/api/device/v1/workspaces", key, &slots) == nil && slots.Workspaces != nil {
		for _, s := range *slots.Workspaces {
			if s.Enrolled || s.Pending || s.Leaving || s.Operation != "" {
				return ErrReplacementDeferred
			}
		}
	}
	// A service from before several workspaces answers only for its one, and says so in its state.
	var state struct {
		Daemon    bool            `json:"daemon"`
		Enrolled  bool            `json:"enrolled"`
		Pending   bool            `json:"pending"`
		Leaving   bool            `json:"leaving"`
		Operation string          `json:"operation"`
		Workspace json.RawMessage `json:"workspace"`
	}
	if askLocal(ctx, base+"/api/device/v1/state", key, &state) == nil && state.Daemon {
		if state.Enrolled || state.Pending || state.Leaving || state.Operation != "" || len(state.Workspace) > 0 && string(state.Workspace) != "null" {
			return ErrReplacementDeferred
		}
	}
	return nil
}

// askLocal reads one JSON answer from the local service, without proxies or redirects. Any answer but a well-formed 200 is an error.
func askLocal(ctx context.Context, address, key string, into any) error {
	cl := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return http.ErrNotSupported
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(into)
}
