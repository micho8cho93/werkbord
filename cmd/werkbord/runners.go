package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/url"
	"time"
)

// `werkbord runners` is the controller owner's side of a lost or revoked runner. Revoking fences a runner's credentials at
// once, but a runner that can continue offline may still be executing, so its runs stay reserved. They are released only
// when the owner has inspected that machine and says it stopped; no timer does it. (`werkbord runner resolve` is the other
// end of this: it clears a runner's own journal on the runner's machine.)
func (a *app) cmdRunners(ctx context.Context, args []string) error {
	const usage = "usage: werkbord runners held | werkbord runners confirm-stopped <runner-id> <run-id> --confirm-stopped"
	if len(args) == 0 {
		return errors.New(usage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "held", "confirm-stopped":
	default:
		return errors.New("unknown runners command \"" + sub + "\" (held, confirm-stopped)")
	}
	fs := flag.NewFlagSet("runners "+sub, flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	confirmed := fs.Bool("confirm-stopped", false, "I inspected that machine and nothing from this run is still executing")
	positional, err := parseInterspersed(fs, rest)
	if err != nil {
		return err
	}
	if _, ok := a.healthy(ctx); !ok {
		return errors.New("Werkbord is not running: start it with `werkbord start`")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if sub == "held" {
		if len(positional) != 0 {
			return errors.New(usage)
		}
		held, err := c.heldRunnerRuns(ctx)
		if err != nil {
			return err
		}
		if len(held) == 0 {
			a.printf("No work is held on a revoked runner.\n")
			return nil
		}
		a.printf("These runs may still be executing on revoked machines. They stay reserved until you inspect the machine, stop\nanything still running and confirm:\n\n")
		for _, h := range held {
			title := h.TaskTitle
			if title == "" {
				title = h.TaskID
			}
			a.printf("%s  run %s  %q on %s (%s), started %s\n    werkbord runners confirm-stopped %s %s --confirm-stopped\n",
				h.ProjectID, h.RunID, title, h.RunnerName, h.Hostname, h.StartedAt.Local().Format(time.DateTime), h.RunnerID, h.RunID)
		}
		return nil
	}
	if len(positional) != 2 || !*confirmed {
		return errors.New("inspect that machine and stop anything still running first, then: werkbord runners confirm-stopped <runner-id> <run-id> --confirm-stopped")
	}
	if err = c.do(ctx, http.MethodPost, "/api/runners/"+url.PathEscape(positional[0])+"/runs/"+url.PathEscape(positional[1])+"/resolve", map[string]bool{"confirmStopped": true}, nil); err != nil {
		return err
	}
	a.printf("Run %s is released. Its work on that machine is unverified: reconcile its branch before running the task again.\n", positional[1])
	return nil
}
