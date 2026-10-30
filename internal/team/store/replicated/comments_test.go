package replicated

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"werkbord/internal/team/domain"
	"werkbord/internal/team/infra/rqlite/rqlitetest"
	"werkbord/internal/team/store"
)

// Comments, the people they mention and what each mention tells its recipient are ordinary rows of the replicated store: the
// migration that adds them runs once for the whole cluster with a workspace's data in it, every host reads what any host wrote,
// and what the database does by itself (the workspace revision moving with a read, a notification withdrawn when its recipient
// leaves the project) happens the same way on every host.

func migrationIndex(t testing.TB, name string) int {
	t.Helper()
	all, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range all {
		if m.Name == name {
			return i
		}
	}
	t.Fatalf("migration %s is missing", name)
	return -1
}

func TestTheCommentsMigrationRunsOnceOnTheClusterAndTheConversationReplicates(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	all, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	target := migrationIndex(t, "ticket_comments")

	// A workspace from before comments, with a member and a ticket in it.
	old := open(t, c, 0, func(o *Options) { o.Migrations = all[:target] })
	k := seedBeforeSourceRef(t, old)
	settle(t, old)
	seq := position(t, old).Seq
	_ = old.Close()

	// Two hosts of the new version start together: the migration is applied once, as one write, and the data is still there.
	var wg sync.WaitGroup
	news := make([]*Store, 2)
	for i := range news {
		wg.Add(1)
		go func() {
			defer wg.Done()
			news[i] = open(t, c, i+1, func(o *Options) { o.Migrations = all[:target+1] })
		}()
	}
	wg.Wait()
	settle(t, news...)
	for i, s := range news {
		if v, _ := s.SchemaVersion(bg); v != target+1 {
			t.Fatalf("host %d: schema %d, want %d", i, v, target+1)
		}
	}
	if got := position(t, news[0]).Seq; got != seq+1 {
		t.Fatalf("the cluster is at position %d, want %d: the migration was not one write", got, seq+1)
	}
	// This historical schema predates columns used by today's Ticket query.
	var title string
	if err := news[1].update(bg, func(q store.Queryer) error {
		return q.QueryRowContext(bg, `SELECT title FROM tickets WHERE id=?`, k.ticket).Scan(&title)
	}); err != nil || title != "First" {
		t.Fatalf("the ticket did not survive the migration: %q, %v", title, err)
	}
	hosts := []*Store{news[0], news[1], open(t, c, 0, func(o *Options) { o.Migrations = all[:target+1] })}
	settle(t, hosts...)

	bo := addMember(t, hosts[0], k, "Bo")
	post := func(h *Store, author, clientID string, mention string) (domain.Comment, error) {
		var out domain.Comment
		err := h.Update(bg, func(tx store.Tx) error {
			if prior, found, err := tx.CommentByClientID(bg, k.ws, k.ticket, author, clientID); err != nil || found {
				out = prior
				return err
			}
			segs := []domain.Segment{{Type: domain.SegmentText, Text: "hello "}}
			if mention != "" {
				segs = append(segs, domain.Segment{Type: domain.SegmentMention, MemberID: mention, Name: "Bo"})
			}
			made, err := tx.InsertComment(bg, k.ws, domain.Comment{ID: domain.NewID(domain.PrefixComment), ProjectID: k.project, TicketID: k.ticket, AuthorID: author, AuthorName: "A",
				Segments: segs, ClientID: clientID, ClientDigest: domain.SegmentsDigest(segs), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)})
			if err != nil {
				return err
			}
			out = made
			if mention != "" {
				_, err = tx.NotifyMention(bg, k.ws, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: k.project, TicketID: k.ticket, CommentID: made.ID,
					RecipientID: mention, AuthorID: author, CreatedAt: made.CreatedAt})
			}
			return err
		})
		return out, err
	}

	// A comment written on one host, mentioning Bo, is read on another, and Bo has one notification everywhere.
	first, err := post(hosts[0], k.owner, "client-first-01", bo)
	if err != nil || first.Seq != 1 {
		t.Fatalf("posting: %+v %v", first, err)
	}
	settle(t, hosts...)
	unread := func(h *Store, member string) (n int) {
		_ = h.View(bg, func(tx store.Tx) error {
			counts, err := tx.UnreadNotifications(bg, k.ws, member)
			for _, v := range counts {
				n += v
			}
			return err
		})
		return n
	}
	for i, h := range hosts {
		if unread(h, bo) != 1 || unread(h, k.owner) != 0 {
			t.Fatalf("host %d: Bo waits for %d", i, unread(h, bo))
		}
		var page store.CommentPage
		if err := h.View(bg, func(tx store.Tx) (err error) {
			page, err = tx.CommentPage(bg, k.ws, k.ticket, store.CommentPageQuery{})
			return
		}); err != nil || len(page.Comments) != 1 || page.Comments[0].ID != first.ID {
			t.Fatalf("host %d reads %+v %v", i, page, err)
		}
	}

	// Reading it, on a third host, moves the workspace revision on all of them, and the counts clear everywhere.
	revision := func(h *Store) (n int64) {
		_ = h.View(bg, func(tx store.Tx) (err error) { n, err = tx.WorkspaceRevision(bg, k.ws); return })
		return n
	}
	before := revision(hosts[0])
	var id string
	if err := hosts[2].View(bg, func(tx store.Tx) error {
		rows, err := tx.Notifications(bg, k.ws, bo, store.NotificationQuery{})
		if err == nil && len(rows) == 1 {
			id = rows[0].ID
		}
		return err
	}); err != nil || id == "" {
		t.Fatalf("finding the notification: %v", err)
	}
	if err := hosts[2].Update(bg, func(tx store.Tx) error {
		_, changed, err := tx.MarkNotificationRead(bg, k.ws, bo, id, time.Now().UTC().Truncate(time.Millisecond))
		if err == nil && !changed {
			t.Error("marking it read changed nothing")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	settle(t, hosts...)
	for i, h := range hosts {
		if unread(h, bo) != 0 {
			t.Errorf("host %d still counts it", i)
		}
		if got := revision(h); got <= before {
			t.Errorf("host %d: the revision did not move with the read (%d -> %d)", i, before, got)
		}
	}

	// A notification is withdrawn when its recipient leaves the project, on every host, and does not come back.
	second, err := post(hosts[1], k.owner, "client-second-1", bo)
	if err != nil {
		t.Fatal(err)
	}
	settle(t, hosts...)
	if unread(hosts[2], bo) != 1 {
		t.Fatal("the second mention did not arrive")
	}
	if err := hosts[0].Update(bg, func(tx store.Tx) error { _, err := tx.RemoveProjectMember(bg, k.ws, k.project, bo); return err }); err != nil {
		t.Fatal(err)
	}
	if err := hosts[1].Update(bg, func(tx store.Tx) error {
		return tx.AddProjectMember(bg, k.ws, domain.ProjectMember{ProjectID: k.project, MemberID: bo, AddedBy: k.owner, AddedAt: time.Now(), Role: domain.ProjectContributor})
	}); err != nil {
		t.Fatal(err)
	}
	settle(t, hosts...)
	for i, h := range hosts {
		if unread(h, bo) != 0 {
			t.Errorf("host %d: a notification came back after leaving and rejoining", i)
		}
	}
	_ = second

	// Posting under one creation id from every host at once makes one comment, and posts from every host take places 2..n in order.
	var posted sync.WaitGroup
	results := make([]domain.Comment, len(hosts))
	for i, h := range hosts {
		posted.Add(1)
		go func() {
			defer posted.Done()
			cm, err := post(h, k.owner, "client-same-001", "")
			if err != nil {
				t.Errorf("host %d: %v", i, err)
			}
			results[i] = cm
		}()
	}
	posted.Wait()
	for i := range results {
		if results[i].ID != results[0].ID {
			t.Fatalf("the hosts posted different comments under one creation id: %v", results)
		}
	}
	for n := 0; n < 9; n++ {
		h := hosts[n%len(hosts)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := post(h, k.owner, fmt.Sprintf("client-many-%03d", n), ""); err != nil {
				t.Errorf("post %d: %v", n, err)
			}
		}()
	}
	wg.Wait()
	settle(t, hosts...)
	if err := hosts[0].View(bg, func(tx store.Tx) error {
		page, err := tx.CommentPage(bg, k.ws, k.ticket, store.CommentPageQuery{Limit: 100})
		if err != nil {
			return err
		}
		// first, second, the one under the shared creation id, and nine more: a place each, no gaps, no repeats.
		if len(page.Comments) != 12 {
			t.Fatalf("%d comments, want 12", len(page.Comments))
		}
		for i, cm := range page.Comments {
			if cm.Seq != int64(i+1) {
				t.Fatalf("comment %d has place %d", i+1, cm.Seq)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
