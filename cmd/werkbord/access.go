package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"devboard/internal/config"
	"devboard/internal/localaccess"
)

// `werkbord access` shows which programs on this computer have been given a narrow way into Werkbord (not the
// controller's own token), and takes it away from one. A program is given access from its own side, with a
// request signed by the controller's token; the owner's tools are these two.
func cmdAccess(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: werkbord access list | werkbord access revoke <id>")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("access "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch sub {
	case "list":
		var list []localaccess.Entry
		if err := c.do(ctx, "GET", "/api/local-access", nil, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "No program has been given access to Werkbord.")
			return nil
		}
		for _, e := range list {
			used := "never used"
			if e.LastUsedAt != nil {
				used = "last used " + e.LastUsedAt.Local().Format(time.RFC3339)
			}
			fmt.Fprintf(stdout, "%s  %-30s since %s, %s\n", e.ID, e.Name, e.CreatedAt.Local().Format("2006-01-02"), used)
		}
		return nil
	case "revoke":
		if len(positional) != 1 {
			return errors.New("usage: werkbord access revoke <id>")
		}
		if err := c.do(ctx, "DELETE", "/api/local-access/"+positional[0], nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s no longer has access to Werkbord.\n", positional[0])
		return nil
	}
	return fmt.Errorf("unknown access command %q (list, revoke)", sub)
}
