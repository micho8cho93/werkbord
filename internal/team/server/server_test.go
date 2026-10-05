package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"devboard/internal/team/config"
)

// A server shuts down promptly even while members have change requests open: the
// sync long poll must not hold shutdown for its whole wait.
func TestShutdownDoesNotWaitForOpenSyncRequests(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg := config.Default()
	cfg.Addr, cfg.DataDir = addr, t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, svc, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateWorkspace(ctx, "Acme", "Ada", "")
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	ran := make(chan error, 1)
	go func() { ran <- Run(ctx, cfg, slog.New(slog.DiscardHandler), "test") }()
	base := "http://" + addr
	var up bool
	for i := 0; i < 100 && !up; i++ {
		if res, err := http.Get(base + "/api/team/v1/health"); err == nil {
			res.Body.Close()
			up = true
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !up {
		t.Fatal("the server did not start")
	}

	// Hold a real wait open: since equals the current revision, so nothing answers it.
	req, _ := http.NewRequest("GET", base+"/api/team/v1/sync?wait=0", nil)
	req.Header.Set("Authorization", "Bearer "+created.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Revision int64 }
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	long := make(chan struct{})
	go func() {
		defer close(long)
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/team/v1/sync?since=%d&after=0&wait=20", base, body.Revision), nil)
		req.Header.Set("Authorization", "Bearer "+created.Token)
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}()
	time.Sleep(200 * time.Millisecond)

	began := time.Now()
	cancel()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("shutdown took %s with a sync request open", d)
	}
	<-long
}
