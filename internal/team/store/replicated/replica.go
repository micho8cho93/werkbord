package replicated

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"devboard/internal/sqlitekit"
)

// replica is this host's copy of the cluster's database: a SQLite file that is the cluster's data as of
// a position. Reads are served from it, a use case runs against it, and it is made again from the cluster
// whenever it cannot be trusted to be a point in the cluster's history.
//
// Making it again never waits for anyone: the new copy is a new file (replica-000007.db after replica-000006.db), the
// store switches to it at once, and the old one is closed and removed when the reads that were using it have finished.
// So a read that is in the middle of something never blocks a new copy, and a new copy never blocks a read.
type replica struct {
	dir string

	mu  sync.RWMutex
	cur *generation
	gen int
}

// generation is one file of the copy and the reads that are using it.
type generation struct {
	n    int
	path string
	pool *sqlitekit.Pool
	wg   sync.WaitGroup
}

var genFile = regexp.MustCompile(`^replica-(\d{6})\.db$`)

func genPath(dir string, n int) string { return filepath.Join(dir, fmt.Sprintf("replica-%06d.db", n)) }

func openReplica(ctx context.Context, dir string) (*replica, error) {
	r := &replica{dir: dir}
	// A copy kept by an earlier version under one name becomes the first generation.
	old := filepath.Join(dir, "replica.db")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var gens []int
	for _, e := range entries {
		if m := genFile.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[1])
			gens = append(gens, n)
		}
	}
	if len(gens) == 0 {
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, genPath(dir, 1)); err != nil {
				return nil, err
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				_ = os.Rename(old+suffix, genPath(dir, 1)+suffix)
			}
			gens = append(gens, 1)
		}
	}
	sort.Ints(gens)
	n := 1
	if len(gens) > 0 {
		n = gens[len(gens)-1]
		// What an older generation left (a crash between switching and removing it) is removed.
		for _, g := range gens[:len(gens)-1] {
			removeFiles(genPath(dir, g))
		}
	}
	g, err := openGeneration(ctx, dir, n)
	if err != nil {
		return nil, err
	}
	r.cur, r.gen = g, n
	return r, nil
}

func openGeneration(ctx context.Context, dir string, n int) (*generation, error) {
	path := genPath(dir, n)
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Replica: true})
	if err != nil {
		return nil, err
	}
	return &generation{n: n, path: path, pool: p}, nil
}

func removeFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

var errClosed = errors.New("replicated: closed")

// acquire gives a read the current copy, and a function to call when it is done with it. The copy it was given is not
// closed under it, even if a newer one is taken meanwhile.
func (r *replica) acquire() (*sqlitekit.Pool, func(), error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cur == nil {
		return nil, nil, errClosed
	}
	g := r.cur
	g.wg.Add(1)
	return g.pool, g.wg.Done, nil
}

// writer is the copy's one writer's pool. Only whoever holds the store's writer lock uses it, which is also whoever may
// install a new copy, so it cannot change under them.
func (r *replica) writer() (*sqlitekit.Pool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cur == nil {
		return nil, errClosed
	}
	return r.cur.pool, nil
}

func (r *replica) close() error {
	r.mu.Lock()
	g := r.cur
	r.cur = nil
	r.mu.Unlock()
	if g == nil {
		return nil
	}
	g.wg.Wait()
	return g.pool.Close()
}

var errNoFence = errors.New("replicated: the local copy has no position yet")

// fence reads the copy's position.
func (r *replica) fence(ctx context.Context) (fence, error) {
	p, release, err := r.acquire()
	if err != nil {
		return fence{}, err
	}
	defer release()
	var f fence
	err = p.Reader.QueryRowContext(ctx, readFenceSQL).Scan(&f.Seq, &f.Chain, &f.Epoch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no such table") {
			return fence{}, errNoFence
		}
		return fence{}, err
	}
	return f, nil
}

// install makes a SQLite file the copy. The file is checked before anything is touched: it must pass SQLite's integrity
// check and have a position. The caller holds the store's writer lock.
func (r *replica) install(ctx context.Context, file string) (fence, error) {
	f, err := checkFile(ctx, file)
	if err != nil {
		return fence{}, err
	}
	r.mu.RLock()
	next := r.gen + 1
	r.mu.RUnlock()
	path := genPath(r.dir, next)
	removeFiles(path)
	if err := os.Rename(file, path); err != nil {
		return fence{}, err
	}
	_ = os.Chmod(path, 0o600)
	g, err := openGeneration(ctx, r.dir, next)
	if err != nil {
		return fence{}, err
	}
	r.mu.Lock()
	old := r.cur
	r.cur, r.gen = g, next
	r.mu.Unlock()
	if old != nil {
		go func() {
			old.wg.Wait()
			_ = old.pool.Close()
			removeFiles(old.path)
		}()
	}
	return f, nil
}

// checkFile opens a SQLite file read-only and checks it is whole and has a position.
func checkFile(ctx context.Context, file string) (fence, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return fence{}, err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return fence{}, fmt.Errorf("replicated: the database file cannot be read: %w", err)
	}
	if res != "ok" {
		return fence{}, fmt.Errorf("replicated: the database file failed SQLite's integrity check: %s", res)
	}
	var f fence
	if err := db.QueryRowContext(ctx, readFenceSQL).Scan(&f.Seq, &f.Chain, &f.Epoch); err != nil {
		return fence{}, fmt.Errorf("replicated: the database file has no position in the workspace's history (it is not a workspace database of this format): %w", err)
	}
	return f, nil
}
