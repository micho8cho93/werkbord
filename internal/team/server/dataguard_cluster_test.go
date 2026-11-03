package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"werkbord/internal/team/service"
)

func TestAClusterWorkspaceIsBackedUpAndCheckedBeforeANewVersionMigratesIt(t *testing.T) {
	cfg := storageCfg(t)
	cfg.BackupDir = t.TempDir() + "/backups"
	w, err := CreateStorage(bg, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := w.Service.CreateWorkspace(bg, "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recordVersion(t, cfg, "5.4.0")

	start := func(version string) (*Storage, *service.Service) {
		st, err := openStorage(bg, cfg, nil, newDataGuard(cfg, "", version, nil))
		if err != nil {
			t.Fatal(err)
		}
		svc := service.New(st.DB())
		st.Attach(svc, nil)
		st.Start(bg)
		return st, svc
	}
	st, svc := start("5.5.0")
	if err := st.WaitReady(bg, 2*time.Minute); err != nil {
		_ = st.Close(bg)
		t.Fatal(err)
	}
	if a := mustAuth(t, svc, created.Token); a.Workspace.Name != "Acme" {
		t.Fatalf("%+v", a)
	}
	_ = st.Close(bg)
	names := backupsIn(t, cfg.BackupDir)
	if len(names) != 1 {
		t.Fatalf("backups: %v", names)
	}
	rec, _ := readGuardRecord(cfg.DataDir)
	if rec.Version != "5.5.0" || rec.From != "5.4.0" || rec.Backup != names[0] || rec.Refused != nil {
		t.Fatalf("%+v", rec)
	}

	// The same version again: no second backup.
	st, _ = start("5.5.0")
	if err := st.WaitReady(bg, 2*time.Minute); err != nil {
		_ = st.Close(bg)
		t.Fatal(err)
	}
	_ = st.Close(bg)
	if got := backupsIn(t, cfg.BackupDir); len(got) != 1 {
		t.Fatalf("a restart of the same version backed up again: %v", got)
	}
}

func TestAClusterWorkspaceThatCannotBeBackedUpIsNotOpenedAndKeepsItsData(t *testing.T) {
	cfg := storageCfg(t)
	blocked := t.TempDir() + "/backups"
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.BackupDir = blocked
	w, err := CreateStorage(bg, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := w.Service.CreateWorkspace(bg, "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recordVersion(t, cfg, "5.4.0")

	st, err := openStorage(bg, cfg, nil, newDataGuard(cfg, "", "5.5.0", nil))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(st.DB())
	st.Attach(svc, nil)
	st.Start(bg)
	err = st.WaitReady(bg, 12*time.Second)
	if err == nil {
		_ = st.Close(bg)
		t.Fatal("the workspace opened although it could not be backed up")
	}
	if !strings.Contains(err.Error(), "did not become ready") && !strings.Contains(err.Error(), "backup") {
		t.Logf("not ready: %v", err)
	}
	if _, err := svc.Authenticate(bg, created.Token); err == nil {
		_ = st.Close(bg)
		t.Fatal("the workspace served a request before its data was protected")
	}
	_ = st.Close(bg)
	view := guardView(cfg.DataDir)
	if view["state"] != "refused" || view["version"] != "5.4.0" || !strings.Contains(view["reason"].(string), "backup directory") {
		t.Fatalf("%v", view)
	}

	// Nothing was migrated or changed: the workspace opens as before once the data is let through.
	cfg.BackupDir = ""
	w2, err := OpenWorkspace(bg, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if a := mustAuth(t, w2.Service, created.Token); a.Workspace.Name != "Acme" {
		t.Fatalf("%+v", a)
	}
}
