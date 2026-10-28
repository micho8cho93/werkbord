package replicated

import (
	"strings"
	"testing"
	"time"
	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/infra/rqlite/rqlitetest"
	"werkbord/internal/team/store"
)

func TestProductMapProposalsAndPublishedSnapshotsReplicate(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := []*Store{open(t, c, 0), open(t, c, 1), open(t, c, 2)}
	k := seed(t, hosts[0])
	settle(t, hosts...)
	snapshot := integration.ProductMapSnapshot{Schema: integration.ProductMapSchema, ID: "map_test", Version: 1, Repository: "github.com/acme/shop", Branch: "main", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), SourceDigest: strings.Repeat("c", 64), Rules: 1, CreatedAt: time.Now(), Content: integration.ProductMapContent{Features: []integration.ProductFeature{{ID: "f", Name: "Feature", Evidence: []integration.ProductEvidence{{Path: "main.go", Start: 1, End: 2}}}}}}
	proposal := domain.ProductMapRecord{ID: "proposal_1", ProjectID: k.project, AuthorID: k.owner, State: "proposed", CreatedAt: time.Now(), Snapshot: snapshot}
	if err := hosts[0].Update(bg, func(tx store.Tx) error { return tx.SaveProductMap(bg, k.ws, k.project, proposal, true) }); err != nil {
		t.Fatal(err)
	}
	settle(t, hosts...)
	if err := hosts[1].View(bg, func(tx store.Tx) error {
		r, err := tx.ProductMap(bg, k.ws, k.project, proposal.ID)
		if err == nil && r.State != "proposed" {
			t.Fatal("proposal lost")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	proposal.State = "published"
	proposal.Version = 1
	proposal.ReviewerID = k.owner
	if err := hosts[1].Update(bg, func(tx store.Tx) error { return tx.SaveProductMap(bg, k.ws, k.project, proposal, false) }); err != nil {
		t.Fatal(err)
	}
	settle(t, hosts...)
	if err := hosts[2].View(bg, func(tx store.Tx) error {
		records, err := tx.ProductMaps(bg, k.ws, k.project)
		if err == nil && (len(records) != 1 || records[0].State != "published") {
			t.Fatal("published snapshot lost")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
