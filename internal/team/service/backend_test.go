package service

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
	"devboard/internal/team/store/replicated"
)

// The service's tests run on a single SQLite file, as they always have. With WERKBORD_TEST_STORE=rqlite they run on a real
// database cluster instead (WERKBORD_TEST_STORE_NODES nodes, default 1): the pinned program, as Team ships it, with the
// replicated store on top, which is how a use case runs in production. Every use case is thereby checked to give the same
// answers on both, which is what "the replicated store supports every Team operation" has to mean.

var sharedCluster *rqlitetest.Cluster

func TestMain(m *testing.M) {
	if os.Getenv("WERKBORD_TEST_STORE") != "rqlite" {
		os.Exit(m.Run())
	}
	nodes := 1
	if v, err := strconv.Atoi(os.Getenv("WERKBORD_TEST_STORE_NODES")); err == nil && v > 0 {
		nodes = v
	}
	tb := &rqlitetest.MainTB{}
	sharedCluster = rqlitetest.New(tb, rqlitetest.Options{Nodes: nodes})
	code := m.Run()
	tb.Close()
	os.Exit(code)
}

var hostCounter int

func newReplicatedDB(t *testing.T) (store.Store, string) {
	t.Helper()
	i := hostCounter % len(sharedCluster.Nodes())
	hostCounter++
	nodes := []string{sharedCluster.Node(i).HTTP.String()}
	for j, n := range sharedCluster.Nodes() {
		if j != i {
			nodes = append(nodes, n.HTTP.String())
		}
	}
	dir := t.TempDir()
	db, err := replicated.Open(bg, replicated.Options{Dir: dir, Nodes: nodes, Auth: replicated.Auth{User: rqlite.UserApp, Pass: sharedCluster.Creds.App},
		HostID: sharedCluster.Node(i).ID, Create: true, Poll: 30 * time.Millisecond, WaitForCluster: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, filepath.Join(dir, "replica.db")
}

// clusterFiles is everything the cluster's nodes keep on disk, as bytes.
func clusterFiles(t *testing.T) []byte {
	t.Helper()
	var all []byte
	for _, n := range sharedCluster.Nodes() {
		_ = filepath.WalkDir(filepath.Join(n.Config().DataDir, "node"), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, err := os.ReadFile(path); err == nil {
					all = append(all, b...)
				}
			}
			return nil
		})
	}
	return all
}
