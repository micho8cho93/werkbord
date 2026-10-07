package rqlite

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The users rqlite is configured with. Every node of a workspace has the same three; their
// passwords are a workspace-host secret, sealed to each Workspace Host (internal/team/infra/pki),
// and are never in the replicated database or in anything the API returns.
const (
	// UserApp is what Team's own storage layer uses to read and write data.
	UserApp = "werkbord-app"
	// UserAdmin is what changes the cluster: removing a node, moving leadership, backing up.
	UserAdmin = "werkbord-admin"
	// UserNode is what a node joining the cluster presents.
	UserNode = "werkbord-node"
)

// Credentials are the passwords of the three users.
type Credentials struct {
	App, Admin, Node string
}

// NewCredentials makes fresh, random credentials.
func NewCredentials() (Credentials, error) {
	one := func() (string, error) {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(b), nil
	}
	var c Credentials
	var err error
	for _, dst := range []*string{&c.App, &c.Admin, &c.Node} {
		if *dst, err = one(); err != nil {
			return Credentials{}, err
		}
	}
	return c, nil
}

// Valid reports whether every password is present and long enough to be one.
func (c Credentials) Valid() error {
	for name, p := range map[string]string{"app": c.App, "admin": c.Admin, "node": c.Node} {
		if len(p) < 16 || strings.ContainsAny(p, "\"\\\r\n\x00:") {
			return fmt.Errorf("rqlite: the %s password is missing, too short or has a character it cannot hold", name)
		}
	}
	return nil
}

type authUser struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Perms    []string `json:"perms"`
}

// authFile renders rqlite's authentication file: what each user may do, and nothing more.
func (c Credentials) authFile() ([]byte, error) {
	return json.MarshalIndent([]authUser{
		{UserApp, c.App, []string{"execute", "query", "load", "backup", "status", "ready"}},
		{UserAdmin, c.Admin, []string{"all"}},
		{UserNode, c.Node, []string{"join", "join-read-only", "status", "ready"}},
	}, "", "  ")
}

var nodeIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// NodeConfig is everything Start needs, and all it takes: there is no field for a program, an
// argument, an environment variable or a path outside DataDir.
type NodeConfig struct {
	// DataDir is a directory the supervisor owns: it makes it private, keeps the node's data,
	// its credentials and the verified copy of the program in it, and writes nowhere else. It
	// must be absolute and owned by the user running Werkbord Team.
	DataDir string
	// NodeID names the node in the cluster for as long as it is a member; Team uses the device's
	// ID. It must not change.
	NodeID string
	// HTTPAddr and RaftAddr are where the node listens, and what it tells the others to use.
	// Each must be a loopback address or one on a private network (Network, if given): a node
	// is never bound to a wildcard or to a public address, because it holds a workspace's data
	// and Raft's own port is not authenticated.
	HTTPAddr, RaftAddr netip.AddrPort
	// HTTPAdvAddr and RaftAdvAddr are what the node tells the others to use, when that differs from
	// where it listens (behind a port mapping, or a test's proxy). They follow the same rules.
	HTTPAdvAddr, RaftAdvAddr netip.AddrPort
	// Network, if set, is the workspace's private network: a node that is not on loopback must
	// be inside it.
	Network netip.Prefix
	// Join are the Raft addresses of members of an existing cluster to join. Empty for the first
	// node of a cluster, which starts one of its own.
	Join []netip.AddrPort
	// NonVoter starts the node as a read-only replica: it holds all the data and takes no part in
	// elections or quorum. A host that is being added joins this way, catches up, and is made a
	// voter once it has (Supervisor.BecomeVoter).
	NonVoter bool
	// Credentials are the users the node is configured with.
	Credentials Credentials
	// ElectionTimeout and HeartbeatTimeout tune Raft; zero is rqlite's own default (one second
	// each). Slow networks raise them; tests lower them.
	ElectionTimeout, HeartbeatTimeout time.Duration
	// JoinAttempts is how many times a joining node tries (default 30, three seconds apart).
	JoinAttempts int
}

// advertised is the address a node asks others to use: its advertised address if it has one, else where it listens.
func advertised(adv, bind netip.AddrPort) netip.AddrPort {
	if adv.IsValid() {
		return adv
	}
	return bind
}

func (c NodeConfig) httpAdv() netip.AddrPort { return advertised(c.HTTPAdvAddr, c.HTTPAddr) }
func (c NodeConfig) raftAdv() netip.AddrPort { return advertised(c.RaftAdvAddr, c.RaftAddr) }

func checkBind(name string, ap netip.AddrPort, network netip.Prefix) error {
	if !ap.IsValid() || ap.Port() == 0 {
		return fmt.Errorf("rqlite: the %s address is not an address and a port", name)
	}
	a := ap.Addr().Unmap()
	switch {
	case a.IsUnspecified():
		return fmt.Errorf("rqlite: the %s address %s is a wildcard: the database listens only on loopback or on the workspace's private network", name, ap)
	case a.IsLoopback():
		return nil
	case network.IsValid() && network.Contains(a):
		return nil
	case !network.IsValid() && a.IsPrivate():
		return nil
	}
	return fmt.Errorf("rqlite: the %s address %s is not on loopback or on the workspace's private network: the database is never exposed", name, ap)
}

// Validate checks a configuration the way Start does, without starting anything.
func (c NodeConfig) Validate() error {
	if c.DataDir == "" || !filepath.IsAbs(c.DataDir) || filepath.Clean(c.DataDir) != c.DataDir {
		return errors.New("rqlite: the data directory must be a clean absolute path")
	}
	if strings.ContainsAny(c.DataDir, "\r\n\x00") {
		return errors.New("rqlite: the data directory has a character a command line cannot hold")
	}
	if !nodeIDRE.MatchString(c.NodeID) {
		return fmt.Errorf("rqlite: node ID %q: use letters, digits, '.', '_' and '-' (up to 64)", c.NodeID)
	}
	if err := checkBind("HTTP", c.HTTPAddr, c.Network); err != nil {
		return err
	}
	if err := checkBind("Raft", c.RaftAddr, c.Network); err != nil {
		return err
	}
	for name, ap := range map[string]netip.AddrPort{"advertised HTTP": c.HTTPAdvAddr, "advertised Raft": c.RaftAdvAddr} {
		if ap.IsValid() {
			if err := checkBind(name, ap, c.Network); err != nil {
				return err
			}
		}
	}
	if c.HTTPAddr == c.RaftAddr {
		return errors.New("rqlite: the HTTP and Raft addresses must differ")
	}
	if c.HTTPAddr.Addr() != c.RaftAddr.Addr() {
		return errors.New("rqlite: the HTTP and Raft addresses must be on the same interface")
	}
	for _, j := range c.Join {
		if !j.IsValid() || j.Port() == 0 {
			return fmt.Errorf("rqlite: join address %v is not an address and a port", j)
		}
		if j == c.raftAdv() {
			return errors.New("rqlite: a node cannot join itself")
		}
	}
	if len(c.Join) == 0 && c.NonVoter {
		return errors.New("rqlite: a read-only replica has to join a cluster")
	}
	if c.ElectionTimeout < 0 || c.HeartbeatTimeout < 0 || c.JoinAttempts < 0 {
		return errors.New("rqlite: timeouts and attempts cannot be negative")
	}
	return c.Credentials.Valid()
}

// paths of what a node keeps under DataDir.
func (c NodeConfig) nodeDir() string  { return filepath.Join(c.DataDir, "node") }
func (c NodeConfig) binDir() string   { return filepath.Join(c.DataDir, "bin") }
func (c NodeConfig) authPath() string { return filepath.Join(c.DataDir, "auth.json") }

// Initialized reports whether a node has run in dataDir before: whether it has Raft state, and
// so already belongs to a cluster (possibly of one) and ignores any request to join one.
func Initialized(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, "node", "raft.db"))
	return err == nil
}

// args are the program's command line for the configuration. Every value is a constant, a
// number, or an address and a name this package validated: nothing a member wrote reaches it.
func (c NodeConfig) args() []string {
	a := []string{
		"-node-id", c.NodeID,
		"-http-addr", c.HTTPAddr.String(), "-http-adv-addr", c.httpAdv().String(),
		"-raft-addr", c.RaftAddr.String(), "-raft-adv-addr", c.raftAdv().String(),
		"-auth", c.authPath(),
		// Foreign keys: Team's schema depends on them (cascades, and the composite keys that
		// keep a member from being put on a project of another workspace).
		"-fk",
		// A node leaves the cluster only when it is told to, by a membership change that checks
		// quorum first; stopping one is not that.
		"-raft-shutdown-stepdown=true",
	}
	if c.ElectionTimeout > 0 {
		a = append(a, "-raft-election-timeout", c.ElectionTimeout.String())
	}
	if c.HeartbeatTimeout > 0 {
		a = append(a, "-raft-heartbeat-timeout", c.HeartbeatTimeout.String())
	}
	// A node that is already a member ignores these (rqlite: autoclustering is idempotent), so
	// they are given on every start; one that was taken out of the cluster uses them to rejoin.
	if len(c.Join) > 0 {
		var addrs []string
		for _, j := range c.Join {
			addrs = append(addrs, j.String())
		}
		attempts := c.JoinAttempts
		if attempts == 0 {
			attempts = 30
		}
		a = append(a, "-join", strings.Join(addrs, ","), "-join-as", UserNode, "-join-attempts", strconv.Itoa(attempts), "-join-interval", "3s")
	}
	if c.NonVoter {
		a = append(a, "-raft-non-voter")
	}
	a = append(a, c.nodeDir())
	return a
}
