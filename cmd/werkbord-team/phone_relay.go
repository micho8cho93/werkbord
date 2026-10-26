package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"path/filepath"
	"time"

	relayserver "werkbord/internal/relay/server"
	"werkbord/internal/team/config"
)

func cmdPhoneRelay(ctx context.Context, cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("phone-relay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:7432", "relay listen address; publish through an HTTPS reverse proxy")
	dir := fs.String("data-dir", filepath.Join(cfg.DataDir, "phone-relay"), "directory for room join-key hashes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: werkbord-team phone-relay [--addr ADDRESS] [--data-dir PATH]")
	}
	h, err := relayserver.Open(*dir)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.Close()
	return srv.Shutdown(stop)
}
