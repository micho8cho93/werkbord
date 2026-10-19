package service

import (
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
	"strings"
	"sync"
	"testing"
	"time"
)

func memoryGitProject(t *testing.T) (*team, *time.Time) {
	t.Helper()
	tm := newTeam(t)
	repo := "https://github.com/acme/shop"
	if _, err := tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Repository: &repo}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	tm.svc.SetClock(func() time.Time { return now })
	return tm, &now
}
func branchMemory(t *testing.T, tm *team, a Actor, client, branch, summary, revision string) integration.SharedMemoryRecord {
	t.Helper()
	p := memoryProposal(a, tm.pid())
	p.ClientID = client
	p.Content.Summary = summary
	p.Content.Source = integration.MemorySource{Kind: "git", ID: "implementation", Revision: revision}
	p.Content.Context = &integration.MemoryContext{Claim: "database", Branch: branch}
	r, err := tm.svc.ProposeMemory(bg, a, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func memoryEvidence(tm *team, now time.Time, branch, revision string) domain.MemoryEvidence {
	return domain.MemoryEvidence{Scope: integration.SharedMemoryScope{WorkspaceID: tm.owner.Workspace.ID, Kind: "project", ProjectID: tm.pid()}, Repository: "github.com/acme/shop", Branch: branch, Revision: revision, Sequence: 1, Available: true, ObservedAt: now}
}
func readFreshness(t *testing.T, tm *team, a Actor, id string) integration.MemoryFreshness {
	t.Helper()
	p, err := tm.svc.SearchMemories(bg, a, tm.pid(), "", "", id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Records[0].Freshness == nil {
		t.Fatal("missing freshness")
	}
	return *p.Records[0].Freshness
}
func publishMemory(t *testing.T, tm *team, r integration.SharedMemoryRecord) integration.SharedMemoryRecord {
	t.Helper()
	var err error
	for _, action := range []string{"approve", "publish"} {
		r, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, memoryCommand(r, action))
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}
func TestMemoryConcurrentBranchesContradictionsAndExplicitResolution(t *testing.T) {
	tm, now := memoryGitProject(t)
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var ra, rb integration.SharedMemoryRecord
	var wg sync.WaitGroup
	// Each member publishes an independent claim; no common execution workspace.
	wg.Add(2)
	go func() {
		defer wg.Done()
		p := memoryProposal(tm.bo, tm.pid())
		p.ClientID = "concurrent_a"
		p.Content.Source = integration.MemorySource{Kind: "git", ID: "implementation", Revision: shaA}
		p.Content.Context = &integration.MemoryContext{Claim: "database", Branch: "feature-a"}
		p.Content.Summary = "Use SQLite"
		var err error
		ra, err = tm.svc.ProposeMemory(bg, tm.bo, p)
		if err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		p := memoryProposal(tm.cy, tm.pid())
		p.ClientID = "concurrent_b"
		p.Content.Source = integration.MemorySource{Kind: "git", ID: "implementation", Revision: shaB}
		p.Content.Context = &integration.MemoryContext{Claim: "database", Branch: "feature-b"}
		p.Content.Summary = "Use Postgres"
		var err error
		rb, err = tm.svc.ProposeMemory(bg, tm.cy, p)
		if err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	if t.Failed() {
		return
	}
	for _, e := range []struct {
		a      Actor
		b, sha string
	}{{tm.bo, "feature-a", shaA}, {tm.cy, "feature-b", shaB}} {
		if err := tm.svc.ReportMemoryEvidence(bg, e.a, memoryEvidence(tm, *now, e.b, e.sha)); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []integration.SharedMemoryRecord{ra, rb} {
		if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "source_matches" || f.Conflict {
			t.Fatalf("branch context lost: %+v", f)
		}
	}
	contradiction := branchMemory(t, tm, tm.cy, "contradiction", "feature-a", "Use Postgres", shaA)
	if f := readFreshness(t, tm, tm.bo, ra.ID); f.Status != "conflict" || !f.Conflict {
		t.Fatal(f)
	}
	if f := readFreshness(t, tm, tm.cy, contradiction.ID); !f.Conflict {
		t.Fatal("lost second provenance", f)
	}
	ra = publishMemory(t, tm, ra)
	resolution := domain.MemoryResolution{Version: ra.Version, Replaced: []domain.MemoryResolutionVersion{{ID: contradiction.ID, Version: contradiction.Version}}}
	if _, err := tm.svc.ResolveMemory(bg, tm.bo, tm.pid(), ra.ID, resolution); err == nil {
		t.Fatal("contributor resolved conflict")
	}
	for i := 0; i < 2; i++ {
		if _, err := tm.svc.ResolveMemory(bg, tm.owner, tm.pid(), ra.ID, resolution); err != nil {
			t.Fatal(err)
		}
	}
	if f := readFreshness(t, tm, tm.bo, ra.ID); f.Conflict {
		t.Fatal("resolution did not remove conflict", f)
	}
	history, err := tm.svc.MemoryHistory(bg, tm.cy, tm.pid(), contradiction.ID)
	if err != nil || history.Revisions[0].SupersededBy != ra.ID {
		t.Fatalf("missing replacement audit: %+v %v", history, err)
	}
}
func TestMemoryEvidenceMissedEventsOfflineAndReplay(t *testing.T) {
	tm, now := memoryGitProject(t)
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	r := branchMemory(t, tm, tm.bo, "missed", "main", "Database uses SQLite", shaA)
	e := memoryEvidence(tm, *now, "main", shaA)
	for i := 0; i < 2; i++ {
		if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
			t.Fatal(err)
		}
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "source_matches" {
		t.Fatal(f)
	}
	*now = now.Add(time.Minute)
	e.Sequence++
	e.ObservedAt = *now
	e.Revision = shaB
	if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
		t.Fatal(err)
	}
	// No worker tick: invalidation must be visible immediately.
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "stale" || f.SourceRevision != shaB {
		t.Fatal(f)
	}
	replay := e
	replay.Revision = shaA
	wantErr(t, tm.svc.ReportMemoryEvidence(bg, tm.bo, replay), domain.ErrConflict)
	*now = now.Add(store.MemoryEvidenceTTL + time.Second)
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "uncertain" {
		t.Fatal("offline source stayed fresh", f)
	}
	e.Sequence++
	e.ObservedAt = *now
	e.Revision = shaA
	if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "source_matches" {
		t.Fatal(f)
	}
	e.Sequence++
	e.Available = false
	if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "uncertain" {
		t.Fatal(f)
	}
	if err := tm.svc.ReconcileMemory(bg); err != nil {
		t.Fatal(err)
	}
	h, err := tm.svc.MemoryHistory(bg, tm.bo, tm.pid(), r.ID)
	if err != nil || len(h.Freshness) != 1 {
		t.Fatalf("audit: %+v %v", h, err)
	}
	if err := tm.svc.ReconcileMemory(bg); err != nil {
		t.Fatal(err)
	}
	h, err = tm.svc.MemoryHistory(bg, tm.bo, tm.pid(), r.ID)
	if err != nil || len(h.Freshness) != 1 {
		t.Fatal("identical reconciliation duplicated audit", h, err)
	}
}
func TestMemoryPRMergeIsNotDeploymentAndMissedPRRecovered(t *testing.T) {
	tm, now := memoryGitProject(t)
	sha := strings.Repeat("a", 40)
	k := tm.working(tm.bo, "Implement storage")
	r := branchMemory(t, tm, tm.bo, "pr_memory", k.Branch, "PR implements SQLite", sha)
	edit := memoryCommand(r, "edit")
	content := r.Content
	context := *content.Context
	context.TaskID = k.ID
	context.PullRequest = 7
	content.Context = &context
	edit.Content = &content
	var err error
	r, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.PRState{domain.PROpen, domain.PRMerged} {
		*now = now.Add(time.Minute)
		if _, err = tm.svc.ReportGit(bg, tm.bo, tm.pid(), k.ID, GitReport{State: &BranchState{HeadSHA: sha}, PullRequest: &domain.PullRequest{Number: 7, URL: prURL, State: state, BaseBranch: "main"}}); err != nil {
			t.Fatal(err)
		}
		f := readFreshness(t, tm, tm.bo, r.ID)
		integrationState := "branch_only"
		if state == domain.PRMerged {
			integrationState = "entered_target"
		}
		if f.PRState != string(state) || f.TargetBranch != "main" || f.Status != "source_matches" || f.IntegrationState != integrationState {
			t.Fatalf("missed PR recovery: %+v", f)
		}
		page, err := tm.svc.SearchMemories(bg, tm.bo, tm.pid(), "", "", r.ID)
		if err != nil || page.Records[0].Content.Context.Branch != k.Branch {
			t.Fatal("merge promoted branch", page, err)
		}
	}
	// A later source snapshot can report a closure/superseding without inventing integration.
	e := memoryEvidence(tm, *now, k.Branch, sha)
	e.PullRequest = 7
	e.PRState = "closed"
	e.TargetBranch = "main"
	if err = tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "conflict" {
		t.Fatal(f)
	}
}
func TestMemoryRetrievalRechecksRevocationAndRestrictedHistory(t *testing.T) {
	tm, now := memoryGitProject(t)
	sha := strings.Repeat("a", 40)
	r := branchMemory(t, tm, tm.bo, "revocation", "main", "SQLite storage", sha)
	if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, memoryEvidence(tm, *now, "main", sha)); err != nil {
		t.Fatal(err)
	}
	if err := tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.SearchMemories(bg, tm.bo, tm.pid(), "SQLite", "", ""); err == nil {
		t.Fatal("revoked member searched")
	}
	if _, err := tm.svc.MemoryHistory(bg, tm.bo, tm.pid(), r.ID); err == nil {
		t.Fatal("revoked member read history")
	}
	if f := readFreshness(t, tm, tm.cy, r.ID); f.Status != "uncertain" {
		t.Fatal("revoked reporter corroborated", f)
	}
	edit := memoryCommand(r, "edit")
	c := r.Content
	c.Classification = "restricted"
	edit.Content = &c
	var err error
	r, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.SearchMemories(bg, tm.cy, tm.pid(), "", "", r.ID); err == nil {
		t.Fatal("restricted read")
	}
	if _, err = tm.svc.MemoryHistory(bg, tm.cy, tm.pid(), r.ID); err == nil {
		t.Fatal("restricted history")
	}
	edit = memoryCommand(r, "edit")
	c.Classification = "team"
	edit.Content = &c
	r, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	h, err := tm.svc.MemoryHistory(bg, tm.cy, tm.pid(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range h.Revisions {
		if v.Content.Classification == "restricted" {
			t.Fatal("historical classification leaked")
		}
	}
}
func TestMemoryPolicyCannotBeReplacedByPreference(t *testing.T) {
	tm, _ := memoryGitProject(t)
	p := memoryProposal(tm.bo, tm.pid())
	p.Content.Kind = "policy"
	r, err := tm.svc.ProposeMemory(bg, tm.bo, p)
	if err != nil {
		t.Fatal(err)
	}
	r = publishMemory(t, tm, r)
	p.ClientID = "preference_replacement"
	p.Content.Kind = "preference"
	p.Supersedes = r.ID
	_, err = tm.svc.ProposeMemory(bg, tm.owner, p)
	wantErr(t, err, domain.ErrForbidden)
	edit := memoryCommand(r, "edit")
	edit.Content = &p.Content
	_, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, edit)
	wantErr(t, err, domain.ErrForbidden)
}

func TestMemoryIdenticalUpdatesKeepOneRecordAndDurableAlias(t *testing.T) {
	tm, _ := memoryGitProject(t)
	sha := strings.Repeat("a", 40)
	p := memoryProposal(tm.bo, tm.pid())
	p.Content.Source = integration.MemorySource{Kind: "git", ID: "source", Revision: sha}
	p.Content.Context = &integration.MemoryContext{Claim: "storage", Branch: "main"}
	r, err := tm.svc.ProposeMemory(bg, tm.bo, p)
	if err != nil {
		t.Fatal(err)
	}
	p.ClientID = "identical_new_key"
	again, err := tm.svc.ProposeMemory(bg, tm.bo, p)
	if err != nil || again.ID != r.ID || again.Version != r.Version {
		t.Fatalf("duplicate update: %+v %v", again, err)
	}
	if _, err = tm.svc.ProposeMemory(bg, tm.bo, p); err != nil {
		t.Fatal(err)
	}
	p.Content.Summary = "Contradiction under reused key"
	_, err = tm.svc.ProposeMemory(bg, tm.bo, p)
	wantErr(t, err, domain.ErrConflict)
	page, err := tm.svc.SearchMemories(bg, tm.bo, tm.pid(), "", "", "")
	if err != nil || len(page.Records) != 1 {
		t.Fatal(page, err)
	}
	// A different member keeps provenance but is linked as a duplicate, never a conflict.
	p.Content = r.Content
	p.ClientID = "other_member"
	other, err := tm.svc.ProposeMemory(bg, tm.cy, p)
	if err != nil {
		t.Fatal(err)
	}
	f1, f2 := readFreshness(t, tm, tm.bo, r.ID), readFreshness(t, tm, tm.bo, other.ID)
	if f1.Conflict || f2.Conflict || f1.DuplicateOf == "" && f2.DuplicateOf == "" {
		t.Fatal("identical member reports lost duplicate relationship", f1, f2)
	}
}
func TestMemoryNewBranchSnapshotRecoversWithoutDedicatedEvent(t *testing.T) {
	tm, now := memoryGitProject(t)
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	r := branchMemory(t, tm, tm.bo, "snapshot", "main", "SQLite storage", shaA)
	e := memoryEvidence(tm, *now, "main", shaA)
	e.AuthoritativeRevision = shaA
	if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.AuthoritativeRevision != shaA {
		t.Fatal(f)
	}
	*now = now.Add(time.Minute)
	if err := tm.svc.ReportBranches(bg, tm.bo, tm.pid(), []BranchInput{{Name: "main", BranchState: BranchState{HeadSHA: shaB}}}, nil); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "stale" || f.SourceRevision != shaB || f.AuthoritativeRevision != "" {
		t.Fatal("missed event left old evidence authoritative", f)
	}
}
func TestMemoryCompetingSourceRevisionsAndRevokedDevice(t *testing.T) {
	tm, now := memoryGitProject(t)
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	r := branchMemory(t, tm, tm.bo, "report_conflict", "main", "SQLite storage", shaA)
	for _, v := range []struct {
		a   Actor
		sha string
	}{{tm.bo, shaA}, {tm.cy, shaB}} {
		if err := tm.svc.ReportMemoryEvidence(bg, v.a, memoryEvidence(tm, *now, "main", v.sha)); err != nil {
			t.Fatal(err)
		}
	}
	f := readFreshness(t, tm, tm.bo, r.ID)
	if f.Status != "conflict" || f.SourceRevision != "" {
		t.Fatal("invented authoritative revision", f)
	}
	identity := laptop(t, "freshness")
	device := tm.mustRegister(tm.di, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.di.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.SearchMemories(bg, actor, tm.pid(), "", "", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.RevokeDevice(bg, tm.owner, device.ID); err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.SearchMemories(bg, actor, tm.pid(), "", "", r.ID)
	wantErr(t, err, domain.ErrUnauthenticated)
	_, err = tm.svc.MemoryHistory(bg, actor, tm.pid(), r.ID)
	wantErr(t, err, domain.ErrUnauthenticated)
}

func TestMemoryGitWithoutNewHeadInvalidatesBeforeWorker(t *testing.T) {
	tm, now := memoryGitProject(t)
	sha := strings.Repeat("a", 40)
	k := tm.working(tm.bo, "Storage")
	r := branchMemory(t, tm, tm.bo, "missing_head", k.Branch, "SQLite storage", sha)
	if err := tm.svc.ReportBranches(bg, tm.bo, tm.pid(), []BranchInput{{Name: k.Branch, BranchState: BranchState{HeadSHA: sha}}}, nil); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "source_matches" {
		t.Fatal(f)
	}
	*now = now.Add(time.Minute)
	commits := []domain.Commit{{SHA: strings.Repeat("b", 40)}}
	if _, err := tm.svc.ReportGit(bg, tm.bo, tm.pid(), k.ID, GitReport{Commits: &commits}); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "stale" {
		t.Fatal("unknown head was assumed unchanged", f)
	}
	legacy, err := tm.svc.ReadMemories(bg, tm.bo, r.Scope, "", false)
	if err != nil || legacy.Records[0].Freshness.Status != "stale" {
		t.Fatal("legacy query bypassed freshness", legacy, err)
	}
	e := memoryEvidence(tm, *now, k.Branch, sha)
	if err := tm.svc.ReportMemoryEvidence(bg, tm.bo, e); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.Status != "source_matches" {
		t.Fatal("resynchronized source did not reconcile", f)
	}
}

func TestMemoryProjectSearchRechecksEveryRepositoryRelationship(t *testing.T) {
	tm, _ := memoryGitProject(t)
	other := tm.project2()
	repo := "https://github.com/acme/other"
	if _, err := tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Repository: &repo}); err != nil {
		t.Fatal(err)
	}
	if err := tm.svc.AddProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID, domain.ProjectContributor); err != nil {
		t.Fatal(err)
	}
	p := memoryProposal(tm.bo, tm.pid())
	p.Scope = integration.SharedMemoryScope{WorkspaceID: tm.bo.Workspace.ID, Kind: "cross_repository", Targets: []integration.MemoryTarget{{ProjectID: tm.pid(), Repository: "github.com/acme/shop"}, {ProjectID: other.ID, Repository: "github.com/acme/other"}}}
	r, err := tm.svc.ProposeMemory(bg, tm.bo, p)
	if err != nil {
		t.Fatal(err)
	}
	page, err := tm.svc.SearchMemories(bg, tm.bo, tm.pid(), "atomic", "", "")
	if err != nil || len(page.Records) != 1 {
		t.Fatal(page, err)
	}
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	page, err = tm.svc.SearchMemories(bg, tm.bo, tm.pid(), "atomic", "", "")
	if err != nil || len(page.Records) != 0 {
		t.Fatal("search leaked inaccessible relationship", page, err)
	}
	_, err = tm.svc.SearchMemories(bg, tm.bo, tm.pid(), "", "", r.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = tm.svc.MemoryHistory(bg, tm.bo, tm.pid(), r.ID)
	wantErr(t, err, domain.ErrNotFound)
}

func TestMemoryIndividualCorrectionCannotWithdrawApprovedPolicy(t *testing.T) {
	tm, _ := memoryGitProject(t)
	identity := laptop(t, "policy-source")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	p := memoryProposal(actor, tm.pid())
	p.ClientID = "policy_source"
	p.Scope.Kind = "repository"
	p.Scope.Repository = "github.com/acme/shop"
	p.Content.Kind = "policy"
	e := integration.MemoryEvent{Schema: integration.MemorySchema, ID: p.ClientID, Sequence: 1, SourceID: p.Content.Source.ID, Revision: 1, Action: "propose", Proposal: p}
	ack, err := tm.svc.DeliverMemory(bg, actor, e)
	if err != nil {
		t.Fatal(err)
	}
	r, err := tm.svc.ChangeMemory(bg, tm.owner, ack.Record.ID, memoryCommand(ack.Record, "approve"))
	if err != nil {
		t.Fatal(err)
	}
	// Protect an approved policy before it is even published, as well as afterwards.
	for _, state := range []string{"approved", "published"} {
		correction := e
		correction.ID = "policy_correction"
		correction.Proposal.ClientID = correction.ID
		correction.Sequence = 2
		correction.Revision = 2
		correction.Previous = e.ID
		correction.Proposal.Content.Kind = "preference"
		_, err = tm.svc.DeliverMemory(bg, actor, correction)
		wantErr(t, err, domain.ErrForbidden)
		page, err := tm.svc.SearchMemories(bg, tm.owner, tm.pid(), "", "", r.ID)
		if err != nil || page.Records[0].State != state {
			t.Fatal("policy changed", page, err)
		}
		if state == "approved" {
			r, err = tm.svc.ChangeMemory(bg, tm.owner, r.ID, memoryCommand(r, "publish"))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMemoryCompactedReceiptStillReplaysCurrentRecord(t *testing.T) {
	tm, now := memoryGitProject(t)
	identity := laptop(t, "compacted-receipt")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	p := memoryProposal(actor, tm.pid())
	p.Scope.Kind = "repository"
	p.Scope.Repository = "github.com/acme/shop"
	p.ClientID = "receipt_compaction"
	e := integration.MemoryEvent{Schema: integration.MemorySchema, ID: p.ClientID, Sequence: 1, SourceID: p.Content.Source.ID, Revision: 1, Action: "propose", Proposal: p}
	ack, err := tm.svc.DeliverMemory(bg, actor, e)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(store.MemoryAuditRetention + time.Hour)
	if err = tm.svc.ReconcileMemory(bg); err != nil {
		t.Fatal(err)
	}
	replay, err := tm.svc.DeliverMemory(bg, actor, e)
	if err != nil || replay.Record.ID != ack.Record.ID || replay.Record.Content.Summary != p.Content.Summary {
		t.Fatal("compaction broke replay", replay, err)
	}
}

func TestMemoryTracksReviewActivityWithoutApprovingCode(t *testing.T) {
	tm, now := memoryGitProject(t)
	k := tm.working(tm.bo, "Review storage")
	sha := strings.Repeat("a", 40)
	p := memoryProposal(tm.bo, tm.pid())
	p.ClientID = "review_context"
	p.Content.Source = integration.MemorySource{Kind: "git", ID: "implementation", Revision: sha}
	p.Content.Context = &integration.MemoryContext{Claim: "storage", Branch: k.Branch, TaskID: k.ID}
	r, err := tm.svc.ProposeMemory(bg, tm.bo, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = tm.svc.ReportBranches(bg, tm.bo, tm.pid(), []BranchInput{{Name: k.Branch, BranchState: BranchState{HeadSHA: sha}}}, nil); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if _, err = tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{ReviewerID: tm.di.Member.ID}); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.TaskState != string(domain.TicketReview) || f.ReviewState != "pending" || f.Status != "stale" {
		t.Fatal("review submission did not invalidate", f)
	}
	*now = now.Add(time.Minute)
	if _, err = tm.svc.RequestChanges(bg, tm.di, tm.pid(), k.ID, "Use atomic writes"); err != nil {
		t.Fatal(err)
	}
	if f := readFreshness(t, tm, tm.bo, r.ID); f.ReviewState != "not_in_review" || f.Status != "stale" || f.IntegrationState != "branch_only" {
		t.Fatal("requested changes were treated as approval", f)
	}
}
