package teaminstall

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMaintenanceHealthChecksEachWorkspaceIdentity(t *testing.T) {
	base := service(t, map[string]string{
		"/api/device/v1/state":          `{"daemon":true,"version":"5.5.0","enrolled":true,"workspace":{"id":"one"}}`,
		"/w/second/api/device/v1/state": `{"daemon":true,"version":"5.5.0","enrolled":true,"workspace":{"id":"two"}}`,
	})
	targets := []maintenanceTarget{
		{Slot: "main", Base: base, Before: maintenanceState{Workspace: json.RawMessage(`{"id":"one"}`)}},
		{Slot: "second", Base: base + "/w/second", Before: maintenanceState{Workspace: json.RawMessage(`{"id":"two"}`)}},
	}
	if err := waitAllHealthy(context.Background(), testKey, "5.5.0", targets); err != nil {
		t.Fatal(err)
	}
	targets[1].Before.Workspace = json.RawMessage(`{"id":"some-other-team"}`)
	if err := waitAllHealthy(context.Background(), testKey, "5.5.0", targets); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("secondary identity was ignored: %v", err)
	}
}
