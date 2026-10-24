package teaminstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceAdmitsASettledWorkspaceOnly(t *testing.T) {
	for _, slot := range []string{"", "slots/second"} {
		root := t.TempDir()
		dir := filepath.Join(root, slot)
		if err := os.MkdirAll(filepath.Join(dir, "workspace"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := CheckMaintenance(root); err != nil {
			t.Fatalf("settled workspace refused (%q): %v", slot, err)
		}
		// The ordinary installer still refuses the same directory.
		if err := CheckReplacement(root); !errors.Is(err, ErrReplacementDeferred) {
			t.Fatalf("ordinary replacement must still be refused, got %v", err)
		}
		for _, state := range []string{"pending", "leaving", "demoting"} {
			if err := os.MkdirAll(filepath.Join(dir, state), 0700); err != nil {
				t.Fatal(err)
			}
			if err := CheckMaintenance(root); !errors.Is(err, ErrMaintenanceBlocked) {
				t.Fatalf("%s accepted: %v", state, err)
			}
			os.RemoveAll(filepath.Join(dir, state))
		}
	}
	if err := CheckMaintenance(t.TempDir()); !errors.Is(err, ErrNothingToMaintain) {
		t.Fatalf("a service with no workspace needs the ordinary update, got %v", err)
	}
	root := t.TempDir()
	elsewhere := t.TempDir()
	os.MkdirAll(filepath.Join(elsewhere, "x", "workspace"), 0700)
	if err := os.Symlink(elsewhere, filepath.Join(root, "slots")); err != nil {
		t.Fatal(err)
	}
	if err := CheckMaintenance(root); err == nil {
		t.Fatal("a symlinked slots directory was accepted")
	}
}

const samplePlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>Label</key><string>dev.werkbord.team</string>
<key>ProgramArguments</key><array><string>/Library/Application Support/Werkbord Team/Helpers/werkbord-team</string><string>daemon</string><string>--data-dir</string><string>/Library/Application Support/Werkbord Team/data</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>`

func TestBackupDirectoryIsTheOnlyChangeToTheServiceDefinition(t *testing.T) {
	out, dir, err := withBackupDir([]byte(samplePlist), "/Library/Application Support/Werkbord Team/backups")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/Library/Application Support/Werkbord Team/backups" {
		t.Fatal(dir)
	}
	want := strings.Replace(samplePlist, "</array>", "<string>--backup-dir</string><string>/Library/Application Support/Werkbord Team/backups</string></array>", 1)
	if string(out) != want {
		t.Fatalf("definition changed beyond the argument:\n%s", out)
	}
	again, dir2, err := withBackupDir(out, "/elsewhere")
	if err != nil || string(again) != string(out) || dir2 != dir {
		t.Fatalf("an existing backup directory must be kept: %v %s", err, dir2)
	}
	if _, _, err := withBackupDir([]byte(strings.Replace(samplePlist, "dev.werkbord.team", "com.other", 1)), "/b"); err == nil {
		t.Fatal("another service's definition was edited")
	}
	if _, _, err := withBackupDir([]byte(samplePlist), "relative"); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, _, err := withBackupDir([]byte(samplePlist), "/a</string><string>x"); err == nil {
		t.Fatal("markup in the path accepted")
	}
}

func TestMaintenanceHealth(t *testing.T) {
	ws := json.RawMessage(`{"id":"w"}`)
	good := maintenanceState{Daemon: true, Version: "v2", Enrolled: true, Connected: true, Workspace: ws}
	if err := beforeMaintenance(good); err != nil {
		t.Fatal(err)
	}
	if beforeMaintenance(maintenanceState{Daemon: true, Enrolled: true, Workspace: json.RawMessage("null")}) == nil {
		t.Fatal("a service with no open workspace was maintained")
	}
	busy := good
	busy.Operation = "leaving"
	if !errors.Is(beforeMaintenance(busy), ErrMaintenanceBlocked) {
		t.Fatal("a changing workspace was maintained")
	}
	if err := afterMaintenance(good, "2", true); err != nil {
		t.Fatal(err)
	}
	wrong := good
	wrong.Version = "v1"
	sealed := good
	sealed.Workspace = nil
	offline := good
	offline.Connected = false
	for name, s := range map[string]maintenanceState{"version": wrong, "unsealed": sealed, "offline": offline} {
		if afterMaintenance(s, "v2", true) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if afterMaintenance(offline, "v2", false) != nil {
		t.Fatal("a workspace that was offline before must not be required online")
	}
}

func TestBackupIsRequestedAsTheDeviceAndMustComplete(t *testing.T) {
	var gotAuth, gotMethod string
	reply := `{"configured":true,"lastOk":true}`
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMethod = r.Header.Get("Authorization"), r.Method+" "+r.URL.Path
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	defer srv.Close()
	if err := requestBackup(context.Background(), srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer k" || gotMethod != "POST /api/team/v1/storage/backup" {
		t.Fatal(gotAuth, gotMethod)
	}
	reply = `{"configured":true,"lastOk":false,"lastError":"disk full"}`
	if requestBackup(context.Background(), srv.URL, "k") == nil {
		t.Fatal("failed backup accepted")
	}
	reply, status = `{"configured":false,"lastOk":false}`, 200
	if requestBackup(context.Background(), srv.URL, "k") == nil {
		t.Fatal("unconfigured backup accepted")
	}
	reply, status = `{"error":"no"}`, 403
	if requestBackup(context.Background(), srv.URL, "k") == nil {
		t.Fatal("refusal accepted")
	}
}

func writeBackup(t *testing.T, dir, name string, content []byte, at time.Time) {
	t.Helper()
	sum := sha256.Sum256(content)
	rec, _ := json.Marshal(map[string]any{"sha256": hex.EncodeToString(sum[:]), "size": len(content)})
	if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), rec, 0600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(filepath.Join(dir, name), at, at)
}

func TestBackupMustBeNewIntactAndReadableByTheNewProgram(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeBackup(t, dir, "workspace-old.sqlite", []byte("old"), now.Add(-time.Hour))
	if _, err := newestBackup(dir, now.Add(-time.Minute)); err == nil {
		t.Fatal("an old backup counted as the new one")
	}
	writeBackup(t, dir, "workspace-new.sqlite", []byte("data"), now)
	name, err := newestBackup(dir, now.Add(-time.Minute))
	if err != nil || name != "workspace-new.sqlite" {
		t.Fatal(name, err)
	}
	if err := checkBackupFile(dir, name); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, name), []byte("dat!"), 0600)
	if checkBackupFile(dir, name) == nil {
		t.Fatal("a damaged backup passed")
	}
	if checkBackupFile(dir, "../x.sqlite") == nil {
		t.Fatal("path accepted as a name")
	}

	var ran []string
	orig := runProgram
	defer func() { runProgram = orig }()
	runProgram = func(_ context.Context, prog string, args ...string) ([]byte, error) {
		ran = append([]string{prog}, args...)
		return []byte("cannot restore"), errors.New("exit 1")
	}
	if verifyBackupWith(context.Background(), "/new/werkbord-team", dir, name) == nil {
		t.Fatal("a backup the new program cannot read passed")
	}
	if strings.Join(ran, " ") != "/new/werkbord-team storage verify-backup workspace-new.sqlite --dir "+dir {
		t.Fatal(ran)
	}
}
