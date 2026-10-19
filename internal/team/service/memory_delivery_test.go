package service

import (
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"testing"
)

func TestMemoryDeliveryRechecksAuthorityOrderAndImmutableIdentity(t *testing.T) {
	tm := newTeam(t)
	identity := laptop(t, "memory-delivery")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	repo := "https://github.com/acme/memory"
	if _, err = tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Repository: &repo}); err != nil {
		t.Fatal(err)
	}
	proposal := memoryProposal(actor, tm.pid())
	proposal.Scope.Kind = "repository"
	proposal.Scope.Repository = "github.com/acme/memory"
	proposal.ClientID = "event_one"
	event := integration.MemoryEvent{Schema: integration.MemorySchema, ID: "event_one", Sequence: 1, SourceID: proposal.Content.Source.ID, Revision: 2, Action: "propose", Proposal: proposal}
	ack, err := tm.svc.DeliverMemory(bg, actor, event)
	if err != nil {
		t.Fatal(err)
	}
	again, err := tm.svc.DeliverMemory(bg, actor, event)
	if err != nil || again.Record.ID != ack.Record.ID {
		t.Fatalf("duplicate event: %+v %v", again, err)
	}
	altered := event
	altered.Proposal.Content.Summary = "Changed under the same event identity"
	_, err = tm.svc.DeliverMemory(bg, actor, altered)
	wantErr(t, err, domain.ErrConflict)
	gap := event
	gap.ID = "event_gap"
	gap.Proposal.ClientID = gap.ID
	gap.Sequence = 3
	_, err = tm.svc.DeliverMemory(bg, actor, gap)
	wantErr(t, err, domain.ErrConflict)
	duplicateRevision := gap
	duplicateRevision.Sequence = 2
	_, err = tm.svc.DeliverMemory(bg, actor, duplicateRevision)
	wantErr(t, err, domain.ErrConflict)
	older := gap
	older.Sequence = 2
	older.Revision = 1
	_, err = tm.svc.DeliverMemory(bg, actor, older)
	wantErr(t, err, domain.ErrConflict)
	// An author can withdraw its own projection, without curator deletion powers.
	revoke := event
	revoke.ID = "event_revoke"
	revoke.Proposal.ClientID = revoke.ID
	revoke.Sequence = 2
	revoke.Action = "revoke"
	revoke.Previous = event.ID
	withdrawn, err := tm.svc.DeliverMemory(bg, actor, revoke)
	if err != nil || withdrawn.Record.State != "deleted" {
		t.Fatalf("author withdrawal: %+v %v", withdrawn, err)
	}
	again, err = tm.svc.DeliverMemory(bg, actor, revoke)
	if err != nil || again.Record.ID != ack.Record.ID {
		t.Fatalf("withdrawal replay: %+v %v", again, err)
	}
	// A changed repository association invalidates replay as well as new writes.
	changed := "https://github.com/acme/changed"
	if _, err = tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Repository: &changed}); err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.DeliverMemory(bg, actor, event)
	wantErr(t, err, domain.ErrConflict)
	if _, err = tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Repository: &repo}); err != nil {
		t.Fatal(err)
	}
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), actor.Member.ID); err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.DeliverMemory(bg, actor, event)
	wantErr(t, err, domain.ErrNotFound)
}
func TestMemoryCancellationWithdrawsOriginalWithLostReply(t *testing.T) {
	tm := newTeam(t)
	identity := laptop(t, "memory-cancel")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	repo := "https://github.com/acme/memory"
	if _, err = tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Repository: &repo}); err != nil {
		t.Fatal(err)
	}
	p := memoryProposal(actor, tm.pid())
	p.Scope.Kind = "repository"
	p.Scope.Repository = "github.com/acme/memory"
	p.ClientID = "uncertain_event"
	event := integration.MemoryEvent{Schema: p.Schema, ID: p.ClientID, Sequence: 1, Revision: 1, SourceID: p.Content.Source.ID, Action: "propose", Proposal: p}
	original, err := tm.svc.DeliverMemory(bg, actor, event)
	if err != nil {
		t.Fatal(err)
	}
	cancel := event
	cancel.Action = "cancel"
	cancel.Proposal.Content.Summary = "Publication withdrawn before delivery"
	ack, err := tm.svc.DeliverMemory(bg, actor, cancel)
	if err != nil || ack.Record.ID != original.Record.ID || ack.Record.State != "deleted" {
		t.Fatalf("uncertain cancellation: %+v %v", ack, err)
	}
	ack, err = tm.svc.DeliverMemory(bg, actor, cancel)
	if err != nil || ack.Record.State != "deleted" {
		t.Fatalf("cancel replay: %+v %v", ack, err)
	}
	_, err = tm.svc.DeliverMemory(bg, actor, event)
	wantErr(t, err, domain.ErrConflict)
}
