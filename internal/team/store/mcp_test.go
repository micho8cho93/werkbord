package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"werkbord/internal/team/domain"
)

func TestMCPGrantsPersistScopesAndRevokeByCredential(t *testing.T) {
	db := openDB(t)
	ada, _ := seedWorkspace(t, db, "tws_1")
	grant := domain.MCPGrant{ID: "tmc_one", WorkspaceID: "tws_1", MemberID: ada, Name: "planner",
		Projects: []string{"tpj_shop"}, Operations: []string{"list_projects", "create_ticket"}, TokenHash: "digest", CreatedAt: t0}
	if err := db.Update(context.Background(), func(tx Tx) error { return tx.InsertMCPGrant(context.Background(), grant) }); err != nil {
		t.Fatal(err)
	}
	var got domain.MCPGrant
	if err := db.View(context.Background(), func(tx Tx) error {
		var err error
		got, err = tx.MCPGrantByTokenHash(context.Background(), "digest")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.ID != grant.ID || got.Name != grant.Name || len(got.Projects) != 1 || got.Projects[0] != "tpj_shop" || len(got.Operations) != 2 {
		t.Fatalf("stored grant = %+v", got)
	}
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	var revoked bool
	if err := db.Update(context.Background(), func(tx Tx) (err error) {
		revoked, err = tx.RevokeMCPGrant(context.Background(), "tws_1", ada, grant.ID, at)
		return err
	}); err != nil || !revoked {
		t.Fatalf("revoke = %v, err=%v", revoked, err)
	}
	if err := db.View(context.Background(), func(tx Tx) error {
		var err error
		got, err = tx.MCPGrantByTokenHash(context.Background(), "digest")
		return err
	}); err != nil || got.RevokedAt == nil || !got.RevokedAt.Equal(at) {
		t.Fatalf("revoked grant = %+v, err=%v", got, err)
	}
	var second bool
	if err := db.Update(context.Background(), func(tx Tx) (err error) {
		second, err = tx.RevokeMCPGrant(context.Background(), "tws_1", ada, grant.ID, at.Add(time.Minute))
		return err
	}); err != nil || second {
		t.Fatalf("grant revoked more than once: changed=%v err=%v", second, err)
	}
	if err := db.Update(context.Background(), func(tx Tx) error { return tx.InsertMCPGrant(context.Background(), grant) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate MCP credential hash: %v", err)
	}
}
