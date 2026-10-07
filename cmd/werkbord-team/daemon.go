package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"io"
	"os"
	"strings"

	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

// licenseIssuer is stamped into both the service and the app at release time. Never a private key.
var licenseIssuer string

func cmdDaemon(ctx context.Context, cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	local := fs.String("local-addr", "127.0.0.1:7431", "device service address, loopback only")
	keyPath := fs.String("local-key-file", "", "native installation's GUI credential")
	runner := fs.String("runner-config", "", "this user's individual Werkbord settings path")
	fs.StringVar(&cfg.BackupDir, "backup-dir", cfg.BackupDir, "this host's backup folder")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var key string
	if *keyPath != "" {
		b, err := os.ReadFile(*keyPath)
		if err != nil {
			return err
		}
		key = strings.TrimSpace(string(b))
		if _, err := hex.DecodeString(key); err != nil || len(key) != 64 {
			return flag.ErrHelp
		}
	}
	pub, _ := base64.RawURLEncoding.DecodeString(licenseIssuer)
	log, err := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	d, err := server.NewDaemon(server.DaemonOptions{Config: cfg, LocalAddr: *local, LocalKey: key, RunnerConfigPath: *runner, LicenseKey: pub, Version: version, Log: log})
	if err != nil {
		return err
	}
	return d.Run(ctx)
}
