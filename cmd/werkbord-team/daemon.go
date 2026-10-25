package main

import (
	"context"
	"encoding/hex"
	"flag"
	"io"
	"os"
	"strings"

	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

// testPremium is set to "1" only by the test scripts that build this executable (-ldflags "-X main.testPremium=1") to run
// teams larger than the Free plan's two people. No release script sets it (internal/archtest checks); the unlock key replaces
// it. Empty in every shipped build.
var testPremium string

func cmdDaemon(ctx context.Context, cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	local := fs.String("local-addr", "127.0.0.1:7431", "device service address, loopback only")
	keyPath := fs.String("local-key-file", "", "native installation's GUI credential")
	runner := fs.String("runner-config", "", "this user's individual Werkbord settings path")
	fs.StringVar(&cfg.PassphraseFile, "passphrase-file", cfg.PassphraseFile, "protected file used to seal Team keys instead of OS secure storage")
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
	log, err := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	// One local service for every Team workspace on this computer; the first is where a single-workspace installation
	// always kept its data, so an upgrade moves nothing.
	hub, err := server.NewHub(server.DaemonOptions{Config: cfg, LocalAddr: *local, LocalKey: key, RunnerConfigPath: *runner, Premium: cfg.Premium, Version: version, Log: log})
	if err != nil {
		return err
	}
	return hub.Run(ctx)
}
