package integration

import "testing"

func TestProjectTransferRejectsUnsafeRemoteAndKeepsStableIdentity(t *testing.T) {
	p := ProjectTransfer{Source: "personal/p1/team:main", Name: "Project"}
	for _, repo := range []string{"/Users/me/private", "https://token@github.com/acme/repo", "https://github.com/acme/repo?token=secret", "file:///private/repo"} {
		p.Repository = repo
		if p.Validate() == nil {
			t.Fatalf("unsafe repository accepted: %s", repo)
		}
	}
	p.Repository = "https://github.com/acme/repo"
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"title\nsecond line", "title\x00"} {
		bad := p
		bad.Tickets = []TransferTicket{{Title: title}}
		if bad.Validate() == nil {
			t.Fatal("invalid title accepted")
		}
	}
	q := p
	q.Tickets = []TransferTicket{}
	q.Revision = "new"
	if p.TransferID("prj") != q.TransferID("prj") {
		t.Fatal("revision or empty collection changed retry identity")
	}
	if p.Equal(q) || !p.SameText(q) {
		t.Fatal("revision and text comparisons disagree")
	}
	q.Source = "personal/p2/team:main"
	if p.TransferID("prj") == q.TransferID("prj") {
		t.Fatal("provenance collapsed")
	}
}
