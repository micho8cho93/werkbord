package main

import (
	"context"
	"errors"
	"time"

	"werkbord/internal/netprivate"
)

// `werkbord network` is the private network (Tailscale) that your other computers join as remote runners. Phones do not
// use it: they come in through the relay (`werkbord phones add`). It is off until you turn it on here, because most people
// have only this computer.
func (a *app) cmdNetwork(ctx context.Context, args []string) error {
	const usage = "usage: werkbord network [status | on | off]"
	sub := "status"
	if len(args) > 1 {
		return errors.New(usage)
	}
	if len(args) == 1 {
		sub = args[0]
	}
	if _, ok := a.healthy(ctx); !ok {
		return errors.New("Werkbord is not running: start it with `werkbord start`")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	switch sub {
	case "status":
		n, err := c.network(ctx)
		if err != nil {
			return err
		}
		a.printNetwork(n)
		return nil
	case "on":
		n, err := a.ensureNetwork(ctx, c, runnerNetworkTimeout, true)
		if err != nil {
			return err
		}
		a.printNetwork(n)
		if n.State == string(netprivate.StateConnected) {
			a.printf("Other computers can join now: in Settings → Runners choose Add runner and paste the command it makes.\n")
		}
		return nil
	case "off":
		n, err := c.setNetwork(ctx, false)
		if err != nil {
			return err
		}
		a.printNetwork(n)
		return nil
	}
	return errors.New(usage)
}

// runnerNetworkTimeout is how long `werkbord network on` waits for you to sign in to Tailscale.
const runnerNetworkTimeout = 5 * time.Minute

func (a *app) printNetwork(n networkInfo) {
	switch n.State {
	case string(netprivate.StateConnected):
		a.printf("Private network (for your other computers): connected at %s\n", n.URL)
	case string(netprivate.StateOff):
		a.printf("Private network (for your other computers): off. `werkbord network on` turns it on; you only need it to add another computer as a runner.\n")
	case string(netprivate.StateNeedsLogin):
		a.printf("Private network: waiting for you to sign in to Tailscale. Run `werkbord network on` to open the sign-in page.\n")
	case string(netprivate.StateNeedsApproval):
		a.printf("Private network: your tailnet's admin has to approve this device: https://login.tailscale.com/admin/machines\n")
	case string(netprivate.StateError):
		a.printf("Private network: %s. Run `werkbord doctor`.\n", n.Error)
	default:
		a.printf("Private network: %s\n", n.State)
	}
}
