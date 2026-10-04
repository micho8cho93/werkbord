// Command devboard runs the local controller and talks to it.
//
//	devboard setup                 install and start everything, then open the app
//	devboard start | stop | restart | status
//	devboard open [--phone]        open the app (on this computer, or the address for a phone)
//	devboard doctor                check that everything works
//	devboard update                install the latest release
//	devboard serve                 run the controller in the foreground
//	devboard migrate               apply database migrations and exit
//	devboard project add <path>    register an existing local Git repository
//	devboard project list          list registered projects
//	devboard token [--url]         print the API token (or a link that signs a browser in)
//	devboard version               print the version
//
// The service commands manage the controller as a background service (launchd,
// systemd, a scheduled task, or a detached process), never starting a second one.
// Commands other than those and serve and migrate are HTTP clients of a running
// controller: the controller is the only process that writes the database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"devboard/internal/config"
	"devboard/internal/controller"
	"devboard/internal/logging"
	"devboard/internal/store/sqlite"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: devboard <command> [flags]

commands:
  setup                 install the service, start Dev Board and open it (safe to run again)
  start | stop | restart  control the background controller
  status                what is running, and where
  open [--phone] [--qr] open the app; --phone shows the address (and QR code) for your phone
  doctor                check the controller, database, Git, agents, network and GitHub
  update                install the latest release
  uninstall             remove the login service (your data stays)
  logs [-n N]           show the end of the controller's log

  serve                 run the controller in the foreground
  migrate               apply database migrations and exit
  project add <path>    register an existing local Git repository
  project list          list registered projects
  token                 print the API token, or with --url a sign-in link for this computer
  version               print the version

Run "devboard <command> -h" for command flags.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var quiet silentError
		if !errors.Is(err, flag.ErrHelp) && !errors.As(err, &quiet) {
			fmt.Fprintln(os.Stderr, "devboard:", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return flag.ErrHelp
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	a := newApp(cfg, stdout, stderr)
	ctx := context.Background()
	switch args[0] {
	case "setup", "install":
		return a.cmdSetup(ctx, args[1:])
	case "start":
		return a.cmdStart(ctx, args[1:])
	case "stop":
		return a.cmdStop(ctx, args[1:])
	case "restart":
		return a.cmdRestart(ctx, args[1:])
	case "status":
		return a.cmdStatus(ctx, args[1:])
	case "open":
		return a.cmdOpen(ctx, args[1:])
	case "doctor":
		return a.cmdDoctor(ctx, args[1:])
	case "update":
		return a.cmdUpdate(ctx, args[1:])
	case "uninstall":
		return a.cmdUninstall(ctx, args[1:])
	case "logs":
		return a.cmdLogs(ctx, args[1:])
	case "serve":
		return cmdServe(cfg, args[1:], stderr)
	case "migrate":
		return cmdMigrate(cfg, args[1:], stdout, stderr)
	case "project", "projects":
		return cmdProject(cfg, args[1:], stdout, stderr)
	case "token":
		return cmdToken(cfg, args[1:], stdout, stderr)
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

// commonFlags registers flags shared by every command that reads config.
func commonFlags(fs *flag.FlagSet, cfg *config.Config) {
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directory for the database, lock and token")
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "controller listen address")
}

func cmdServe(cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "debug, info, warn or error")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "text or json")
	fs.BoolVar(&cfg.RequireToken, "require-token", cfg.RequireToken,
		"require the API token on loopback too (the default; --require-token=false turns it off, and only ever applies to 127.0.0.1)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log, err := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return controller.New(cfg, log, version).Run(ctx)
}

func cmdMigrate(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	log, err := logging.New(stderr, "info", "text")
	if err != nil {
		return err
	}
	db, err := sqlite.Open(context.Background(), cfg.DBPath(), log)
	if err != nil {
		return err
	}
	defer db.Close()
	v, err := db.SchemaVersion(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "database %s is at schema version %d\n", cfg.DBPath(), v)
	return nil
}

// cmdToken prints the API token so it can be pasted into the app on another
// device, or with --url a link that signs a browser on this computer in.
func cmdToken(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	asURL := fs.Bool("url", false, "print a sign-in link for a browser on this computer instead of the bare token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: devboard token [--url]")
	}
	if !cfg.AuthRequired() {
		return errors.New("API authentication is disabled for this configuration, so there is no token")
	}
	tok, err := cfg.ResolveToken(false)
	if err != nil {
		return err
	}
	if tok == "" {
		return errors.New("no token yet: start the controller with `devboard serve` and it will create one")
	}
	if *asURL {
		fmt.Fprintf(stdout, "http://%s/#token=%s\n", cfg.ClientAddr(), tok)
	} else {
		fmt.Fprintln(stdout, tok)
	}
	return nil
}

func cmdProject(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: devboard project add <path> | devboard project list")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("project "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	name := fs.String("name", "", "display name (defaults to the repository directory name)")
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
	case "add":
		if len(positional) != 1 {
			return errors.New("usage: devboard project add <path> [--name NAME]")
		}
		p, err := c.registerProject(ctx, positional[0], *name)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "registered %s (%s)\n  path:   %s\n", p.Name, p.ID, p.RepoPath)
		if p.Repository != nil {
			fmt.Fprintf(stdout, "  branch: %s\n", orDash(p.Repository.CurrentBranch))
		}
		return nil
	case "list", "ls":
		ps, err := c.listProjects(ctx)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Fprintln(stdout, "no projects registered; add one with: devboard project add <path>")
			return nil
		}
		for _, p := range ps {
			branch := "-"
			if p.Repository != nil {
				branch = orDash(p.Repository.CurrentBranch)
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", p.ID, p.Name, branch, p.RepoPath)
		}
		return nil
	default:
		return fmt.Errorf("unknown project command %q", sub)
	}
}

// parseInterspersed parses flags that appear before or after positional
// arguments; the standard flag package stops at the first positional one.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
