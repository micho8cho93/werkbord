package main

import (
	"strings"
	"testing"
	"time"
)

// `werkbord network` is for adding other computers as runners: off until asked for, signed in through the browser,
// and never mentioned by setup or by phone enrollment.
func TestTheNetworkCommandTurnsThePrivateNetworkOnAndOffForOtherComputers(t *testing.T) {
	e := newTestEnv(t)
	e.browseFn = func(url string) error {
		if url == e.node.authURL {
			go func() {
				time.Sleep(50 * time.Millisecond)
				e.node.signIn()
			}()
		}
		return nil
	}
	if err := e.app.cmdNetwork(bg, nil); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("network with nothing running: %v", err)
	}
	if err := e.app.cmdSetup(bg, []string{"--no-open"}); err != nil {
		t.Fatal(err)
	}
	e.out.Reset()
	if err := e.app.cmdNetwork(bg, nil); err != nil || !strings.Contains(e.output(), "off") || !strings.Contains(e.output(), "werkbord network on") {
		t.Fatalf("status: %v\n%s", err, e.output())
	}

	e.out.Reset()
	if err := e.app.cmdNetwork(bg, []string{"on"}); err != nil {
		t.Fatalf("on: %v\n%s", err, e.output())
	}
	if !strings.Contains(e.output(), "https://werkbord-test.tail1234.ts.net/") || !strings.Contains(e.output(), "Add runner") {
		t.Fatalf("on:\n%s", e.output())
	}
	if o := e.opened(); len(o) == 0 || o[len(o)-1] != e.node.authURL {
		t.Fatalf("the Tailscale sign-in page was not opened: %v", o)
	}
	e.out.Reset()
	if err := e.app.cmdNetwork(bg, []string{"status"}); err != nil || !strings.Contains(e.output(), "connected") || strings.Contains(e.output(), e.node.authURL) {
		t.Fatalf("status: %v\n%s", err, e.output())
	}
	e.out.Reset()
	if err := e.app.cmdNetwork(bg, []string{"off"}); err != nil || !strings.Contains(e.output(), "off") {
		t.Fatalf("off: %v\n%s", err, e.output())
	}
	if err := e.app.cmdNetwork(bg, []string{"sideways"}); err == nil {
		t.Fatal("an unknown subcommand was accepted")
	}
}
