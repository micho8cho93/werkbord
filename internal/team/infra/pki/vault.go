package pki

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"devboard/internal/enrollment"
)

// File names in a vault directory.
const (
	fileMeta     = "workspace.json"
	fileTrust    = "workspace.key.sealed"
	fileCACert   = "network-ca.crt"
	fileCAKey    = "network-ca.key.sealed"
	fileHostKeys = "host.keys.sealed"

	fileStorage  = "storage.sealed"
	labelStorage = "werkbord/vault/storage"

	fileToken = "device.token.sealed"
	fileJoin  = "join.json"

	labelToken = "werkbord/vault/device-token"
	labelTrust = "werkbord/vault/workspace-key"
	labelCA    = "werkbord/vault/network-ca-key"
	labelHost  = "werkbord/vault/host-keys"
)

// Meta is the public description of the workspace's PKI on this host. Nothing in it is secret.
type Meta struct {
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceName string `json:"workspaceName"`
	// Fingerprint is the workspace's public fingerprint.
	Fingerprint string `json:"fingerprint"`
	// NetworkPrefix is the private network's address range.
	NetworkPrefix string `json:"networkPrefix"`
	// Authority says whether this host holds the workspace key and the network
	// authority's key, that is, whether it is a Workspace Host.
	Authority bool `json:"authority"`
	// HostDeviceID is this host's own device ID.
	HostDeviceID string `json:"hostDeviceId"`
}

// Material is a vault's contents, opened.
type Material struct {
	Meta Meta
	// Trust and CA are nil on a host that does not hold the authority.
	Trust *Trust
	CA    *NetworkCA
	Host  *HostKeys
}

// Vault keeps a host's key material on disk in one directory: owner-only, secrets
// sealed (Sealer), public parts in clear. It is a host's own, and is never replicated
// with the workspace's data: the database may be copied to another machine, and the
// vault never travels with it.
type Vault struct {
	dir    string
	sealer Sealer
}

// OpenVault opens (creating if need be) the vault in dir, which is made 0700.
func OpenVault(dir string, s Sealer) (*Vault, error) {
	if s == nil {
		return nil, errors.New("pki: a vault needs a Sealer")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &Vault{dir: dir, sealer: s}, nil
}

// Dir is the vault's directory.
func (v *Vault) Dir() string { return v.dir }

// Exists reports whether the vault holds a workspace's PKI.
func (v *Vault) Exists() bool {
	_, err := os.Stat(filepath.Join(v.dir, fileMeta))
	return err == nil
}

// Create bootstraps a workspace's PKI on this host, the first Workspace Host: the
// workspace's trust identity, the network authority, and the host's own keys. It
// refuses to overwrite an existing one.
func (v *Vault) Create(workspaceID, workspaceName string, prefix netip.Prefix, now time.Time) (*Material, error) {
	host, err := NewHostKeys()
	if err != nil {
		return nil, err
	}
	return v.CreateWith(host, workspaceID, workspaceName, prefix, now)
}

// CreateWith is Create for a host whose keys were made beforehand (the workspace's database names its first node
// after the host's device ID, which therefore has to exist before the workspace does).
func (v *Vault) CreateWith(host *HostKeys, workspaceID, workspaceName string, prefix netip.Prefix, now time.Time) (*Material, error) {
	if v.Exists() {
		return nil, errors.New("pki: this host already holds a workspace's keys")
	}
	trust, err := NewTrust(workspaceID, workspaceName, now)
	if err != nil {
		return nil, err
	}
	ca, err := NewNetworkCA(workspaceName, prefix, now)
	if err != nil {
		return nil, err
	}
	m := &Material{
		Meta:  Meta{WorkspaceID: workspaceID, WorkspaceName: workspaceName, Fingerprint: trust.Fingerprint(), NetworkPrefix: prefix.String(), Authority: true, HostDeviceID: host.DeviceID()},
		Trust: trust, CA: ca, Host: host,
	}
	return m, v.save(m)
}

// CreateJoined stores what a host that has enrolled (but is not yet a Workspace Host)
// holds: its own keys and what it learned, and no authority.
func (v *Vault) CreateJoined(meta Meta, host *HostKeys, caCertPEM []byte) error {
	if v.Exists() {
		return errors.New("pki: this host already holds a workspace's keys")
	}
	meta.Authority, meta.HostDeviceID = false, host.DeviceID()
	return v.save(&Material{Meta: meta, Host: host}, withCACert(caCertPEM))
}

type saveOpt func(*saveOpts)
type saveOpts struct{ caCert []byte }

func withCACert(b []byte) saveOpt { return func(o *saveOpts) { o.caCert = b } }

func (v *Vault) save(m *Material, opts ...saveOpt) error {
	var o saveOpts
	for _, f := range opts {
		f(&o)
	}
	hk, err := m.Host.marshal()
	if err != nil {
		return err
	}
	sealedHost, err := v.sealer.Seal(labelHost, hk)
	if err != nil {
		return err
	}
	files := map[string][]byte{fileHostKeys: sealedHost}
	if m.Trust != nil {
		if files[fileTrust], err = v.sealer.Seal(labelTrust, m.Trust.Seed()); err != nil {
			return err
		}
	}
	if m.CA != nil {
		if files[fileCAKey], err = v.sealer.Seal(labelCA, m.CA.PrivateKeyPEM()); err != nil {
			return err
		}
		if files[fileCACert], err = m.CA.CertificatePEM(); err != nil {
			return err
		}
	} else if o.caCert != nil {
		files[fileCACert] = o.caCert
	}
	meta, err := json.MarshalIndent(m.Meta, "", "  ")
	if err != nil {
		return err
	}
	// The secrets first and the metadata last: Exists means everything else is there.
	for name, data := range files {
		if err := writeFile(filepath.Join(v.dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return writeFile(filepath.Join(v.dir, fileMeta), append(meta, '\n'), 0o600)
}

// Load opens the vault. now is only used to rebuild the workspace's root certificate.
func (v *Vault) Load(now time.Time) (*Material, error) {
	b, err := os.ReadFile(filepath.Join(v.dir, fileMeta))
	if err != nil {
		return nil, err
	}
	var m Material
	if err := json.Unmarshal(b, &m.Meta); err != nil {
		return nil, fmt.Errorf("pki: %s: %w", fileMeta, err)
	}
	read := func(name, label string) ([]byte, error) {
		path := filepath.Join(v.dir, name)
		if err := checkPrivateFile(path); err != nil {
			return nil, err
		}
		sealed, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return v.sealer.Open(label, sealed)
	}
	hk, err := read(fileHostKeys, labelHost)
	if err != nil {
		return nil, fmt.Errorf("pki: the host's keys: %w", err)
	}
	if m.Host, err = unmarshalHostKeys(hk); err != nil {
		return nil, err
	}
	if m.Meta.Authority {
		seed, err := read(fileTrust, labelTrust)
		if err != nil {
			return nil, fmt.Errorf("pki: the workspace key: %w", err)
		}
		if m.Trust, err = TrustFromSeed(m.Meta.WorkspaceID, m.Meta.WorkspaceName, seed, now); err != nil {
			return nil, err
		}
		if !enrollment.SameFingerprint(m.Trust.Fingerprint(), m.Meta.Fingerprint) {
			return nil, errors.New("pki: the workspace key does not match its recorded fingerprint")
		}
		keyPEM, err := read(fileCAKey, labelCA)
		if err != nil {
			return nil, fmt.Errorf("pki: the network authority's key: %w", err)
		}
		certPEM, err := os.ReadFile(filepath.Join(v.dir, fileCACert))
		if err != nil {
			return nil, err
		}
		if m.CA, err = NetworkCAFromPEM(certPEM, keyPEM); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// CACertificate returns the network authority's certificate, which every host has.
func (v *Vault) CACertificate() ([]byte, error) {
	return os.ReadFile(filepath.Join(v.dir, fileCACert))
}

// Promote stores the authority's secrets, received from another Workspace Host, so
// this host becomes one too. The caller has already opened and checked them (OpenSecrets).
func (v *Vault) Promote(s Secrets, now time.Time) error {
	cur, err := v.Load(now)
	if err != nil {
		return err
	}
	if cur.Meta.WorkspaceID != s.WorkspaceID {
		return errors.New("pki: these secrets are for another workspace")
	}
	trust, err := TrustFromSeed(s.WorkspaceID, s.WorkspaceName, s.TrustSeed, now)
	if err != nil {
		return err
	}
	if !enrollment.SameFingerprint(trust.Fingerprint(), cur.Meta.Fingerprint) {
		return errors.New("pki: the workspace key is not the one this host enrolled with")
	}
	ca, err := NetworkCAFromPEM(s.NetworkCACertificate, s.NetworkCAKey)
	if err != nil {
		return err
	}
	cur.Trust, cur.CA, cur.Meta.Authority = trust, ca, true
	if err := v.save(cur); err != nil {
		return err
	}
	if s.Storage != nil {
		return v.SaveStorage(*s.Storage)
	}
	return nil
}

// SaveStorage keeps, sealed, what this host needs to take part in the workspace's replicated database. It works
// on a host that has no other keys yet (a workspace on one host, with no private network, still has a database).
func (v *Vault) SaveStorage(s StorageSecrets) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	sealed, err := v.sealer.Seal(labelStorage, b)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(v.dir, fileStorage), sealed, 0o600)
}

// HasStorage reports whether the vault holds storage secrets.
func (v *Vault) HasStorage() bool {
	_, err := os.Stat(filepath.Join(v.dir, fileStorage))
	return err == nil
}

// Storage returns them.
func (v *Vault) Storage() (StorageSecrets, error) {
	var s StorageSecrets
	path := filepath.Join(v.dir, fileStorage)
	if err := checkPrivateFile(path); err != nil {
		return s, err
	}
	sealed, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	b, err := v.sealer.Open(labelStorage, sealed)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// Demote removes the authority's secrets from this host (it is no longer a Workspace
// Host). The sealed files are overwritten before they are removed; that is
// best-effort on a copy-on-write disk, which is why a host that is removed for cause
// means rotating the authority (docs/TEAM_NETWORK.md).
func (v *Vault) Demote(now time.Time) error {
	cur, err := v.Load(now)
	if err != nil {
		return err
	}
	cur.Meta.Authority = false
	cur.Trust, cur.CA = nil, nil
	// Persist the reduced role first; interrupted erasure can resume without needing the removed keys.
	if err := v.save(cur); err != nil {
		return err
	}
	for _, name := range []string{fileTrust, fileCAKey, fileStorage} {
		path := filepath.Join(v.dir, name)
		if fi, err := os.Stat(path); err == nil {
			_ = os.WriteFile(path, make([]byte, fi.Size()), 0o600)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	ok = true
	return nil
}

// SaveDeviceToken keeps, sealed, the credential this host was given for the workspace's
// API when it enrolled.
func (v *Vault) SaveDeviceToken(token string) error {
	sealed, err := v.sealer.Seal(labelToken, []byte(token))
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(v.dir, fileToken), sealed, 0o600)
}

// DeviceToken returns it.
func (v *Vault) DeviceToken() (string, error) {
	path := filepath.Join(v.dir, fileToken)
	if err := checkPrivateFile(path); err != nil {
		return "", err
	}
	sealed, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	b, err := v.sealer.Open(labelToken, sealed)
	return string(b), err
}

// JoinInfo is what a host that enrolled learned about the workspace, which is public.
type JoinInfo struct {
	// Endpoints are the bootstrap endpoints the invitation named.
	Endpoints []string `json:"endpoints"`
	// APIAddrs are the private-network addresses of the Workspace Hosts' API.
	APIAddrs []string `json:"apiAddrs"`
	// APIPort is the port the API is served on there.
	APIPort int `json:"apiPort"`
	// Node is what the workspace last said this host's place on the network is, in its own terms
	// (opaque here), so the host can bring its node up before it can ask.
	Node json.RawMessage `json:"node,omitempty"`
}

// SaveJoinInfo records it.
func (v *Vault) SaveJoinInfo(j JoinInfo) error {
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(v.dir, fileJoin), append(b, '\n'), 0o600)
}

// JoinInfo returns it.
func (v *Vault) JoinInfo() (JoinInfo, error) {
	var j JoinInfo
	b, err := os.ReadFile(filepath.Join(v.dir, fileJoin))
	if err != nil {
		return j, err
	}
	return j, json.Unmarshal(b, &j)
}

// ---- named secrets ----

const labelSecretPrefix = "werkbord/vault/secret/"

func secretFile(name string) string { return "secret." + name + ".sealed" }

func checkSecretName(name string) error {
	if name == "" || len(name) > 40 {
		return errors.New("pki: a secret's name is 1 to 40 characters")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return fmt.Errorf("pki: %q is not a secret's name (lower case letters, digits and hyphens)", name)
		}
	}
	return nil
}

// SaveSecret keeps a named secret of this device, sealed, beside its keys: a credential it holds for something on this
// computer, or a join that is waiting to be approved. The vault does not interpret it.
func (v *Vault) SaveSecret(name string, data []byte) error {
	if err := checkSecretName(name); err != nil {
		return err
	}
	sealed, err := v.sealer.Seal(labelSecretPrefix+name, data)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(v.dir, secretFile(name)), sealed, 0o600)
}

// Secret returns a named secret; os.ErrNotExist when there is none.
func (v *Vault) Secret(name string) ([]byte, error) {
	if err := checkSecretName(name); err != nil {
		return nil, err
	}
	path := filepath.Join(v.dir, secretFile(name))
	if err := checkPrivateFile(path); err != nil {
		return nil, err
	}
	sealed, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return v.sealer.Open(labelSecretPrefix+name, sealed)
}

// HasSecret reports whether there is one.
func (v *Vault) HasSecret(name string) bool {
	if checkSecretName(name) != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(v.dir, secretFile(name)))
	return err == nil
}

// DeleteSecret removes one (overwriting it first, as far as the disk allows). Removing what is not there is not an error.
func (v *Vault) DeleteSecret(name string) error {
	if err := checkSecretName(name); err != nil {
		return err
	}
	path := filepath.Join(v.dir, secretFile(name))
	if fi, err := os.Stat(path); err == nil {
		_ = os.WriteFile(path, make([]byte, fi.Size()), 0o600)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
