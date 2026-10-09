package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// service stands in for the running Team service: each path answers what it is given, and every other path answers 404.
func service(t *testing.T, answers map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}
		body, ok := answers[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAServiceThatHoldsAWorkspaceIsRecognisedBeforeAnAdministratorIsAsked(t *testing.T) {
	const idleSlot = `{"slot":"main","enrolled":false,"connected":false}`
	for name, tc := range map[string]struct {
		answers  map[string]string
		deferred bool
	}{
		"current service, enrolled slot":    {map[string]string{"/api/device/v1/workspaces": `{"workspaces":[` + idleSlot + `,{"slot":"ws_1","enrolled":true}]}`}, true},
		"current service, joining slot":     {map[string]string{"/api/device/v1/workspaces": `{"workspaces":[{"slot":"main","pending":true}]}`}, true},
		"current service, leaving slot":     {map[string]string{"/api/device/v1/workspaces": `{"workspaces":[{"slot":"main","leaving":true}]}`}, true},
		"current service, creating":         {map[string]string{"/api/device/v1/workspaces": `{"workspaces":[{"slot":"main","operation":"Creating"}]}`}, true},
		"current service, nothing on it":    {map[string]string{"/api/device/v1/workspaces": `{"workspaces":[` + idleSlot + `]}`, "/api/device/v1/state": `{"daemon":true,"version":"3.10.0"}`}, false},
		"older service, enrolled":           {map[string]string{"/api/device/v1/state": `{"daemon":true,"version":"3.0.1","enrolled":true,"workspace":{"id":"w","name":"Acme"}}`}, true},
		"older service, workspace only":     {map[string]string{"/api/device/v1/state": `{"daemon":true,"version":"3.0.1","workspace":{"id":"w","name":"Acme"}}`}, true},
		"older service, joining":            {map[string]string{"/api/device/v1/state": `{"daemon":true,"version":"3.0.1","pending":true}`}, true},
		"older service, nothing on it":      {map[string]string{"/api/device/v1/state": `{"daemon":true,"version":"3.0.1","workspace":null}`}, false},
		"something that is not the service": {map[string]string{"/api/device/v1/state": `{"enrolled":true}`}, false},
		"an answer that is not JSON":        {map[string]string{"/api/device/v1/state": `<html>`, "/api/device/v1/workspaces": `<html>`}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := preflightReplacement(context.Background(), service(t, tc.answers), testKey)
			if tc.deferred != errors.Is(err, ErrReplacementDeferred) || (!tc.deferred && err != nil) {
				t.Fatalf("deferred=%v, got %v", tc.deferred, err)
			}
		})
	}
}

func TestTheServiceMustAnswerToThePersonsCredentialAndNothingElseDecidesAnything(t *testing.T) {
	enrolled := map[string]string{"/api/device/v1/state": `{"daemon":true,"enrolled":true}`}
	// A service that does not know this credential cannot be asked: the installer's own check under administrator authority decides.
	if err := preflightReplacement(context.Background(), service(t, enrolled), strings.Repeat("0", 64)); err != nil {
		t.Fatalf("an unanswered question was taken for an answer: %v", err)
	}
	// Nothing listening is not a refusal either.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if err := preflightReplacement(context.Background(), url, testKey); err != nil {
		t.Fatalf("a service that is not running was refused: %v", err)
	}
}

func TestARedirectFromTheServiceIsNotFollowed(t *testing.T) {
	target := service(t, map[string]string{"/api/device/v1/state": `{"daemon":true,"enrolled":true}`})
	redirect := httptest.NewServer(http.RedirectHandler(target+"/api/device/v1/state", http.StatusFound))
	defer redirect.Close()
	if err := preflightReplacement(context.Background(), redirect.URL, testKey); err != nil {
		t.Fatalf("a redirect was followed: %v", err)
	}
}

func TestTheInstallersRefusalIsGivenToThePersonAsItsOwnWordsAndAnythingElseKeepsItsDetail(t *testing.T) {
	framed := "0:279: execution error: " + ErrReplacementDeferred.Error() + " (1)\n"
	if err := installerRefusal(ActionInstallIsolated, framed); !errors.Is(err, ErrReplacementDeferred) || strings.Contains(err.Error(), "execution error") || strings.Contains(err.Error(), "install-isolated") {
		t.Fatalf("the refusal was not given as itself: %v", err)
	}
	// A refusal that came with another failure (a rollback that did not finish) must not hide the other failure.
	joined := "0:279: execution error: " + ErrReplacementDeferred.Error() + "\nautomatic rollback failed; backups retained: boom (1)"
	err := installerRefusal(ActionInstallIsolated, joined)
	if errors.Is(err, ErrReplacementDeferred) || !strings.Contains(err.Error(), "automatic rollback failed") {
		t.Fatalf("a rollback failure was hidden behind the refusal: %v", err)
	}
	err = installerRefusal(ActionUninstall, "0:12: execution error: Team service is still running; installation and data have been preserved (1)")
	if err == nil || err.Error() != "macOS could not remove the Team service: Team service is still running; installation and data have been preserved" {
		t.Fatalf("%v", err)
	}
	if got := installerDetail("plain text"); got != "plain text" {
		t.Fatalf("%q", got)
	}
}

func TestAWorkspaceOnDiskIsRefusedWithTheWordsThePersonReads(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data", "workspace"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CheckReplacement(filepath.Join(root, "data")); !errors.Is(err, ErrReplacementDeferred) {
		t.Fatalf("%v", err)
	}
}
