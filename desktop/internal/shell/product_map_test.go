package shell

import (
	"context"
	"strings"
	"testing"
	"time"
	"werkbord/desktop/internal/teamlink"
	"werkbord/internal/integration"
)

type mapPersonal struct {
	*fakeGrants
	snapshot integration.ProductMapSnapshot
}

func (p *mapPersonal) ProductMapSnapshot(context.Context, string) (integration.ProductMapSnapshot, error) {
	return p.snapshot, nil
}

type mapTeam struct {
	*fakeTeam
	repository string
	sent       []integration.ProductMapSnapshot
	ids        []string
}

func (p *mapTeam) ProductMapTarget(context.Context, string, string) (teamlink.ProductMapTarget, error) {
	return teamlink.ProductMapTarget{Repository: p.repository, CanPropose: true}, nil
}
func (p *mapTeam) ProposeProductMap(_ context.Context, w, project, id string, expected int64, snapshot integration.ProductMapSnapshot) (string, error) {
	p.sent = append(p.sent, snapshot)
	p.ids = append(p.ids, id)
	return id, nil
}
func nativeMap() integration.ProductMapSnapshot {
	return integration.ProductMapSnapshot{Schema: integration.ProductMapSchema, ID: "map_test", Version: 1, Repository: "github.com/acme/shop", Branch: "main", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), SourceDigest: strings.Repeat("c", 64), Rules: 1, Provider: "codex", CreatedAt: time.Now(), FilesRead: 1, FilesAvailable: 1, Content: integration.ProductMapContent{Features: []integration.ProductFeature{{ID: "checkout", Name: "Checkout", Evidence: []integration.ProductEvidence{{Path: "main.go", Start: 1, End: 2}}}}}}
}
func TestMapSharingHasExactScopeReviewAndIdempotentDelivery(t *testing.T) {
	rig := newRig(t, "Cancel", "Share map")
	personal := &mapPersonal{fakeGrants: rig.gr, snapshot: nativeMap()}
	team := &mapTeam{fakeTeam: rig.tm, repository: "github.com/acme/shop"}
	rig.o.Grants = personal
	rig.o.Team = team
	if _, err := rig.PreviewProductMapShare("prj_1", "team:missing", "tpj_1"); err == nil {
		t.Fatal("unregistered Team accepted")
	}
	team.repository = "github.com/other/repo"
	if _, err := rig.PreviewProductMapShare("prj_1", "team:main", "tpj_1"); err == nil {
		t.Fatal("wrong repository accepted")
	}
	team.repository = "github.com/acme/shop"
	preview, err := rig.PreviewProductMapShare("prj_1", "team:main", "tpj_1")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := rig.ConfirmProductMapShare(preview.Token); err != nil || result != "" || len(team.sent) != 0 {
		t.Fatal("cancelled review sent data", err)
	}
	personal.snapshot.Model = "changed after preview"
	result, err := rig.ConfirmProductMapShare(preview.Token)
	if err != nil || result == "" || len(team.sent) != 1 || team.sent[0].Model != "" {
		t.Fatal("preview did not freeze the shared snapshot", err)
	}
	if _, err = rig.ConfirmProductMapShare(preview.Token); err != nil || len(team.sent) != 1 {
		t.Fatal("duplicate delivery", err)
	}
	rig.mapShares.pending[preview.Token].At = time.Now().Add(-11 * time.Minute)
	if _, err = rig.ConfirmProductMapShare(preview.Token); err == nil {
		t.Fatal("expired grant accepted")
	}
}
