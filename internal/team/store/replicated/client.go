package replicated

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Auth is the user the storage layer presents to the database: the application user of the workspace's
// database credentials (rqlite.UserApp), or its administrator for the few calls that need one.
type Auth struct{ User, Pass string }

// Client talks to the nodes of one workspace's rqlite cluster, and only to them: it is the one way Team's
// storage layer reaches the database, it accepts only addresses on loopback or a private network, and
// nothing outside a Workspace Host's own service holds one. A request may be sent to any node (rqlite
// forwards writes and consistent reads to the leader), so a call that did not reach one node is tried on
// the next.
type Client struct {
	hc   *http.Client
	auth Auth

	mu    sync.Mutex
	nodes []netip.AddrPort // preferred first
}

// Node and cluster errors the callers tell apart.
var (
	// ErrUnreachable: no node could be reached at all. Nothing was sent that anything acted on.
	ErrUnreachable = errors.New("replicated: no node of the database cluster could be reached")
	// ErrNoLeader: the nodes that answered have no leader, which is what losing quorum looks like.
	ErrNoLeader = errors.New("replicated: the database cluster has no leader")
)

// RemoteError is an error the database returned that is not about one statement.
type RemoteError struct {
	Status int
	Msg    string
}

func (e *RemoteError) Error() string { return fmt.Sprintf("database: %d: %s", e.Status, e.Msg) }

// StatementError is the database refusing one statement of a request.
type StatementError struct {
	Index int
	Msg   string
}

func (e *StatementError) Error() string {
	return fmt.Sprintf("database: statement %d: %s", e.Index+1, e.Msg)
}

// ParseAddrs checks that every address is "host:port" with an IP on loopback or a private network.
func ParseAddrs(addrs []string) ([]netip.AddrPort, error) {
	var out []netip.AddrPort
	for _, a := range addrs {
		ap, err := netip.ParseAddrPort(strings.TrimPrefix(a, "http://"))
		if err != nil {
			return nil, fmt.Errorf("replicated: database node address %q: %w", a, err)
		}
		if ip := ap.Addr().Unmap(); !ip.IsLoopback() && !ip.IsPrivate() {
			return nil, fmt.Errorf("replicated: database node address %s is not on loopback or a private network: the database is only reached inside the workspace's own network", ap)
		}
		out = append(out, ap)
	}
	return out, nil
}

// NewClient returns a Client for the nodes at addrs.
func NewClient(addrs []string, auth Auth) (*Client, error) {
	c := &Client{auth: auth, hc: &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       30 * time.Second,
			ResponseHeaderTimeout: 40 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if err := c.SetNodes(addrs); err != nil {
		return nil, err
	}
	return c, nil
}

// SetNodes replaces the known nodes (the first is tried first).
func (c *Client) SetNodes(addrs []string) error {
	nodes, err := ParseAddrs(addrs)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return errors.New("replicated: no database node address was given")
	}
	c.mu.Lock()
	c.nodes = nodes
	c.mu.Unlock()
	return nil
}

// Nodes returns the known node addresses, preferred first.
func (c *Client) Nodes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.nodes))
	for i, n := range c.nodes {
		out[i] = n.String()
	}
	return out
}

// Prefer moves a node to the front of the order, which a caller does for a node it just found healthy or the leader.
func (c *Client) Prefer(addr string) {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, n := range c.nodes {
		if n == ap {
			c.nodes[0], c.nodes[i] = c.nodes[i], c.nodes[0]
			return
		}
	}
}

// demote moves a node to the back of the order. A caller does it for a node that took a request and then did not
// answer (typically one whose own connection to the leader has gone stale after a network was mended), so that the
// next request, and in particular the barrier that finds out what became of the lost one, goes to another node
// instead of the same one again.
func (c *Client) demote(node netip.AddrPort) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.nodes) < 2 {
		return
	}
	for i, n := range c.nodes {
		if n == node {
			c.nodes = append(append(append([]netip.AddrPort(nil), c.nodes[:i]...), c.nodes[i+1:]...), node)
			return
		}
	}
}

func (c *Client) order() []netip.AddrPort {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]netip.AddrPort(nil), c.nodes...)
}

// result of one HTTP exchange.
type exchange struct {
	status int
	body   []byte
	sent   bool // the request may have reached the node (a connection was made)
	err    error
}

func (c *Client) do(ctx context.Context, node netip.AddrPort, method, path string, q url.Values, body []byte, ctype string) exchange {
	u := "http://" + node.String() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return exchange{err: err}
	}
	req.SetBasicAuth(c.auth.User, c.auth.Pass)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return exchange{err: err} // never connected: nothing was sent
		}
		return exchange{sent: true, err: err}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 256<<20))
	if err != nil {
		return exchange{sent: true, status: res.StatusCode, err: err}
	}
	return exchange{sent: true, status: res.StatusCode, body: b}
}

func isNoLeader(ex exchange) bool {
	return ex.status == http.StatusServiceUnavailable && strings.Contains(strings.ToLower(string(ex.body)), "leader")
}

// ---- reads ----

// Result is one statement's rows.
type Result struct {
	Columns []string
	Values  [][]any // numbers are json.Number, text string, NULL nil, BLOB base64 string
}

// Level is a read consistency level (rqlite's "level").
type Level string

const (
	// LevelNone reads the node's own copy: it needs no leader and no quorum, and may be behind.
	LevelNone Level = "none"
	// LevelLinearizable is a read the leader confirms with a quorum: it sees every write that was
	// acknowledged before it began. It needs a quorum.
	LevelLinearizable Level = "linearizable"
	// LevelWeak is a read served by the leader.
	LevelWeak Level = "weak"
)

// Query runs read statements on the first node that answers. A statement that fails is a *StatementError.
func (c *Client) Query(ctx context.Context, level Level, stmts ...Stmt) ([]Result, error) {
	payload := make([][]any, len(stmts))
	for i, s := range stmts {
		payload[i] = s.wire()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	q := url.Values{"level": {string(level)}, "timeout": {"8s"}}
	var last error = ErrUnreachable
	nudged := false
	for _, node := range c.order() {
	again:
		ex := c.do(ctx, node, http.MethodPost, "/db/query", q, body, "application/json")
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case ex.err != nil:
			last = fmt.Errorf("%w: %v", ErrUnreachable, ex.err)
			continue
		case isNoLeader(ex):
			last = ErrNoLeader
			continue
		case ex.status == http.StatusOK:
			res, err := parseQuery(ex.body)
			var plain errPlain
			if errors.As(err, &plain) {
				if level == LevelLinearizable && !nudged && (strings.Contains(string(plain), "waiting for fsm") || strings.Contains(string(plain), "timeout")) {
					// A linearizable read waits for the database to have applied the leader's last committed entry, and an
					// entry that is not a write (a change of who is in the cluster, a new leader's first entry) is never
					// applied by it: the read would wait until the next write. A write that changes nothing moves it on.
					nudged = true
					if _, _, nerr := c.Execute(ctx, []Stmt{{SQL: nudgeSQL}}); nerr == nil {
						goto again
					}
				}
				// The node could not serve it (its leader had just gone): another is asked.
				last = err
				continue
			}
			if err == nil && len(res) != len(stmts) {
				// A node that answered with nothing, or with fewer answers than questions (it was between
				// leaders): another is asked.
				last = &RemoteError{ex.status, fmt.Sprintf("the node answered %d of %d queries", len(res), len(stmts))}
				continue
			}
			return res, err
		case ex.status == http.StatusUnauthorized || ex.status == http.StatusForbidden:
			return nil, &RemoteError{ex.status, "the database refused this host's credentials"}
		default:
			last = &RemoteError{ex.status, strings.TrimSpace(string(ex.body))}
			if ex.status >= 400 && ex.status < 500 {
				return nil, last
			}
		}
	}
	return nil, last
}

// nudgeSQL is a write that changes nothing and is applied by the database like any other (see Query).
const nudgeSQL = `UPDATE _meta SET value = value WHERE key = 'format'`

// errPlain is what a node says in an answer's own "error" when it could not do what was asked at all (for
// instance when it forwarded the request to a leader that had just died). It is not about any one statement.
type errPlain string

func (e errPlain) Error() string { return "database: " + string(e) }

// neverSent reports whether a node's own error says the request did not get to where it was going.
func neverSent(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{"connection refused", "no such host", "no route to host", "network is unreachable", "leader not found"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

func parseQuery(b []byte) ([]Result, error) {
	var raw struct {
		Error   string `json:"error"`
		Results []struct {
			Columns []string `json:"columns"`
			Values  [][]any  `json:"values"`
			Error   string   `json:"error"`
		} `json:"results"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("database: unreadable answer: %w", err)
	}
	if raw.Error != "" {
		return nil, errPlain(raw.Error)
	}
	out := make([]Result, len(raw.Results))
	for i, r := range raw.Results {
		if r.Error != "" {
			return nil, &StatementError{Index: i, Msg: r.Error}
		}
		out[i] = Result{Columns: r.Columns, Values: r.Values}
	}
	return out, nil
}

// ---- writes ----

// Outcome says what is known about a write after Execute.
type Outcome int

const (
	// OutcomeCommitted: every statement ran and the transaction was committed by the cluster.
	OutcomeCommitted Outcome = iota
	// OutcomeRolledBack: the database refused a statement and rolled the whole transaction back.
	OutcomeRolledBack
	// OutcomeNotApplied: it is certain nothing was written: no node could be reached, the cluster had no
	// leader to take it, or the request was refused before it was looked at.
	OutcomeNotApplied
	// OutcomeUnknown: the request may have reached the cluster and the answer was lost (a timeout, a
	// connection that broke). The write may have happened and may still happen; the caller must find out
	// before it does anything else with it.
	OutcomeUnknown
)

// ExecResult is one statement's effect.
type ExecResult struct {
	RowsAffected int64
}

// Execute sends the statements as one transaction (rqlite's ?transaction: all of them take effect, or
// none, and it stops at the first that fails). A node that cannot be reached is skipped and the next tried,
// because nothing was sent; once a request has been sent it is never sent again by this call.
func (c *Client) Execute(ctx context.Context, stmts []Stmt) ([]ExecResult, Outcome, error) {
	payload := make([][]any, len(stmts))
	for i, s := range stmts {
		payload[i] = s.wire()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, OutcomeNotApplied, err
	}
	q := url.Values{"transaction": {""}, "timeout": {"10s"}}
	var last error = ErrUnreachable
	for _, node := range c.order() {
		ex := c.do(ctx, node, http.MethodPost, "/db/execute", q, body, "application/json")
		switch {
		case ex.err != nil && !ex.sent:
			if ctx.Err() != nil {
				return nil, OutcomeNotApplied, ctx.Err()
			}
			last = fmt.Errorf("%w: %v", ErrUnreachable, ex.err)
			continue
		case ex.err != nil:
			c.demote(node)
			return nil, OutcomeUnknown, ex.err
		case isNoLeader(ex):
			last = ErrNoLeader
			continue
		case ex.status == http.StatusOK:
			res, outcome, err := parseExecute(ex.body)
			var plain errPlain
			if outcome == OutcomeNotApplied && errors.As(err, &plain) {
				last = err
				continue // it did not get anywhere: another node may know the new leader
			}
			return res, outcome, err
		case ex.status == http.StatusUnauthorized || ex.status == http.StatusForbidden:
			return nil, OutcomeNotApplied, &RemoteError{ex.status, "the database refused this host's credentials"}
		case ex.status >= 400 && ex.status < 500:
			return nil, OutcomeNotApplied, &RemoteError{ex.status, strings.TrimSpace(string(ex.body))}
		default:
			c.demote(node)
			return nil, OutcomeUnknown, &RemoteError{ex.status, strings.TrimSpace(string(ex.body))}
		}
	}
	return nil, OutcomeNotApplied, last
}

func parseExecute(b []byte) ([]ExecResult, Outcome, error) {
	var raw struct {
		Error   string `json:"error"`
		Results []struct {
			RowsAffected int64  `json:"rows_affected"`
			Error        string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, OutcomeUnknown, fmt.Errorf("database: unreadable answer to a write: %w", err)
	}
	if raw.Error != "" {
		if neverSent(raw.Error) {
			return nil, OutcomeNotApplied, errPlain(raw.Error)
		}
		return nil, OutcomeUnknown, errPlain(raw.Error)
	}
	out := make([]ExecResult, len(raw.Results))
	for i, r := range raw.Results {
		if r.Error != "" {
			return nil, OutcomeRolledBack, &StatementError{Index: i, Msg: r.Error}
		}
		out[i] = ExecResult{RowsAffected: r.RowsAffected}
	}
	return out, OutcomeCommitted, nil
}

// ---- the whole database ----

// Backup writes a SQLite file holding the database. By default it is the leader's copy, which needs a
// quorum; with local it is the copy of the first node that answers (rqlite's noleader), which is a
// consistent point of the cluster's history and may be a little behind it, and needs no quorum.
func (c *Client) Backup(ctx context.Context, w io.Writer, local bool) error {
	q := url.Values{}
	if local {
		q.Set("noleader", "")
	}
	var last error = ErrUnreachable
	for _, node := range c.order() {
		err := c.backupFrom(ctx, node, q, w)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errPartial) {
			return err // some of it was written: another node cannot continue it
		}
		last = err
	}
	return last
}

var errPartial = errors.New("replicated: the backup was cut short")

func (c *Client) backupFrom(ctx context.Context, node netip.AddrPort, q url.Values, w io.Writer) error {
	u := "http://" + node.String() + "/db/backup"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.auth.User, c.auth.Pass)
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		if res.StatusCode == http.StatusServiceUnavailable && strings.Contains(strings.ToLower(string(b)), "leader") {
			return ErrNoLeader
		}
		return &RemoteError{res.StatusCode, strings.TrimSpace(string(b))}
	}
	n, err := io.Copy(w, res.Body)
	if err != nil {
		if n > 0 {
			return fmt.Errorf("%w: %v", errPartial, err)
		}
		return err
	}
	if msg := res.Trailer.Get("X-STREAM-ERROR"); msg != "" {
		return fmt.Errorf("%w: %s", errPartial, msg)
	}
	return nil
}

// Load replaces the whole database with a SQLite file, through the cluster (rqlite's /db/load): every
// node takes it. It needs the application user's "load" permission and a leader.
func (c *Client) Load(ctx context.Context, sqliteFile []byte) error {
	var last error = ErrUnreachable
	for _, node := range c.order() {
		ex := c.do(ctx, node, http.MethodPost, "/db/load", url.Values{"timeout": {"60s"}}, sqliteFile, "application/octet-stream")
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case ex.err != nil && ex.sent:
			return fmt.Errorf("the database did not confirm the load: %w", ex.err)
		case ex.err != nil:
			last = fmt.Errorf("%w: %v", ErrUnreachable, ex.err)
		case isNoLeader(ex):
			last = ErrNoLeader
		case ex.status == http.StatusOK:
			var raw struct {
				Results []struct {
					Error string `json:"error"`
				} `json:"results"`
			}
			if json.Unmarshal(ex.body, &raw) == nil {
				for _, r := range raw.Results {
					if r.Error != "" {
						return &RemoteError{ex.status, r.Error}
					}
				}
			}
			return nil
		default:
			return &RemoteError{ex.status, strings.TrimSpace(string(ex.body))}
		}
	}
	return last
}

// NodeInfo is a member of the cluster as the first node that answers reports it.
type NodeInfo struct {
	ID        string
	API       string
	Raft      string
	Voter     bool
	Reachable bool
	Leader    bool
	Error     string
}

// ClusterNodes asks the cluster for its members (read-only replicas included), sorted by ID. It also learns
// their API addresses, so that the client can reach a node it was not told about.
func (c *Client) ClusterNodes(ctx context.Context) ([]NodeInfo, error) {
	var last error = ErrUnreachable
	for _, node := range c.order() {
		ex := c.do(ctx, node, http.MethodGet, "/nodes", url.Values{"nonvoters": {""}, "timeout": {"3s"}}, nil, "")
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case ex.err != nil:
			last = fmt.Errorf("%w: %v", ErrUnreachable, ex.err)
			continue
		case ex.status != http.StatusOK:
			last = &RemoteError{ex.status, strings.TrimSpace(string(ex.body))}
			continue
		}
		m := map[string]struct {
			API       string `json:"api_addr"`
			Addr      string `json:"addr"`
			Voter     bool   `json:"voter"`
			Reachable bool   `json:"reachable"`
			Leader    bool   `json:"leader"`
			Error     string `json:"error"`
		}{}
		if err := json.Unmarshal(ex.body, &m); err != nil {
			return nil, err
		}
		out := make([]NodeInfo, 0, len(m))
		for id, n := range m {
			out = append(out, NodeInfo{ID: id, API: strings.TrimPrefix(n.API, "http://"), Raft: n.Addr, Voter: n.Voter, Reachable: n.Reachable, Leader: n.Leader, Error: n.Error})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return nil, last
}
