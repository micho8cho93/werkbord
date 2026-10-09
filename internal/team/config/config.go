// Package config holds Werkbord Team's settings. Precedence, lowest to highest:
// built-in defaults, WERKBORD_TEAM_* environment variables, then command-line flags
// (applied by the caller). Team has its own settings, data directory and
// variables so that it and the individual product can run side by side on one
// computer without sharing anything.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"devboard/internal/httpkit"
)

// DefaultAddr listens on this computer only. A team server is reached by other
// people, so the person running it chooses a wider address on purpose; see
// docs/TEAM.md for putting it behind HTTPS.
const DefaultAddr = "127.0.0.1:7430"

// Defaults of the private network.
const (
	// DefaultBootstrapAddr is where a Workspace Host answers new devices that are
	// joining the workspace, over TLS, with the workspace's own identity.
	DefaultBootstrapAddr = "0.0.0.0:7440"
	// DefaultNetworkPort is the UDP port the private network's node listens on.
	DefaultNetworkPort = 4242
)

// Defaults of the replicated database.
const (
	// DefaultStorageHTTPPort and DefaultStorageRaftPort are where a Workspace Host's database node listens, on
	// loopback or on the workspace's private network. They are the ports the network's policy lets Workspace
	// Hosts reach one another on.
	DefaultStorageHTTPPort = 4001
	DefaultStorageRaftPort = 4002
	// DefaultBackupEvery is how often a Workspace Host takes a backup, when backups are configured.
	DefaultBackupEvery = 24 * time.Hour
	// DefaultBackupKeep is how many of the newest backups are always kept.
	DefaultBackupKeep = 14
	// DefaultBackupKeepFor is how long a backup is kept beyond that.
	DefaultBackupKeepFor = 30 * 24 * time.Hour
)

// The kinds of storage: where a workspace's data is.
const (
	// StorageReplicated holds the data in a cluster of Workspace Hosts (rqlite). It is what a workspace has
	// unless it is told otherwise.
	StorageReplicated = "replicated"
	// StorageSingleFile holds the data in one SQLite file on one host, as Team did before replication. It is
	// valid for evaluation and for a workspace that has not been moved yet; it has no copy but its backups.
	StorageSingleFile = "single-file"
)

// Config holds Team's settings.
type Config struct {
	// Production commands require an offline license; the key is supplied only by the executable's build.
	LicenseRequired bool
	LicenseKey      []byte
	LicenseFile     string
	KeyStorage      string
	// SecureStorageID stays stable while the daemon atomically stages a new workspace directory.
	SecureStorageID string
	Addr            string
	DataDir         string
	LogLevel        string // debug, info, warn, error
	LogFormat       string // text or json
	ShutdownTimeout time.Duration

	// The customer-owned private network. Every setting here names a machine the
	// customer owns or a file on this one: none is a service, an account, a URL or a
	// key from anyone else (internal/archtest holds that line).
	//
	// BootstrapAddr is where this host answers devices that are joining; Endpoints are
	// the hosts (addresses or DNS names) at which this machine can be reached from outside
	// its own network, if it can, which the invitations and the network's discovery
	// advertise; NetworkPort is the UDP port the network node listens on. RunNode says
	// whether this host runs the network node itself (it needs the privileges to create a
	// network interface); NebulaDirs are extra directories the pinned program may be in.
	// PassphraseFile, if set, names a file holding the passphrase the workspace's keys are
	// sealed with, instead of a key kept beside the data.
	BootstrapAddr  string
	Endpoints      []string
	NetworkPort    int
	RunNode        bool
	NebulaDirs     []string
	PassphraseFile string

	// Storage says where a workspace that is being created keeps its data: StorageReplicated (the default) or
	// StorageSingleFile. A workspace that already exists keeps it where it is (storage.json in the data
	// directory says where), whatever this says.
	Storage string
	// StoragePort and StorageRaftPort are the ports of this host's database node.
	StoragePort     int
	StorageRaftPort int
	// DatabaseDirs are extra directories the pinned database program may be in.
	DatabaseDirs []string
	// BackupDir is where this host puts the workspace's backups: a directory on a disk or a network share
	// that belongs to the customer. Empty means no scheduled backups (a manual one needs a directory too).
	// BackupEvery is how often (0 turns the schedule off); the newest BackupKeep are always kept, and any younger
	// than BackupKeepFor.
	BackupDir string
	// EmbedOrigins are extra exact loopback origins that may show the device service's pages in a frame, besides the
	// Werkbord desktop app's window. For running the desktop shell from source and testing it in a browser.
	EmbedOrigins  []string
	BackupEvery   time.Duration
	BackupKeep    int
	BackupKeepFor time.Duration
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{Addr: DefaultAddr, DataDir: defaultDataDir(), KeyStorage: "file", LogLevel: "info", LogFormat: "text", ShutdownTimeout: 10 * time.Second,
		BootstrapAddr: DefaultBootstrapAddr, NetworkPort: DefaultNetworkPort, RunNode: true,
		Storage: StorageReplicated, StoragePort: DefaultStorageHTTPPort, StorageRaftPort: DefaultStorageRaftPort,
		BackupEvery: DefaultBackupEvery, BackupKeep: DefaultBackupKeep, BackupKeepFor: DefaultBackupKeepFor}
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "werkbord-team")
	}
	return ".werkbord-team"
}

// Load returns the defaults overlaid with the environment.
func Load() Config {
	c := Default()
	c.LicenseRequired = true
	c.KeyStorage = "os"
	set := func(key string, dst *string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	set("WERKBORD_TEAM_ADDR", &c.Addr)
	set("WERKBORD_TEAM_LICENSE_FILE", &c.LicenseFile)
	set("WERKBORD_TEAM_KEY_STORAGE", &c.KeyStorage)
	set("WERKBORD_TEAM_DATA_DIR", &c.DataDir)
	set("WERKBORD_TEAM_LOG_LEVEL", &c.LogLevel)
	set("WERKBORD_TEAM_LOG_FORMAT", &c.LogFormat)
	set("WERKBORD_TEAM_BOOTSTRAP_ADDR", &c.BootstrapAddr)
	set("WERKBORD_TEAM_PKI_PASSPHRASE_FILE", &c.PassphraseFile)
	if v := os.Getenv("WERKBORD_TEAM_ENDPOINTS"); v != "" {
		c.Endpoints = splitList(v)
	}
	if v := os.Getenv("WERKBORD_TEAM_NEBULA_DIR"); v != "" {
		c.NebulaDirs = splitList(v)
	}
	if v := os.Getenv("WERKBORD_TEAM_NETWORK_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.NetworkPort = n
		} else {
			c.NetworkPort = -1 // Validate reports it
		}
	}
	if v := os.Getenv("WERKBORD_TEAM_NETWORK_NODE"); v == "off" || v == "0" || v == "false" {
		c.RunNode = false
	}
	set("WERKBORD_TEAM_STORAGE", &c.Storage)
	set("WERKBORD_TEAM_BACKUP_DIR", &c.BackupDir)
	if v := os.Getenv("WERKBORD_TEAM_EMBED_ORIGINS"); v != "" {
		c.EmbedOrigins = splitList(v)
	}
	if v := os.Getenv("WERKBORD_TEAM_DATABASE_DIR"); v != "" {
		c.DatabaseDirs = splitList(v)
	}
	num := func(key string, dst *int) {
		if v := os.Getenv(key); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			} else {
				*dst = -1 // Validate reports it
			}
		}
	}
	num("WERKBORD_TEAM_STORAGE_PORT", &c.StoragePort)
	num("WERKBORD_TEAM_STORAGE_RAFT_PORT", &c.StorageRaftPort)
	num("WERKBORD_TEAM_BACKUP_KEEP", &c.BackupKeep)
	if v := os.Getenv("WERKBORD_TEAM_BACKUP_EVERY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.BackupEvery = d
		} else {
			c.BackupEvery = -1
		}
	}
	if v := os.Getenv("WERKBORD_TEAM_BACKUP_KEEP_FOR"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.BackupKeepFor = d
		} else {
			c.BackupKeepFor = -1
		}
	}
	return c
}

func splitList(v string) []string {
	var out []string
	for _, f := range strings.Split(v, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Validate checks the settings for mistakes.
func (c Config) Validate() error {
	switch c.KeyStorage {
	case "", "file", "os":
	default:
		return fmt.Errorf("key storage must be os or file")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return fmt.Errorf("addr %q: %w", c.Addr, err)
	}
	if c.LicenseRequired && !c.IsLoopback() {
		return fmt.Errorf("the local API must listen on loopback; other devices use the separately bound Nebula API")
	}
	if c.DataDir == "" {
		return fmt.Errorf("the data directory is required")
	}
	if len(c.EmbedOrigins) > 0 {
		if _, err := httpkit.ParseEmbedOrigins(strings.Join(c.EmbedOrigins, ",")); err != nil {
			return fmt.Errorf("embed origins: %w", err)
		}
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("log format %q: want text or json", c.LogFormat)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("the shutdown timeout must be positive")
	}
	if _, _, err := net.SplitHostPort(c.BootstrapAddr); err != nil {
		return fmt.Errorf("bootstrap addr %q: %w", c.BootstrapAddr, err)
	}
	if c.NetworkPort < 1 || c.NetworkPort > 65535 {
		return fmt.Errorf("network port %d: want 1-65535", c.NetworkPort)
	}
	switch c.Storage {
	case StorageReplicated, StorageSingleFile:
	default:
		return fmt.Errorf("storage %q: want %s or %s", c.Storage, StorageReplicated, StorageSingleFile)
	}
	for name, p := range map[string]int{"storage port": c.StoragePort, "storage raft port": c.StorageRaftPort} {
		if p < 1 || p > 65535 {
			return fmt.Errorf("%s %d: want 1-65535", name, p)
		}
	}
	if c.StoragePort == c.StorageRaftPort {
		return fmt.Errorf("the storage port and the storage raft port must differ")
	}
	if c.BackupEvery < 0 || c.BackupKeepFor < 0 || c.BackupKeep < 1 {
		return fmt.Errorf("backup settings: the interval and the age limit cannot be negative, and at least one backup is kept")
	}
	if c.BackupDir != "" && !filepath.IsAbs(c.BackupDir) {
		return fmt.Errorf("backup directory %q: give an absolute path", c.BackupDir)
	}
	for _, e := range c.Endpoints {
		if e == "" || strings.ContainsAny(e, " \t\r\n/\\\"") || strings.Contains(e, "://") || (strings.Contains(e, ":") && net.ParseIP(strings.Trim(e, "[]")) == nil) {
			return fmt.Errorf("endpoint %q: give a host (an IP address or a DNS name) at which this machine can be reached, without a scheme or a port", e)
		}
	}
	return nil
}

// PKIDir is where this host keeps the workspace's keys, sealed (internal/team/infra/pki).
// It is never replicated with the database.
func (c Config) PKIDir() string { return filepath.Join(c.DataDir, "pki") }

// SealingKeyPath is the key the workspace's keys are sealed with, unless a passphrase is
// used. It is kept apart from the sealed files.
func (c Config) SealingKeyPath() string { return filepath.Join(c.DataDir, "secrets", "sealing.key") }

// NodeDir is the directory the network node's own files live in.
func (c Config) NodeDir() string { return filepath.Join(c.DataDir, "network") }

// BootstrapPort is the port of BootstrapAddr.
func (c Config) BootstrapPort() int {
	_, p, err := net.SplitHostPort(c.BootstrapAddr)
	n, _ := strconv.Atoi(p)
	if err != nil {
		return 0
	}
	return n
}

// DBPath is the SQLite database of a workspace that keeps its data in one file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "team.db") }

func (c Config) LicensePath() string {
	if c.LicenseFile != "" {
		return c.LicenseFile
	}
	return filepath.Join(c.DataDir, "license.json")
}

// StorageMarkerPath is the file that says where this host keeps the workspace's data.
func (c Config) StorageMarkerPath() string { return filepath.Join(c.DataDir, "storage.json") }

// StorageDir is where this host's database node keeps its files and this host's copy of the data.
func (c Config) StorageDir() string { return filepath.Join(c.DataDir, "storage") }

// ReplicaDir is where this host's copy of the replicated data is.
func (c Config) ReplicaDir() string { return filepath.Join(c.StorageDir(), "replica") }

// IsLoopback reports whether Addr only listens on this computer.
func (c Config) IsLoopback() bool {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ClientAddr is the address to open in a browser on this computer.
func (c Config) ClientAddr() string {
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return c.Addr
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
