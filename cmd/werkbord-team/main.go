// Command werkbord-team runs Werkbord Team: the shared workspace where a team's
// members and projects are coordinated.
//
//	werkbord-team workspace create   start a licensed workspace and its customer-owned Nebula network
//	werkbord-team serve              run the server in the foreground
//	werkbord-team network …          the private network: status, invitations, approvals, a host's node
//	werkbord-team device …           join as a host, list and revoke devices
//	werkbord-team host …             add a Workspace Host (its keys and a copy of the data), remove one
//	werkbord-team storage …          where the workspace's data is: status, backups, moving it into a cluster
//	werkbord-team migrate            apply database migrations and exit
//	werkbord-team version            print the version
//
// Team is a separate product from the individual Werkbord (cmd/werkbord): its own
// executable, version, data directory and database. It coordinates; it never
// runs anyone's agents or commands. Every member keeps using their own Werkbord
// runner with their own credentials (docs/STRUCTURE.md).
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/license"
	"devboard/internal/team/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: werkbord-team <command> [flags]

commands:
  daemon             run this device's background service (workspace, networking and local runner bridge)
  workspace create   start a workspace with Nebula; prints the owner's local token once
  serve              run the Team server in the foreground
  network            the private network: status, invite, pending, approve, deny, node
  device             join as a host, list and revoke devices
  host               promote a device to Workspace Host (keys and a copy of the data), collect them on it, remove a host
  storage            where the workspace's data is kept: status, backup, restore, move it into a cluster
  migrate            apply database migrations and exit
  license import     replace the workspace's signed offline license (owner only)
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
	cfg.LicenseKey, _ = base64.RawURLEncoding.DecodeString(licenseIssuer)
	switch args[0] {
	case "daemon":
		return cmdDaemon(ctx, cfg, args[1:], stderr)
	case "serve":
		return cmdServe(ctx, cfg, args[1:], stderr)
	case "migrate":
		return cmdMigrate(ctx, cfg, args[1:], stdout, stderr)
	case "workspace":
		return cmdWorkspace(ctx, cfg, args[1:], stdout, stderr)
	case "network":
		return cmdNetwork(ctx, cfg, args[1:], stdout, stderr)
	case "device":
		return cmdDevice(ctx, cfg, args[1:], stdout, stderr)
	case "host":
		return cmdHost(ctx, cfg, args[1:], stdout, stderr)
	case "storage":
		return cmdStorage(ctx, cfg, args[1:], stdout, stderr)
	case "license":
		return cmdLicense(ctx, cfg, args[1:], stdout, stderr)
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
	var opt server.RunOptions
	if src, ok := deviceSource(cfg, log); ok {
		// A host that joined as a device can learn what its network node should be from a Workspace Host's API, which it needs
		// to do while its own database is not up yet (it has just been made a Workspace Host and is joining the cluster).
		opt.NodeFallback = src
	}
	return server.RunWith(ctx, cfg, log, version, opt)
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
	withNetwork := fs.Bool("network", true, "create the customer's Nebula network (default); --network=false is loopback evaluation only")
	var endpoints listFlag
	fs.Var(&endpoints, "endpoint", "with --network: a host (address or DNS name) at which this machine can be reached from outside its network (repeatable; $WERKBORD_TEAM_ENDPOINTS)")
	connectivity := fs.String("connectivity", "auto", "with --network: make this host a Connectivity Host: auto (if an --endpoint can be reached from outside), yes or no")
	approval := fs.String("approval", "admin", "with --network: admin (default; approves every device) or auto (an invitation is enough)")
	netRange := fs.String("network-range", "", "with --network: the private network's address range (a /16 to /24 in 10/8, 172.16/12 or 192.168/16; default: a random /16 in 10.128.0.0/9)")
	fs.StringVar(&cfg.BootstrapAddr, "bootstrap-addr", cfg.BootstrapAddr, "with --network: where this host answers devices that are joining")
	fs.IntVar(&cfg.NetworkPort, "network-port", cfg.NetworkPort, "with --network: the UDP port of the network node")
	fs.StringVar(&cfg.PassphraseFile, "passphrase-file", cfg.PassphraseFile, "seal keys with this protected external passphrase file instead of OS secure storage")
	fs.StringVar(&cfg.Storage, "storage", cfg.Storage, "where the workspace's data is kept: replicated (a cluster of Workspace Hosts, which starts as one host; the default) or single-file (one SQLite file: valid for evaluation, no copy but its backups)")
	fs.IntVar(&cfg.StoragePort, "storage-port", cfg.StoragePort, "with replicated storage: the port of this host's database node (HTTP)")
	fs.IntVar(&cfg.StorageRaftPort, "storage-raft-port", cfg.StorageRaftPort, "with replicated storage: the port of this host's database node (Raft)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if len(endpoints) > 0 {
		cfg.Endpoints = endpoints
	}
	if !*withNetwork && !cfg.IsLoopback() {
		return errors.New("a workspace without Nebula must listen on loopback for evaluation")
	}
	if cfg.LicenseRequired {
		raw, err := readLicenseFile(cfg.LicensePath())
		if err != nil {
			return errors.New("supply a signed offline license with WERKBORD_TEAM_LICENSE_FILE before creating a workspace")
		}
		if _, err := license.Verify(raw, cfg.LicenseKey, time.Now()); err != nil {
			return err
		}
	}
	if *withNetwork {
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	log, err := logging.New(stderr, "warn", cfg.LogFormat)
	if err != nil {
		return err
	}
	// A host whose workspace has a private network names its database node after its own device, so its keys are made first.
	var hostKeys *pki.HostKeys
	nodeID := ""
	if *withNetwork && cfg.Storage == config.StorageReplicated {
		if hostKeys, err = pki.NewHostKeys(); err != nil {
			return err
		}
		nodeID = hostKeys.DeviceID()
	}
	w, err := server.CreateStorage(ctx, cfg, log, nodeID)
	if err != nil {
		return err
	}
	defer w.Close()
	svc := w.Service
	created, err := svc.CreateWorkspace(ctx, *name, *owner, *email)
	if err != nil {
		return err
	}
	host := cfg.ClientAddr()
	if h, p, err := net.SplitHostPort(host); err == nil && h == "::1" {
		host = net.JoinHostPort("localhost", p)
	}
	var nc *server.NetworkCreated
	if *withNetwork {
		n, err := server.CreateNetwork(ctx, cfg, log, svc, created, server.NetworkOptions{Range: *netRange, Connectivity: *connectivity, Approval: domain.ApprovalPolicy(*approval), HostKeys: hostKeys})
		if err != nil {
			return fmt.Errorf("the workspace %q was created, but its private network was not: %w\nIts data is in %s; start again with another --data-dir, or remove that directory", created.Workspace.Name, err, cfg.DataDir)
		}
		nc = &n
		credential, err := svc.IssueLocalDeviceCredential(ctx, created.Workspace.ID, n.HostDeviceID)
		if err != nil {
			return err
		}
		sealer, err := server.SealerFor(cfg)
		if err != nil {
			return err
		}
		vault, err := pki.OpenVault(cfg.PKIDir(), sealer)
		if err != nil {
			return err
		}
		if err := vault.SaveDeviceToken(credential); err != nil {
			return err
		}
		if err := vault.SaveSecret("member", []byte(created.Owner.ID)); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Workspace %q created, owned by %s.\n\n", created.Workspace.Name, created.Owner.Name)
	fmt.Fprintf(stdout, "Owner token (shown once; it is not stored and cannot be shown again):\n\n  %s\n\n", created.Token)
	fmt.Fprintf(stdout, "Start the server with `werkbord-team serve`, then sign in at:\n\n  http://%s/#token=%s\n\n", host, created.Token)
	fmt.Fprintln(stdout, "Add the rest of the team from the console's Members tab.")
	if cfg.Storage == config.StorageReplicated {
		fmt.Fprintf(stdout, "\nThe workspace's data is kept in a cluster of Workspace Hosts, which for now is this one host: valid, and with no high availability.\nAdd two more hosts for three, which tolerate the loss of one (docs/TEAM_STORAGE.md). Replication is not a backup: set WERKBORD_TEAM_BACKUP_DIR.\n")
	} else {
		fmt.Fprintf(stdout, "\nThe workspace's data is kept in one file on this host (--storage single-file): there is no copy of it but your backups. `werkbord-team storage migrate` moves it into a cluster.\n")
	}
	if nc != nil {
		printNetworkCreated(stdout, cfg, *nc)
	}
	return nil
}

func printNetworkCreated(w io.Writer, cfg config.Config, nc server.NetworkCreated) {
	fmt.Fprintf(w, "\nPrivate network %s created. This host is its first Workspace Host (%s, at %s on it).\n", nc.Range, nc.HostDeviceID, nc.HostAddress)
	fmt.Fprintf(w, "Workspace fingerprint (what every device pins; read it out when you invite someone):\n\n  %s\n\n", nc.Fingerprint)
	fmt.Fprintf(w, "The workspace's keys, sealed, are in %s. This host now holds the workspace's signing key and the network authority's:\nit is a high-trust machine, and those keys are never in the database or in anything the API returns. Back up %s with the data, and keep the sealing key somewhere else.\n", cfg.PKIDir(), cfg.PKIDir())
	if nc.ConnectivityHost {
		fmt.Fprintf(w, "\nThis host is also a Connectivity Host: it helps devices find each other and relays for them, at %s. Forward UDP %d and TCP %d to it.\n", strings.Join(nc.NetworkEndpoints, ", "), cfg.NetworkPort, cfg.BootstrapPort())
	}
	for _, warn := range nc.Warnings {
		fmt.Fprintf(w, "\nWARNING: %s\n", warn)
	}
	fmt.Fprintln(w, "\nStart it with `werkbord-team serve` (the network node needs the privileges to create a network interface; see docs/TEAM_NETWORK.md, \"Privileges\").")
	fmt.Fprintln(w, "Then invite people with `werkbord-team network invite`.")
}
