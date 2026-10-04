package service

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Settings manages what the user sets in the app: the global execution
// defaults, project defaults, and the record of first-time setup. Which
// settings win is decided by domain.ResolveExecution; this stores them.
type Settings struct {
	Deps
	Catalog AgentCatalog
	// Version is recorded on the runner this controller registers.
	Version string
}

// Execution returns the global default execution configuration: only what has
// been set.
func (s *Settings) Execution(ctx context.Context) (domain.ExecutionConfig, error) {
	var cfg domain.ExecutionConfig
	err := s.Store.View(ctx, func(tx store.Tx) error {
		err := tx.Settings().Get(ctx, domain.SettingExecution, &cfg)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	})
	return cfg, err
}

// SetExecution replaces the global defaults. An empty configuration means every
// field is the built-in default: the agent chosen for you, the agent's own model
// and reasoning, "ask me when needed", normal priority.
func (s *Settings) SetExecution(ctx context.Context, cfg domain.ExecutionConfig) (domain.ExecutionConfig, error) {
	cfg = cfg.Normalized()
	if err := validateExecution(ctx, s.Catalog, cfg); err != nil {
		return cfg, err
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Settings().Set(ctx, domain.SettingExecution, cfg, s.now()); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": domain.SettingExecution}))
	})
	return cfg, err
}

// Levels returns the stored levels above a task: the global defaults and the
// project's.
func (s *Settings) Levels(ctx context.Context, projectID string) (Levels, error) {
	var lv Levels
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		lv, err = loadLevels(ctx, tx, projectID)
		return err
	})
	return lv, err
}

// Onboarding returns how far first-time setup has got. A computer that already
// has projects (it was set up before there was a setup flow) counts as done, so an
// upgrade never opens a wizard over someone's existing work.
func (s *Settings) Onboarding(ctx context.Context) (domain.Onboarding, error) {
	var ob domain.Onboarding
	err := s.Store.View(ctx, func(tx store.Tx) error {
		err := tx.Settings().Get(ctx, domain.SettingOnboarding, &ob)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if ob.CompletedAt == nil {
			ps, err := tx.Projects().List(ctx)
			if err != nil {
				return err
			}
			if len(ps) > 0 {
				// Never persisted: the answer is the same every time it is asked.
				t := ps[0].CreatedAt
				ob.CompletedAt = &t
			}
		}
		return nil
	})
	return ob, err
}

// CompleteOnboarding records that the user finished or skipped setup, and which
// steps they skipped.
func (s *Settings) CompleteOnboarding(ctx context.Context, skipped []string) (domain.Onboarding, error) {
	var out domain.Onboarding
	for _, k := range skipped {
		switch k {
		case "network", "github", "agents", "projects":
			out.Skipped = append(out.Skipped, k)
		default:
			return out, errors.Join(domain.ErrInvalid, errors.New("unknown setup step "+k))
		}
	}
	now := s.now()
	out.CompletedAt = &now
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Settings().Set(ctx, domain.SettingOnboarding, out, now); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": domain.SettingOnboarding}))
	})
	return out, err
}

// ResetOnboarding forgets that setup was completed, so the app offers it again.
func (s *Settings) ResetOnboarding(ctx context.Context) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Settings().Set(ctx, domain.SettingOnboarding, domain.Onboarding{}, s.now()); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": domain.SettingOnboarding}))
	})
}

// Network returns whether the user turned the private network on, and whether
// they have said either way.
func (s *Settings) Network(ctx context.Context) (enabled, set bool, err error) {
	var n domain.NetworkSetting
	err = s.Store.View(ctx, func(tx store.Tx) error {
		e := tx.Settings().Get(ctx, domain.SettingNetwork, &n)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		set = e == nil
		return e
	})
	return n.Enabled, set, err
}

// SetNetwork records the user's choice.
func (s *Settings) SetNetwork(ctx context.Context, enabled bool) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Settings().Set(ctx, domain.SettingNetwork, domain.NetworkSetting{Enabled: enabled}, s.now()); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": domain.SettingNetwork}))
	})
}

// RegisterRunner records this computer as a runner, or refreshes its record. It
// is called every time the controller starts, so the runner exists as soon as
// there is a controller, with nothing to configure.
func (s *Settings) RegisterRunner(ctx context.Context) (*domain.Runner, error) {
	host, _ := os.Hostname()
	host = strings.TrimSuffix(host, ".local")
	name := host
	if name == "" {
		name = "This computer"
	}
	now := s.now()
	in := &domain.Runner{
		ID: domain.NewID(domain.PrefixRunner), Name: name, Kind: domain.RunnerLocal, Hostname: host,
		OS: runtime.GOOS, Arch: runtime.GOARCH, Version: s.Version, CreatedAt: now, LastSeenAt: now,
	}
	var out *domain.Runner
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		out, err = tx.Runners().UpsertLocal(ctx, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	out.Online = true
	return out, nil
}

// Runners lists the registered runners. The local one is online: the controller
// that owns it is the one answering.
func (s *Settings) Runners(ctx context.Context) ([]domain.Runner, error) {
	var out []domain.Runner
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Runners().List(ctx)
		return err
	})
	for i := range out {
		out[i].Online = out[i].Kind == domain.RunnerLocal
	}
	return out, err
}
