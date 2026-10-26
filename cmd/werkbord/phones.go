package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"werkbord/internal/netprivate"
	"werkbord/internal/phoneaccess"
)

// `werkbord phones` shows the phones that have been enrolled, takes one away, and enrolls another. A phone is enrolled by
// scanning a QR code that carries a single-use code (5 minutes); it is then given a credential of its own that this
// command can revoke. The controller's token is never in the code.
func (a *app) cmdPhones(ctx context.Context, args []string) error {
	const usage = "usage: werkbord phones list | werkbord phones revoke <id> | werkbord phones add [--qr]"
	if len(args) == 0 {
		return errors.New(usage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "revoke", "add":
	default:
		return errors.New("unknown phones command " + `"` + sub + `"` + " (list, revoke, add)")
	}
	if _, ok := a.healthy(ctx); !ok {
		return errors.New("Werkbord is not running: start it with `werkbord start`")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if len(rest) != 0 {
			return errors.New(usage)
		}
		phones, err := c.phones(ctx)
		if err != nil {
			return err
		}
		if len(phones) == 0 {
			a.printf("No phone is enrolled. `werkbord phones add --qr` shows a code to scan.\n")
			return nil
		}
		for _, p := range phones {
			seen := "never seen"
			if p.LastUsedAt != nil {
				seen = "last seen " + p.LastUsedAt.Local().Format("2006-01-02 15:04")
			}
			a.printf("%s  %-30s added %s, %s\n", p.ID, p.Name, p.CreatedAt.Local().Format("2006-01-02"), seen)
		}
		return nil
	case "revoke":
		if len(rest) != 1 {
			return errors.New(usage)
		}
		if err := c.do(ctx, "DELETE", "/api/phones/"+rest[0], nil, nil); err != nil {
			return err
		}
		a.printf("%s is signed out and can no longer reach Werkbord.\n", rest[0])
		return nil
	default: // add
		qr := false
		for _, x := range rest {
			if x != "--qr" {
				return errors.New(usage)
			}
			qr = true
		}
		n, err := a.ensureNetwork(ctx, c, phoneTimeout, true)
		if err != nil {
			return err
		}
		if n.State != string(netprivate.StateConnected) {
			return errors.New("the private network is " + n.State + ": finish signing in, then run this again")
		}
		return a.printEnrollment(ctx, c, n, qr)
	}
}

// printEnrollment makes a single-use code and shows it as a link, and as a QR code when asked or on a terminal.
func (a *app) printEnrollment(ctx context.Context, c *client, n networkInfo, qr bool) error {
	e, err := c.enrollment(ctx)
	if err != nil {
		return err
	}
	a.printf("Scan this with your phone's camera. It opens Werkbord on the phone, signed in. The phone needs the Tailscale app, signed in to the same account.\n\n")
	if qr || a.isTerminal(a.out) {
		text, err := netprivate.QRText(e.Link)
		if err != nil {
			return err
		}
		a.printf("%s\n", indent(strings.TrimRight(text, "\n"), "  "))
	} else {
		a.printf("Link: %s\n", e.Link)
	}
	a.printf("Address: %s\nThe code works for one phone, for %d minutes. `werkbord phones list` shows who is enrolled and `werkbord phones revoke <id>` signs one out.\n", n.URL, int(phoneaccess.CodeTTL/time.Minute))
	if !n.HTTPS && n.HTTPSHint != "" {
		a.printf("Note: %s\n", n.HTTPSHint)
	}
	return nil
}
