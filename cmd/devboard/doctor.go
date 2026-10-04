package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"devboard/internal/config"
	"devboard/internal/controller"
	"devboard/internal/daemon"
	"devboard/internal/doctor"
	"devboard/internal/github"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
)

// cmdDoctor checks everything. The controller's own checks run inside it, so they
// see what it sees; the ones about the service and the shell run here.
func (a *app) cmdDoctor(ctx context.Context, args []string) error {
	asJSON, strict := false, false
	for _, x := range args {
		switch x {
		case "--json":
			asJSON = true
		case "--strict":
			strict = true
		default:
			return errors.New("usage: devboard doctor [--json] [--strict]")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	r := a.runDoctor(ctx)
	if asJSON {
		if err := r.JSON(a.out); err != nil {
			return err
		}
	} else {
		a.printf("Dev Board %s doctor\n\n", version)
		r.Text(a.out, a.isTerminal(a.out))
	}
	switch w := r.Worst(); {
	case w == doctor.Fail, strict && w == doctor.Warn:
		return silentError{}
	}
	return nil
}

func (a *app) runDoctor(ctx context.Context) doctor.Report {
	r := doctor.Report{Version: version}
	r.Add(a.controllerCheck(ctx))
	r.Add(a.serviceCheck(ctx))

	var inner doctor.Report
	if _, up := a.healthy(ctx); up {
		c, err := a.client()
		if err == nil {
			inner, err = c.doctor(ctx)
		}
		if err != nil {
			r.Add(doctor.Check{ID: "controller-checks", Name: "checks", Status: doctor.Fail, Summary: "the controller would not run its checks: " + err.Error(),
				Fix: "If it says the token is wrong, run `devboard restart`."})
			inner = a.offline(ctx)
		}
	} else {
		inner = a.offline(ctx)
	}
	r.Add(inner.Checks...)
	r.Add(a.pathCheck(inner)...)
	return r
}

// offline runs the checks that need no controller.
func (a *app) offline(ctx context.Context) doctor.Report {
	agents, _ := controller.NewAgents(a.cfg)
	env := doctor.Env{Config: a.cfg, Agents: agents, Git: &gitrepo.CLI{}}
	if !a.cfg.GitHub.Disabled {
		cli := &github.CLI{Binary: a.cfg.GitHub.Command}
		env.GitHub = &service.GitHubSetup{CLI: cli, Login: &github.LoginSession{CLI: cli}}
	} else {
		env.GitHub = &service.GitHubSetup{}
	}
	return doctor.Run(ctx, version, env.Standard()...)
}

func (a *app) controllerCheck(ctx context.Context) doctor.Check {
	c := doctor.Check{ID: "controller", Name: "controller"}
	v, up := a.healthy(ctx)
	if !up {
		c.Status, c.Summary, c.Fix = doctor.Fail, "not running", "Run `devboard start`; if it will not start, `devboard logs` shows why."
		return c
	}
	c.Status, c.Summary = doctor.OK, fmt.Sprintf("%s running at %s", v, a.controllerURL())
	if v != version {
		c.Status = doctor.Warn
		c.Summary += fmt.Sprintf(", but this devboard is %s", version)
		c.Fix = "Run `devboard restart` so the controller is the version you just installed."
	}
	if cl, err := a.client(); err == nil {
		if _, err := cl.listProjects(ctx); err != nil && strings.Contains(err.Error(), "token") {
			c.Status, c.Summary = doctor.Fail, c.Summary+"; it refuses this computer's access token"
			c.Fix = "The token file changed since it started: run `devboard restart`."
		}
	}
	return c
}

func (a *app) serviceCheck(ctx context.Context) doctor.Check {
	c := doctor.Check{ID: "service", Name: "service"}
	m, inst := a.installed(ctx)
	if !inst {
		bg := &daemon.Background{Options: daemon.Options{DataDir: a.cfg.DataDir}}
		st, _ := bg.Status(ctx)
		c.Status = doctor.Warn
		c.Summary = "not installed: Dev Board will not start when you log in"
		if st.Running {
			c.Summary += " (running as a background process, pid " + fmt.Sprint(st.PID) + ")"
		}
		c.Fix = "Run `devboard setup` to install it."
		return c
	}
	st, err := m.Status(ctx)
	switch {
	case err != nil:
		c.Status, c.Summary = doctor.Warn, fmt.Sprintf("%s: %v", m.Name(), err)
	case st.Running:
		c.Status, c.Summary = doctor.OK, fmt.Sprintf("%s: running (pid %d), starts at login", m.Name(), st.PID)
	default:
		c.Status, c.Summary = doctor.Warn, m.Name()+": installed but not running"
		if _, up := a.healthy(ctx); up {
			c.Summary += "; a controller started by hand is answering instead"
			c.Fix = "Stop that one and run `devboard start`, so the service owns the controller."
		} else {
			c.Fix = "Run `devboard start`."
		}
	}
	return c
}

// pathCheck catches the commonest way an installed service goes wrong: an agent
// that works in the user's shell and is not found by the service, because a
// service does not inherit the shell's PATH.
func (a *app) pathCheck(inner doctor.Report) []doctor.Check {
	var out []doctor.Check
	for _, id := range []string{config.AgentClaudeCode, config.AgentCodex} {
		ck, ok := inner.Find(id)
		if !ok || ck.Status == doctor.OK {
			continue
		}
		command := map[string]string{config.AgentClaudeCode: "claude", config.AgentCodex: "codex"}[id]
		if cfg := a.cfg.Agents[id]; cfg.Command != "" {
			command = cfg.Command
		}
		if p, err := exec.LookPath(command); err == nil && strings.Contains(ck.Summary, "not installed") {
			out = append(out, doctor.Check{ID: id + "-path", Name: id + " path", Status: doctor.Warn,
				Summary: fmt.Sprintf("your shell finds %s at %s, but the controller does not", command, p),
				Fix:     "The controller runs with the PATH from when you ran `devboard setup`. Run `devboard setup` again from a terminal where `" + command + "` works."})
		}
	}
	return out
}
