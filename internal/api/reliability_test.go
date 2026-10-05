package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func TestGitCompareAllowsCompleteBoundedCommitReport(t *testing.T) {
	g := newGitAPI(t, "")
	gitIn(t, g.repo, "checkout", g.branch)
	for i := 0; i < 55; i++ {
		gitIn(t, g.repo, "commit", "--allow-empty", "-q", "-m", "bridge report")
	}
	var cmp domain.GitComparison
	endpoint := g.url + "/api/projects/" + g.project.ID + "/git/compare?scope=local&branch=" + g.branch
	if code := do(t, "GET", endpoint, "", &cmp); code != 200 || !cmp.Unique.Truncated || len(cmp.Unique.Items) != 50 {
		t.Fatalf("default bounded page: %d %+v", code, cmp.Unique)
	}
	if code := do(t, "GET", endpoint+"&commitLimit=200", "", &cmp); code != 200 || cmp.Unique.Truncated || len(cmp.Unique.Items) != cmp.Unique.Total {
		t.Fatalf("complete report: %d %+v", code, cmp.Unique)
	}
	if code := do(t, "GET", endpoint+"&commitLimit=201", "", nil); code != 400 {
		t.Fatalf("unbounded reporting accepted: %d", code)
	}
}

func TestEventReplayRequestsSnapshotAfterRetention(t *testing.T) {
	server := newTestServer(t, func(o *Options) {
		if err := o.Store.Update(context.Background(), func(tx store.Tx) error {
			return tx.Settings().Set(context.Background(), "events:replay-floor", int64(100), time.Now())
		}); err != nil {
			t.Fatal(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/events?after=2", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "event: resync_required" {
			return
		}
	}
}
