package service

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"errors"
	"fmt"
	"testing"
	"time"
)

func memoryProposal(a Actor, p string) integration.MemoryProposal {
	return integration.MemoryProposal{Schema: integration.MemorySchema, Scope: integration.SharedMemoryScope{WorkspaceID: a.Workspace.ID, Kind: "project", ProjectID: p}, Content: integration.SharedMemoryContent{Kind: "observation", Summary: "Use atomic transactions", Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "human", ID: "review_one"}}, ClientID: "client_one"}
}

func TestMemoryPagesHaveBoundedReplacementCursors(t *testing.T) {
	tm := newTeam(t)
	scope := integration.SharedMemoryScope{WorkspaceID: tm.owner.Workspace.ID, Kind: "workspace"}
	for i := 0; i < integration.MaxMemoryBatch+1; i++ {
		in := integration.MemoryProposal{
			Schema: integration.MemorySchema, Scope: scope,
			Content:  integration.SharedMemoryContent{Kind: "observation", Summary: "Bounded record", Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "human", ID: "review_one"}},
			ClientID: fmt.Sprintf("batch_%02d", i),
		}
		if _, err := tm.svc.ProposeMemory(bg, tm.owner, in); err != nil {
			t.Fatal(err)
		}
	}
	page, err := tm.svc.ReadMemories(bg, tm.owner, scope, "", false)
	if err != nil || len(page.Records) != integration.MaxMemoryBatch || !page.More || page.Next != page.Records[len(page.Records)-1].ID {
		t.Fatalf("first page has invalid cursor: len=%d more=%v next=%q err=%v", len(page.Records), page.More, page.Next, err)
	}
	last, err := tm.svc.ReadMemories(bg, tm.owner, scope, page.Next, false)
	if err != nil || len(last.Records) != 1 || last.More || last.Next != "" || last.Records[0].ID == page.Next {
		t.Fatalf("last page has invalid cursor: %+v %v", last, err)
	}
}
func memoryCommand(r integration.SharedMemoryRecord, action string) integration.MemoryCommand {
	return integration.MemoryCommand{Schema: integration.MemorySchema, Scope: r.Scope, Version: r.Version, Action: action}
}
func TestMemoryPermissionsApprovalAndSupersession(t *testing.T) {
	tm := newTeam(t)
	self := memoryProposal(tm.di, tm.pid())
	self.ClientID = "self_approval"
	selfRecord, err := tm.svc.ProposeMemory(bg, tm.di, self)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.di, selfRecord.ID, memoryCommand(selfRecord, "approve"))
	wantErr(t, err, domain.ErrForbidden)
	mcp := tm.di
	mcp.MCPGrantID = "mcp_grant"
	_, err = tm.svc.ProposeMemory(bg, mcp, self)
	wantErr(t, err, domain.ErrForbidden)
	_, err = tm.svc.ReadMemories(bg, mcp, self.Scope, "", false)
	wantErr(t, err, domain.ErrForbidden)
	_, err = tm.svc.ChangeMemory(bg, mcp, selfRecord.ID, memoryCommand(selfRecord, "delete"))
	wantErr(t, err, domain.ErrForbidden)

	in := memoryProposal(tm.bo, tm.pid())
	r, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	if err != nil {
		t.Fatal(err)
	}
	if r.AuthorID != tm.bo.Member.ID || r.Approval != nil || r.State != "suggested" {
		t.Fatal(r)
	}
	again, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	if err != nil || again.ID != r.ID {
		t.Fatal("non-idempotent proposal", again, err)
	}
	changed := in
	changed.Content.Summary = "Different"
	_, err = tm.svc.ProposeMemory(bg, tm.bo, changed)
	wantErr(t, err, domain.ErrConflict)
	for _, action := range []string{"approve", "publish", "edit", "delete"} {
		cmd := memoryCommand(r, action)
		if action == "edit" {
			cmd.Content = &in.Content
		}
		_, err := tm.svc.ChangeMemory(bg, tm.bo, r.ID, cmd)
		wantErr(t, err, domain.ErrForbidden)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, memoryCommand(r, "publish"))
	wantErr(t, err, domain.ErrConflict)
	r, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, memoryCommand(r, "approve"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Approval.MemberID != tm.di.Member.ID || r.Approval.Version != r.Version {
		t.Fatal(r)
	}
	approvedVersion := r.Version
	r, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, memoryCommand(r, "publish"))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "published" {
		t.Fatal(r)
	}
	// An edit of published content withdraws it and its approval atomically.
	edit := memoryCommand(r, "edit")
	edit.Content = &in.Content
	r, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, edit)
	if err != nil || r.Approval != nil || r.State != "suggested" {
		t.Fatal(r, err)
	}
	page, err := tm.svc.ReadMemories(bg, tm.bo, in.Scope, "", true)
	if err != nil || len(page.Records) != 0 {
		t.Fatal(page, err)
	}
	stale := memoryCommand(r, "approve")
	stale.Version = approvedVersion
	_, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, stale)
	wantErr(t, err, domain.ErrConflict)
	// An inferred preference stays a preference after human approval/publication.
	edit = memoryCommand(r, "edit")
	pref := in.Content
	pref.Kind = "preference"
	edit.Content = &pref
	r, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"approve", "publish"} {
		r, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, memoryCommand(r, action))
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.Content.Kind != "preference" {
		t.Fatal("approval promoted preference")
	}
	// Suggested policies need the stronger project owner authority.
	policy := in
	policy.ClientID = "policy_two"
	policy.Content.Kind = "policy"
	policy.Supersedes = r.ID
	p, err := tm.svc.ProposeMemory(bg, tm.bo, policy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.di, p.ID, memoryCommand(p, "approve"))
	wantErr(t, err, domain.ErrForbidden)
	for _, action := range []string{"approve", "publish"} {
		p, err = tm.svc.ChangeMemory(bg, tm.owner, p.ID, memoryCommand(p, action))
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err = tm.svc.ReadMemories(bg, tm.bo, in.Scope, "", true)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != p.ID {
		t.Fatal(page, err)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, memoryCommand(r, "delete"))
	wantErr(t, err, domain.ErrConflict)
	p, err = tm.svc.ChangeMemory(bg, tm.owner, p.ID, memoryCommand(p, "delete"))
	if err != nil || p.Content.Summary != "" {
		t.Fatal(p, err)
	}
	page, err = tm.svc.ReadMemories(bg, tm.bo, in.Scope, "", true)
	if err != nil || len(page.Records) != 0 {
		t.Fatal(page, err)
	}
}

func TestRestrictedMemoriesRequireTheirOwnReadPermission(t *testing.T) {
	tm := newTeam(t)
	in := memoryProposal(tm.bo, tm.pid())
	in.ClientID = "restricted_memory"
	in.Content.Classification = "restricted"
	restricted, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := tm.svc.ReadMemories(bg, tm.bo, in.Scope, "", false)
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("contributor read restricted memory: %+v %v", page, err)
	}
	for _, reviewer := range []Actor{tm.di, tm.owner} {
		page, err = tm.svc.ReadMemories(bg, reviewer, in.Scope, "", false)
		if err != nil || len(page.Records) != 1 || page.Records[0].ID != restricted.ID {
			t.Fatalf("authorized restricted read: %+v %v", page, err)
		}
	}
}

func TestMemoryIsolationAndRevocationEveryOperation(t *testing.T) {
	tm := newTeam(t)
	other := tm.project2()
	eve, _ := tm.member(tm.owner, "Eve")
	in := memoryProposal(tm.bo, tm.pid())
	r, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	if err != nil {
		t.Fatal(err)
	}
	// Query and every command preserve the project's not-found behavior.
	for _, a := range []Actor{eve} {
		_, err := tm.svc.ReadMemories(bg, a, in.Scope, "", false)
		wantErr(t, err, domain.ErrNotFound)
		_, err = tm.svc.ProposeMemory(bg, a, in)
		wantErr(t, err, domain.ErrNotFound)
		for _, action := range []string{"approve", "publish", "delete", "edit"} {
			cmd := memoryCommand(r, action)
			if action == "edit" {
				cmd.Content = &in.Content
			}
			_, err = tm.svc.ChangeMemory(bg, a, r.ID, cmd)
			wantErr(t, err, domain.ErrNotFound)
		}
	}
	wrong := in.Scope
	wrong.ProjectID = other.ID
	page, err := tm.svc.ReadMemories(bg, tm.owner, wrong, "", false)
	if err != nil || len(page.Records) != 0 {
		t.Fatal(page, err)
	}
	cmd := memoryCommand(r, "delete")
	cmd.Scope = wrong
	_, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, cmd)
	wantErr(t, err, domain.ErrNotFound)
	// Workspace memory is an exact partition; ordinary members can propose/read but cannot approve it.
	workspace := in
	workspace.ClientID = "workspace_one"
	workspace.Scope = integration.SharedMemoryScope{WorkspaceID: tm.owner.Workspace.ID, Kind: "workspace"}
	wr, err := tm.svc.ProposeMemory(bg, tm.bo, workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.bo, wr.ID, memoryCommand(wr, "approve"))
	wantErr(t, err, domain.ErrForbidden)
	foreign, _ := tm.workspace("Other", "Else")
	_, err = tm.svc.ReadMemories(bg, foreign, in.Scope, "", false)
	wantErr(t, err, domain.ErrNotFound)
	// Device revocation and membership changes invalidate already authenticated actors, even with a bare context.
	identity := laptop(t, "memory")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.ReadMemories(bg, actor, in.Scope, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.RevokeDevice(bg, tm.bo, device.ID); err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ReadMemories(bg, actor, in.Scope, "", false)
	wantErr(t, err, domain.ErrUnauthenticated)
	_, err = tm.svc.ProposeMemory(bg, actor, in)
	wantErr(t, err, domain.ErrUnauthenticated)
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ReadMemories(bg, tm.bo, in.Scope, "", false)
	wantErr(t, err, domain.ErrNotFound)
}
func ptrString(s string) *string { return &s }

func TestMemoryRepositoryAndCrossRepositoryFences(t *testing.T) {
	tm := newTeam(t)
	p2 := tm.project2()
	for _, p := range []domain.Project{tm.project, p2} {
		_, err := tm.svc.UpdateProject(bg, tm.owner, p.ID, ProjectPatch{Name: &p.Name, Repository: ptrString("https://github.com/acme/" + p.ID)})
		if err != nil {
			t.Fatal(err)
		}
	}
	in := memoryProposal(tm.bo, tm.pid())
	in.Scope.Kind = "cross_repository"
	in.Scope.ProjectID = ""
	in.Scope.Targets = []integration.MemoryTarget{{ProjectID: tm.pid(), Repository: "github.com/acme/" + tm.pid()}, {ProjectID: p2.ID, Repository: "github.com/acme/" + p2.ID}}
	_, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	wantErr(t, err, domain.ErrNotFound)
	if err = tm.svc.AddProjectMember(bg, tm.owner, p2.ID, tm.bo.Member.ID, domain.ProjectContributor); err != nil {
		t.Fatal(err)
	}
	r, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, p2.ID, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ReadMemories(bg, tm.bo, in.Scope, "", false)
	wantErr(t, err, domain.ErrNotFound)
	// An address change suspends old repository knowledge, including owner reads.
	_, err = tm.svc.UpdateProject(bg, tm.owner, p2.ID, ProjectPatch{Name: &p2.Name, Repository: ptrString("https://github.com/acme/changed")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, memoryCommand(r, "approve"))
	wantErr(t, err, domain.ErrConflict)
	_, err = tm.svc.ReadMemories(bg, tm.owner, in.Scope, "", false)
	wantErr(t, err, domain.ErrConflict)
}
func TestMemoryRequiresQuorumAndRecovers(t *testing.T) {
	if sharedCluster == nil || len(sharedCluster.Nodes()) != 3 {
		t.Skip("requires three-node replicated service harness")
	}
	tm := newTeam(t)
	in := memoryProposal(tm.bo, tm.pid())
	r, err := tm.svc.ProposeMemory(bg, tm.bo, in)
	if err != nil {
		t.Fatal(err)
	}
	sharedCluster.Partition([]int{0}, []int{1}, []int{2})
	defer sharedCluster.Heal()
	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	_, err = tm.svc.ReadMemories(ctx, tm.bo, in.Scope, "", false)
	cancel()
	if err == nil {
		t.Fatal("isolated host returned protected memory")
	}
	ctx, cancel = context.WithTimeout(bg, 3*time.Second)
	_, err = tm.svc.ChangeMemory(ctx, tm.owner, r.ID, memoryCommand(r, "approve"))
	cancel()
	if err == nil {
		t.Fatal("isolated host approved memory")
	}
	sharedCluster.Heal()
	sharedCluster.WaitLeader(30 * time.Second)
	ctx, cancel = context.WithTimeout(bg, 30*time.Second)
	defer cancel()
	approved, err := tm.svc.ChangeMemory(ctx, tm.owner, r.ID, memoryCommand(r, "approve"))
	if err != nil || approved.Version != 2 {
		t.Fatal(approved, err)
	}
	if _, err = tm.svc.ReadMemories(ctx, tm.bo, in.Scope, "", false); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
