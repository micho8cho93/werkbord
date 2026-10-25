package shell

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/integration"
	"devboard/internal/workspace"
)

type movePersonal struct {
	*fakeGrants
	snapshot     integration.ProjectTransfer
	archives     int
	archiveError error
}

func (p *movePersonal) ProjectTransfer(_ context.Context, op, id string, in *integration.ProjectTransfer) (integration.ProjectTransfer, string, error) {
	if op == "export" {
		return p.snapshot, "", nil
	}
	if op == "archive" {
		p.archives++
		return integration.ProjectTransfer{}, "", p.archiveError
	}
	return integration.ProjectTransfer{}, "prj_destination", nil
}

type moveTeam struct {
	*fakeTeam
	imports int
	fail    bool
}

func (p *moveTeam) ProjectTransfer(_ context.Context, where, op, id string, in *integration.ProjectTransfer) (integration.ProjectTransfer, string, error) {
	if op == "import" {
		p.imports++
		if p.fail {
			return integration.ProjectTransfer{}, "", errors.New("lost response")
		}
		return integration.ProjectTransfer{}, "tpj_destination", nil
	}
	return integration.ProjectTransfer{}, "", nil
}
func TestProjectMoveRequiresNativeApprovalAndFrozenReview(t *testing.T) {
	r := newRig(t, "Cancel", "Move project", "Move project")
	personal := &movePersonal{fakeGrants: r.gr, snapshot: integration.ProjectTransfer{Name: "Private", Tickets: []integration.TransferTicket{{Title: "Reviewed"}}}}
	team := &moveTeam{fakeTeam: r.tm}
	r.o.Grants = personal
	r.o.Team = team
	for _, pair := range [][2]string{{"personal", "personal"}, {"team:main", "team:ws_2"}, {"personal", "team:unknown"}} {
		if _, err := r.PreviewProjectMove(pair[0], pair[1], "prj_source"); err == nil {
			t.Fatal("invalid destination accepted")
		}
	}
	preview, err := r.PreviewProjectMove("personal", "team:main", "prj_source")
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.ConfirmProjectMove(preview.Token)
	if err != nil || !result.Cancelled || team.imports != 0 || personal.archives != 0 {
		t.Fatalf("cancel wrote: %+v %v", result, err)
	}
	personal.snapshot.Name = "Changed"
	if _, err := r.ConfirmProjectMove(preview.Token); err == nil || team.imports != 0 {
		t.Fatal("stale review wrote")
	}
	personal.snapshot.Name = "Private"
	result, err = r.ConfirmProjectMove(preview.Token)
	if err != nil || !result.Archived || team.imports != 1 || personal.archives != 1 {
		t.Fatalf("move: %+v %v", result, err)
	}
	if _, err := r.ConfirmProjectMove(preview.Token); err != nil || team.imports != 1 {
		t.Fatal("confirmation replay duplicated")
	}
	for _, method := range []string{"PreviewProjectMove", "ConfirmProjectMove"} {
		if _, err := r.Relay("team:main", method, nil); err == nil {
			t.Fatal("Team frame reached move")
		}
	}
}
func TestProjectMoveUnknownImportNeverArchivesAndRetryUsesReview(t *testing.T) {
	r := newRig(t, "Move project", "Move project")
	personal := &movePersonal{fakeGrants: r.gr, snapshot: integration.ProjectTransfer{Name: "Private", Tickets: []integration.TransferTicket{}}}
	team := &moveTeam{fakeTeam: r.tm, fail: true}
	r.o.Grants = personal
	r.o.Team = team
	preview, err := r.PreviewProjectMove("personal", "team:main", "prj_source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ConfirmProjectMove(preview.Token); err == nil || personal.archives != 0 {
		t.Fatal("unknown import archived source")
	}
	team.fail = false
	personal.archiveError = errors.New("changed source")
	result, err := r.ConfirmProjectMove(preview.Token)
	if err != nil || result.Archived || result.ID == "" || result.Warning == "" {
		t.Fatalf("partial result hidden: %+v %v", result, err)
	}
	r.reg.items[1].State = workspace.StateUnavailable
	if _, err := r.PreviewProjectMove("personal", "team:main", "prj_source"); err == nil {
		t.Fatal("unavailable destination accepted")
	}
}
