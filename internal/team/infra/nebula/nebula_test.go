//go:build !windows

package nebula

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
)

// ---- the pin ----

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestThePinIsWellFormedAndMatchesWhatUpstreamPublished(t *testing.T) {
	// third_party/nebula/SHASUM256-v<version>.txt is the checksum file the release itself
	// published, kept in the repository so that a change to the pin is checked against
	// something other than the pin.
	_, file, _, _ := runtime.Caller(0)
	published, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "third_party", "nebula", "SHASUM256-v"+Version+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(published), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			have[f[1]+" "+f[0]] = true
		}
	}
	if len(Platforms()) != 4 {
		t.Errorf("platforms = %v", Platforms())
	}
	for _, p := range Platforms() {
		goos, goarch, _ := strings.Cut(p, "/")
		a, err := ArtifactFor(goos, goarch)
		if err != nil {
			t.Fatal(err)
		}
		if !hex64.MatchString(a.ArchiveSHA256) || !hex64.MatchString(a.BinarySHA256) || a.Archive == "" {
			t.Errorf("%s: malformed pin %+v", p, a)
		}
		if !have[a.Archive+" "+a.ArchiveSHA256] {
			t.Errorf("%s: the archive %s with SHA-256 %s is not in what upstream published", p, a.Archive, a.ArchiveSHA256)
		}
		if !have[a.Archive+"/nebula "+a.BinarySHA256] {
			t.Errorf("%s: the program in %s with SHA-256 %s is not in what upstream published", p, a.Archive, a.BinarySHA256)
		}
	}
	if _, err := ArtifactFor("windows", "amd64"); !errors.As(err, new(ErrUnsupportedPlatform)) {
		t.Errorf("windows: %v", err)
	}
	if !strings.HasPrefix(ReleaseURL, "https://github.com/slackhq/nebula/releases/download/v") || !strings.HasSuffix(ReleaseURL, Version+"/") {
		t.Errorf("ReleaseURL = %s", ReleaseURL)
	}
}

func TestTheLicensesOfWhatIsShippedAreInTheRepository(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "third_party")
	lic, err := os.ReadFile(filepath.Join(dir, "nebula", "LICENSE"))
	if err != nil || !strings.Contains(string(lic), "MIT License") || !strings.Contains(string(lic), "Slack Technologies") {
		t.Errorf("third_party/nebula/LICENSE: %v", err)
	}
	notices, err := os.ReadFile(filepath.Join(dir, "nebula", "THIRD_PARTY_LICENSES.txt"))
	if err != nil || !strings.Contains(string(notices), "Nebula v"+Version) || strings.Count(string(notices), "\nModule: ") < 20 {
		t.Errorf("third_party/nebula/THIRD_PARTY_LICENSES.txt is missing or is for another version: %v", err)
	}
	if strings.Contains(string(notices), "ships no licence file") {
		t.Error("a module in the bundled program has no licence file recorded: look at it before shipping")
	}
	if _, err := os.Stat(filepath.Join(dir, "go", "LICENSE")); err != nil {
		t.Error(err)
	}
}

// ---- finding and verifying the program ----

func standIn(t *testing.T, script string) (dir string, art Artifact) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in programs are shell scripts")
	}
	dir = privateDir(t)
	p := filepath.Join(dir, "nebula")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum, _ := sha256File(p)
	return dir, Artifact{Archive: "stand-in", ArchiveSHA256: sum, BinarySHA256: sum}
}

func TestOnlyTheProgramThatMatchesThePinIsStarted(t *testing.T) {
	marker := filepath.Join(privateDir(t), "ran")
	_, art := standIn(t, "echo ran > "+marker)
	wrongDir, _ := standIn(t, "echo ran > "+marker+"; echo different")
	if _, err := locate([]string{wrongDir}, filepath.Join(privateDir(t), "bin"), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Errorf("a program that is not the pinned one: %v", err)
	}
	if _, err := locate(nil, filepath.Join(privateDir(t), "bin"), art); !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("no program: %v", err)
	}
	if _, err := locate([]string{"relative/dir"}, filepath.Join(privateDir(t), "bin"), art); !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("a relative directory was searched: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a program that was not verified was run")
	}
}

func TestThePathIsNeverSearched(t *testing.T) {
	marker := filepath.Join(privateDir(t), "ran")
	dirOnPath, art := standIn(t, "echo ran > "+marker)
	t.Setenv("PATH", dirOnPath+string(os.PathListSeparator)+os.Getenv("PATH"))
	s, err := New(Options{artifact: &art})
	if err != nil {
		t.Fatal(err)
	}
	n := newNetwork(t)
	nd := n.issue("a", pki.GroupMember)
	err = s.StartNebula(t.Context(), n.config(nd, nil))
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("StartNebula = %v, want ErrBinaryNotFound (a program on PATH matches the pin and must still not be used)", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the program on PATH was run")
	}
	if st := s.Status(); st.State != StateFailed {
		t.Errorf("state = %s", st.State)
	}
}

func TestASymbolicLinkIsNotFollowed(t *testing.T) {
	dir, art := standIn(t, "echo hi")
	link := privateDir(t)
	if err := os.Symlink(filepath.Join(dir, "nebula"), filepath.Join(link, "nebula")); err != nil {
		t.Skip(err)
	}
	if _, err := locate([]string{link}, filepath.Join(privateDir(t), "bin"), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Errorf("a symlink: %v", err)
	}
}

func TestACopyChangedAfterVerificationIsNotStarted(t *testing.T) {
	dir, art := standIn(t, "echo hi")
	v, err := locate([]string{dir}, filepath.Join(privateDir(t), "bin"), art)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.reverify(art); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(v.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v.Path, []byte("#!/bin/sh\necho evil\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.reverify(art); !errors.Is(err, ErrBinaryMismatch) {
		t.Errorf("reverify after a change = %v", err)
	}
	// A new locate repairs the copy from the verified original.
	v2, err := locate([]string{dir}, filepath.Join(filepath.Dir(v.Path)), art)
	if err != nil {
		t.Fatal(err)
	}
	if err := v2.reverify(art); err != nil {
		t.Errorf("the copy was not repaired: %v", err)
	}
}

func TestADirectoryOthersCanWriteIsRefused(t *testing.T) {
	dir, art := standIn(t, "echo hi")
	loose := filepath.Join(privateDir(t), "bin")
	if err := os.Mkdir(loose, 0o777); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(loose, 0o777)
	if _, err := locate([]string{dir}, loose, art); err == nil || !strings.Contains(err.Error(), "writable by others") {
		t.Errorf("a world-writable directory for the program: %v", err)
	}
}

// ---- running it ----

func supervisor(t *testing.T, script string, mutate func(*Options)) (*Supervisor, NebulaConfig, string) {
	t.Helper()
	dir, art := standIn(t, script)
	opts := Options{BinaryDirs: []string{dir}, artifact: &art, grace: 300 * time.Millisecond, healthyFor: 400 * time.Millisecond, backoff: 50 * time.Millisecond}
	if mutate != nil {
		mutate(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(t.Context()) })
	n := newNetwork(t)
	cfg := n.config(n.issue("a", pki.GroupMember), nil)
	return s, cfg, cfg.DataDir
}

func TestTheProgramGetsOnlyTheArgumentsTheSupervisorBuilds(t *testing.T) {
	s, cfg, data := supervisor(t, `printf '%s\n' "$@" > args.txt; env > env.txt; pwd > pwd.txt; exec sleep 30`, nil)
	if err := s.StartNebula(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the stand-in to record itself", 5*time.Second, func() bool { b, err := os.ReadFile(filepath.Join(data, "pwd.txt")); return err == nil && len(b) > 0 })
	args, _ := os.ReadFile(filepath.Join(data, "args.txt"))
	if string(args) != "-config\n"+filepath.Join(data, "config.yml")+"\n" {
		t.Errorf("arguments = %q", args)
	}
	env, _ := os.ReadFile(filepath.Join(data, "env.txt"))
	for _, v := range strings.Split(strings.TrimSpace(string(env)), "\n") {
		name, _, _ := strings.Cut(v, "=")
		switch name {
		case "", "PWD", "SHLVL", "_", "OLDPWD":
		default:
			t.Errorf("the program inherited %s: it gets no environment", name)
		}
	}
	if pwd, _ := os.ReadFile(filepath.Join(data, "pwd.txt")); strings.TrimSpace(string(pwd)) != data {
		t.Errorf("working directory = %q", pwd)
	}
	st := s.Status()
	if st.State != StateRunning || st.PID == 0 || st.Version != Version {
		t.Errorf("status = %+v", st)
	}
	for _, f := range []string{"ca.crt", "node.crt", "node.key", "config.yml"} {
		if fi, err := os.Stat(filepath.Join(data, f)); err != nil || fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: %v %v: the node's files are owner-only", f, fi, err)
		}
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.State != StateStopped || st.PID != 0 {
		t.Errorf("after Stop: %+v", st)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Errorf("stopping a stopped supervisor: %v", err)
	}
}

func TestAProgramThatCannotStartIsReportedWithAReasonAndNotRetried(t *testing.T) {
	// A generous grace: the first run of a new program can be slow on a Mac that scans it.
	s, cfg, _ := supervisor(t, `echo '{"msg":"failed to open tun: operation not permitted"}'; exit 1`, func(o *Options) { o.grace = 3 * time.Second })
	err := s.StartNebula(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), "docs/TEAM_NETWORK.md") || !strings.Contains(err.Error(), "privileges") {
		t.Fatalf("StartNebula = %v, want advice about privileges", err)
	}
	time.Sleep(400 * time.Millisecond)
	if st := s.Status(); st.State != StateFailed || st.Restarts != 0 {
		t.Errorf("status = %+v: a program that cannot start must not be restarted in a loop", st)
	}
	if err := s.StartNebula(t.Context(), cfg); err == nil {
		t.Error("a second start of a failing program succeeded")
	}
}

func TestAProgramThatDiesLaterIsRestarted(t *testing.T) {
	s, cfg, data := supervisor(t, `echo run >> runs.txt; sleep 0.7; exit 3`, nil)
	if err := s.StartNebula(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two restarts", 8*time.Second, func() bool { return s.Status().Restarts >= 2 })
	b, _ := os.ReadFile(filepath.Join(data, "runs.txt"))
	if n := strings.Count(string(b), "run"); n < 2 {
		t.Errorf("the program ran %d times", n)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := s.Status().Restarts
	time.Sleep(600 * time.Millisecond)
	if after := s.Status().Restarts; after != before {
		t.Errorf("restarts continued after Stop: %d -> %d", before, after)
	}
}

func TestStoppingEndsTheProcess(t *testing.T) {
	s, cfg, _ := supervisor(t, `exec sleep 60`, nil)
	if err := s.StartNebula(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	pid := s.Status().PID
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the process to end", 5*time.Second, func() bool { return syscall.Kill(pid, 0) != nil })
}

func TestStartingTwiceIsRefused(t *testing.T) {
	s, cfg, _ := supervisor(t, `exec sleep 60`, nil)
	if err := s.StartNebula(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.StartNebula(t.Context(), cfg); err == nil || !strings.Contains(err.Error(), "already started") {
		t.Errorf("second StartNebula = %v", err)
	}
}

// ---- what the configuration must be to be started at all ----

func TestAConfigurationThatDoesNotHangTogetherIsNotStarted(t *testing.T) {
	n := newNetwork(t)
	good := n.issue("a", pki.GroupMember)
	base := func() NebulaConfig { return n.config(good, nil) }
	other := newNetwork(t)
	foreign := other.issue("x", pki.GroupMember)
	_, wrongKeyPEM, _ := pki.GenerateNodeKey()
	privOther, _, _ := pki.GenerateNodeKey()
	_ = wrongKeyPEM
	expiredNet := newNetworkAt(t, time.Now().Add(-90*24*time.Hour)) // its certificates last 30 days
	old := expiredNet.issue("old", pki.GroupMember)

	for name, edit := range map[string]func(*NebulaConfig){
		"a relative data directory":                func(c *NebulaConfig) { c.DataDir = "relative/dir" },
		"an unclean data directory":                func(c *NebulaConfig) { c.DataDir += "/../x" },
		"no data directory":                        func(c *NebulaConfig) { c.DataDir = "" },
		"a quote in the data directory":            func(c *NebulaConfig) { c.DataDir += `/a"b` },
		"a certificate from another authority":     func(c *NebulaConfig) { c.NodeCertPEM = foreign.certPEM },
		"a key for another certificate":            func(c *NebulaConfig) { c.NodeKeyPEM = privOther },
		"an expired certificate":                   func(c *NebulaConfig) { c.NodeCertPEM, c.CACertPEM = old.certPEM, expiredNet.caPEM },
		"an authority that is a node":              func(c *NebulaConfig) { c.CACertPEM = good.certPEM },
		"no certificate":                           func(c *NebulaConfig) { c.NodeCertPEM = nil },
		"an address that is not the certificate's": func(c *NebulaConfig) { c.Node.Addr = netip.MustParseAddr("10.77.0.200") },
		"a rule for a group that does not exist": func(c *NebulaConfig) {
			c.Node.Policy.Inbound = append(c.Node.Policy.Inbound, overlay.Rule{Port: "22", Proto: "tcp", Group: "root"})
		},
	} {
		cfg := base()
		edit(&cfg)
		if err := Validate(cfg, time.Now()); err == nil {
			t.Errorf("%s: Validate passed", name)
		}
	}
	if err := Validate(base(), time.Now()); err != nil {
		t.Errorf("a good configuration: %v", err)
	}
}
