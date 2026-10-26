package service

import (
	"context"
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
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
// has projects and no setup record (it predates guided setup) counts as done, so an
// upgrade never opens a wizard over someone's existing work.
func (s *Settings) Onboarding(ctx context.Context) (domain.Onboarding, error) {
	var ob domain.Onboarding
	err := s.Store.View(ctx, func(tx store.Tx) error {
		err := tx.Settings().Get(ctx, domain.SettingOnboarding, &ob)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if errors.Is(err, domain.ErrNotFound) {
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

// updateOnboarding applies fn to the stored record, not the one Onboarding derives
// from existing projects, so a derived answer is never written back.
func (s *Settings) updateOnboarding(ctx context.Context, fn func(*domain.Onboarding) error) (domain.Onboarding, error) {
	var out domain.Onboarding
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		out = domain.Onboarding{}
		if err := tx.Settings().Get(ctx, domain.SettingOnboarding, &out); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err := fn(&out); err != nil {
			return err
		}
		if err := tx.Settings().Set(ctx, domain.SettingOnboarding, out, s.now()); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": domain.SettingOnboarding}))
	})
	return out, err
}

func invalid(msg string) error { return errors.Join(domain.ErrInvalid, errors.New(msg)) }

// CompleteOnboarding records that the user finished or skipped setup, and which
// steps they skipped. Step states and tours already recorded are kept.
func (s *Settings) CompleteOnboarding(ctx context.Context, skipped []string) (domain.Onboarding, error) {
	for _, k := range skipped {
		switch k {
		case "network", "github", "agents", "projects":
		default:
			return domain.Onboarding{}, invalid("unknown setup step " + k)
		}
	}
	return s.updateOnboarding(ctx, func(ob *domain.Onboarding) error {
		now := s.now()
		ob.CompletedAt = &now
		ob.Skipped = append([]string(nil), skipped...)
		return nil
	})
}

// SetOnboardingStep records the state of one guided-setup step. An empty state
// clears it, so the step is offered again.
func (s *Settings) SetOnboardingStep(ctx context.Context, step, state string) (domain.Onboarding, error) {
	if !slices.Contains(domain.OnboardingSteps, step) {
		return domain.Onboarding{}, invalid("unknown setup step " + step)
	}
	if state != "" && state != domain.StepDone && state != domain.StepSkipped {
		return domain.Onboarding{}, invalid("unknown step state " + state)
	}
	return s.updateOnboarding(ctx, func(ob *domain.Onboarding) error {
		if state == "" {
			delete(ob.Steps, step)
			return nil
		}
		if ob.Steps == nil {
			ob.Steps = map[string]string{}
		}
		ob.Steps[step] = state
		return nil
	})
}

// SetOnboardingVariant chooses the guided-setup path: with GitHub, or local only.
func (s *Settings) SetOnboardingVariant(ctx context.Context, variant string) (domain.Onboarding, error) {
	if variant != domain.OnboardingGitHub && variant != domain.OnboardingLocal {
		return domain.Onboarding{}, invalid("unknown setup path " + variant)
	}
	return s.updateOnboarding(ctx, func(ob *domain.Onboarding) error { ob.Variant = variant; return nil })
}

// SetOnboardingExample remembers the example project and ticket. An empty
// project forgets them.
func (s *Settings) SetOnboardingExample(ctx context.Context, projectID, taskID string) (domain.Onboarding, error) {
	return s.updateOnboarding(ctx, func(ob *domain.Onboarding) error {
		if projectID == "" {
			ob.Example = nil
			return nil
		}
		ob.Example = &domain.OnboardingExample{ProjectID: projectID, TaskID: taskID}
		return nil
	})
}

// MarkTour records that a surface's tour was shown. The first time wins, so a
// replay does not move the date.
func (s *Settings) MarkTour(ctx context.Context, surface string) (domain.Onboarding, error) {
	if !slices.Contains(domain.TourSurfaces, surface) {
		return domain.Onboarding{}, invalid("unknown tour " + surface)
	}
	return s.updateOnboarding(ctx, func(ob *domain.Onboarding) error {
		if _, seen := ob.Tours[surface]; seen {
			return nil
		}
		if ob.Tours == nil {
			ob.Tours = map[string]time.Time{}
		}
		ob.Tours[surface] = s.now()
		return nil
	})
}

// ResetTours forgets one surface's tour, or every tour when surface is empty, so
// it is shown again.
func (s *Settings) ResetTours(ctx context.Context, surface string) (domain.Onboarding, error) {
	if surface != "" && !slices.Contains(domain.TourSurfaces, surface) {
		return domain.Onboarding{}, invalid("unknown tour " + surface)
	}
	return s.updateOnboarding(ctx, func(ob *domain.Onboarding) error {
		if surface == "" {
			ob.Tours = nil
		} else {
			delete(ob.Tours, surface)
		}
		return nil
	})
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
