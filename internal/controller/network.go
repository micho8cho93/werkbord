package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"devboard/internal/config"
	"devboard/internal/domain"
	"devboard/internal/netprivate"
	"devboard/internal/service"
)

// networkControl is the controller's private network: the manager that runs it,
// and the user's choice of whether it is on.
type networkControl struct {
	mgr      *netprivate.Manager
	settings *service.Settings
	log      *slog.Logger
	// pinned is config.json's (or the environment's) decision, which overrides the
	// app's. nil means the app decides.
	pinned *bool
}

// newNetworkControl builds the private network for a controller whose handler
// to serve on it is h. Nothing is started.
func newNetworkControl(cfg config.Config, settings *service.Settings, h http.Handler, backend netprivate.Backend, log *slog.Logger) *networkControl {
	host := cfg.Network.Hostname
	if host == "" {
		machine, _ := os.Hostname()
		host = netprivate.HostnameFor(netprivate.StateDir(cfg.DataDir), machine)
	}
	// An auth key signs the node in without a browser. It is read from the
	// environment only: a key in config.json would sit on disk.
	authKey := config.Getenv("TS_AUTHKEY")
	if authKey == "" {
		authKey = os.Getenv("TS_AUTHKEY")
	}
	if backend == nil {
		backend = netprivate.NewTailscale(netprivate.TailscaleOptions{
			Dir: netprivate.StateDir(cfg.DataDir), Hostname: host, AuthKey: authKey, ControlURL: cfg.Network.ControlURL, Log: log,
		})
	}
	return &networkControl{
		mgr:      netprivate.New(netprivate.Options{Backend: backend, Handler: h, Log: log}),
		settings: settings,
		pinned:   cfg.Network.Enabled,
		log:      log,
	}
}

// wanted reports whether the network should be running now.
func (n *networkControl) wanted(ctx context.Context) (bool, error) {
	if n.pinned != nil {
		return *n.pinned, nil
	}
	enabled, _, err := n.settings.Network(ctx)
	return enabled, err
}

// StartIfWanted starts the network if the user, or config.json, asked for it.
func (n *networkControl) StartIfWanted(ctx context.Context) error {
	on, err := n.wanted(ctx)
	if err != nil {
		return err
	}
	if on {
		n.mgr.Start()
	}
	return nil
}

func (n *networkControl) Status() netprivate.Status { return n.mgr.Status() }

func (n *networkControl) Enable(ctx context.Context) error {
	if n.pinned != nil && !*n.pinned {
		return fmt.Errorf("%w: the private network is turned off in config.json (network.enabled)", domain.ErrConflict)
	}
	if err := n.settings.SetNetwork(ctx, true); err != nil {
		return err
	}
	n.mgr.Start()
	return nil
}

func (n *networkControl) Disable(ctx context.Context) error {
	if n.pinned != nil && *n.pinned {
		return fmt.Errorf("%w: the private network is turned on in config.json (network.enabled)", domain.ErrConflict)
	}
	if err := n.settings.SetNetwork(ctx, false); err != nil {
		return err
	}
	if netprivate.IsPrivateRequest(ctx) {
		// Stopping a private listener from one of its handlers would wait for that
		// handler to return. Let its response finish before closing the listener.
		go func() {
			<-ctx.Done()
			if err := n.mgr.Stop(context.Background()); err != nil && n.log != nil {
				n.log.Warn("could not stop the private network", "err", err)
			}
		}()
		return nil
	}
	return n.mgr.Stop(ctx)
}

func (n *networkControl) Stop(ctx context.Context) error { return n.mgr.Stop(ctx) }

// Choice implements api.NetworkController.
func (n *networkControl) Choice(ctx context.Context) string {
	if n.pinned != nil {
		if *n.pinned {
			return "pinned_on"
		}
		return "pinned_off"
	}
	enabled, set, err := n.settings.Network(ctx)
	switch {
	case err != nil || !set:
		return "unset"
	case enabled:
		return "on"
	}
	return "off"
}
