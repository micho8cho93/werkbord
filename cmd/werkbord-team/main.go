// Command werkbord-team runs Werkbord Team: the shared workspace where a team's
// members and projects are coordinated.
//
//	werkbord-team workspace create   start a workspace and print its owner's token
//	werkbord-team serve              run the server in the foreground
//	werkbord-team migrate            apply database migrations and exit
//	werkbord-team version            print the version
//
// Team is a separate product from the individual Werkbord (cmd/devboard): its own
// executable, version, data directory and database. It coordinates; it never
// runs anyone's agents or commands. Every member keeps using their own Werkbord
// runner with their own credentials (docs/PRODUCTS.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: werkbord-team <command> [flags]

commands:
  workspace create   start a workspace; prints the owner's token (once)
  serve              run the Team server in the foreground
  migrate            apply database migrations and exit
  version            print the version

Run "werkbord-team <command> -h" for command flags.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "werkbord-team:", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return flag.ErrHelp
	}
	cfg := config.Load()
	switch args[0] {
	case "serve":
		return cmdServe(ctx, cfg, args[1:], stderr)
	case "migrate":
		return cmdMigrate(ctx, cfg, args[1:], stdout, stderr)
	case "workspace":
		return cmdWorkspace(ctx, cfg, args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func commonFlags(fs *flag.FlagSet, cfg *config.Config) {
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directory for the database")
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address")
}

func cmdServe(ctx context.Context, cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "debug, info, warn or error")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log, err := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	return server.Run(ctx, cfg, log, version)
}

func cmdMigrate(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	log, err := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	db, _, err := server.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer db.Close()
	v, err := db.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "database %s is at schema version %d\n", cfg.DBPath(), v)
	return nil
}

func cmdWorkspace(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: werkbord-team workspace create --name <workspace> --owner <your name> [--email <you@example.com>]")
	}
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	name := fs.String("name", "", "the workspace's name")
	owner := fs.String("owner", "", "the owner's name (you)")
	email := fs.String("email", "", "the owner's email (optional)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	log, err := logging.New(stderr, "warn", cfg.LogFormat)
	if err != nil {
		return err
	}
	db, svc, err := server.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer db.Close()
	created, err := svc.CreateWorkspace(ctx, *name, *owner, *email)
	if err != nil {
		return err
	}
	host := cfg.ClientAddr()
	if h, p, err := net.SplitHostPort(host); err == nil && h == "::1" {
		host = net.JoinHostPort("localhost", p)
	}
	fmt.Fprintf(stdout, "Workspace %q created, owned by %s.\n\n", created.Workspace.Name, created.Owner.Name)
	fmt.Fprintf(stdout, "Owner token (shown once; it is not stored and cannot be shown again):\n\n  %s\n\n", created.Token)
	fmt.Fprintf(stdout, "Start the server with `werkbord-team serve`, then sign in at:\n\n  http://%s/#token=%s\n\n", host, created.Token)
	fmt.Fprintln(stdout, "Add the rest of the team from the console's Members tab.")
	return nil
}
