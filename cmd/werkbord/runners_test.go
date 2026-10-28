package main

import (
	"strings"
	"testing"
)

func TestRunnersCommandListsHeldWorkAndNeedsAnExplicitConfirmation(t *testing.T) {
	e := newTestEnv(t)
	spec, err := e.app.spec()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.mgr.Install(bg, spec); err != nil {
		t.Fatal(err)
	}
	if err = e.mgr.Start(bg); err != nil {
		t.Fatal(err)
	}
	if err = e.app.cmdRunners(bg, []string{"held"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.output(), "No work is held") {
		t.Fatalf("output: %q", e.output())
	}
	for name, args := range map[string][]string{
		"no subcommand": nil,
		"unknown":       {"release"},
		"no flag":       {"confirm-stopped", "rnr_x", "run_x"},
		"missing run":   {"confirm-stopped", "rnr_x", "--confirm-stopped"},
		"extra to held": {"held", "rnr_x"},
	} {
		if err = e.app.cmdRunners(bg, args); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// With the flag the request reaches the controller, which refuses a runner it does not know.
	if err = e.app.cmdRunners(bg, []string{"confirm-stopped", "rnr_missing", "run_missing", "--confirm-stopped"}); err == nil {
		t.Error("a release of unknown work succeeded")
	}
}
