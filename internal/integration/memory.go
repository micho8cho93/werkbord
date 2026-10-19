package integration

// Memory is a separate protocol. It never extends the integration execution grant.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MemorySchema = "werkbord.memory/v1"
const MaxMemoryBatch = 50

// MemoryTarget binds cross-repository knowledge to every constituent project.
type MemoryTarget struct {
	ProjectID  string `json:"projectId"`
	Repository string `json:"repository"`
}
type SharedMemoryScope struct {
	WorkspaceID string         `json:"workspaceId"`
	Kind        string         `json:"kind"`
	ProjectID   string         `json:"projectId,omitempty"`
	Repository  string         `json:"repository,omitempty"`
	Targets     []MemoryTarget `json:"targets,omitempty"`
}

// MemoryRepository accepts a remote or its canonical identity, never a local path.
func MemoryRepository(raw string) (string, error) {
	if id, err := RepositoryIdentity(raw); err == nil {
		return id, nil
	}
	if !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "git@") {
		address := raw
		host, path, found := strings.Cut(raw, "/")
		if found && net.ParseIP(host) != nil {
			address = "[" + host + "]/" + path
		}
		for _, prefix := range []string{"https://", "ssh://git@"} {
			if id, err := RepositoryIdentity(prefix + address); err == nil && id == raw {
				return id, nil
			}
		}
	}
	return "", errors.New("invalid memory repository identity")
}
func (s SharedMemoryScope) Validate() error {
	if !Identifier(s.WorkspaceID) {
		return errors.New("invalid workspace identity")
	}
	switch s.Kind {
	case "workspace":
		if s.ProjectID != "" || s.Repository != "" || len(s.Targets) != 0 {
			return errors.New("workspace scope has no project or repository")
		}
	case "project", "repository":
		if !Identifier(s.ProjectID) || len(s.Targets) != 0 {
			return errors.New("invalid project scope")
		}
		if s.Kind == "project" && s.Repository != "" {
			return errors.New("project scope has no repository")
		}
		if s.Kind == "repository" {
			if id, e := MemoryRepository(s.Repository); e != nil || id != s.Repository {
				return errors.New("use canonical repository identity")
			}
		}
	case "cross_repository":
		if s.ProjectID != "" || s.Repository != "" || len(s.Targets) < 2 || len(s.Targets) > 8 {
			return errors.New("cross-repository scope requires 2 to 8 explicit targets")
		}
		seenProjects, seenRepositories := map[string]bool{}, map[string]bool{}
		for _, t := range s.Targets {
			id, e := MemoryRepository(t.Repository)
			if !Identifier(t.ProjectID) || e != nil || id != t.Repository || seenProjects[t.ProjectID] || seenRepositories[t.Repository] {
				return errors.New("invalid or duplicate memory target")
			}
			seenProjects[t.ProjectID], seenRepositories[t.Repository] = true, true
		}
	default:
		return errors.New("unsupported memory scope")
	}
	return nil
}
func (s SharedMemoryScope) Projects() []MemoryTarget {
	if s.Kind == "cross_repository" {
		return s.Targets
	}
	if s.ProjectID != "" {
		return []MemoryTarget{{ProjectID: s.ProjectID, Repository: s.Repository}}
	}
	return nil
}

// Only reviewed, bounded statements cross the bridge. No arbitrary JSON, local
// references, file paths, diffs, transcript or execution configuration fields exist.
type MemorySource struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
}
type SharedMemoryContent struct {
	Kind           string       `json:"kind"` // observation, preference, convention, policy
	Summary        string       `json:"summary"`
	Classification string       `json:"classification"` // team or restricted
	Verification   string       `json:"verification"`   // unverified, verified, stale, rejected
	Source         MemorySource `json:"source"`
}

func (c SharedMemoryContent) Validate() error {
	switch c.Kind {
	case "observation", "preference", "convention", "policy":
	default:
		return errors.New("invalid memory kind")
	}
	switch c.Classification {
	case "team", "restricted":
	default:
		return errors.New("invalid classification")
	}
	switch c.Verification {
	case "unverified", "verified", "stale", "rejected":
	default:
		return errors.New("invalid verification")
	}
	if c.Source.Kind != "human" && c.Source.Kind != "derived" && c.Source.Kind != "git" {
		return errors.New("invalid provenance kind")
	}
	if !Identifier(c.Source.ID) {
		return errors.New("invalid source identity")
	}
	if c.Source.Revision != "" && !memorySHA(c.Source.Revision) || c.Source.Kind == "git" && !memorySHA(c.Source.Revision) {
		return errors.New("invalid source revision")
	}
	if !utf8.ValidString(c.Summary) || len(c.Summary) < 1 || len(c.Summary) > 2048 || strings.TrimSpace(c.Summary) == "" || strings.TrimSpace(c.Summary) != c.Summary {
		return errors.New("summary must contain 1 to 2048 bounded bytes")
	}
	for _, r := range c.Summary {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return errors.New("summary must be plain text without control characters")
		}
	}
	lower := strings.ToLower(c.Summary)
	for _, s := range []string{"-----begin", "diff --git", "@@ ", "/users/", "/home/", "c:\\", "bearer ", "sk-", "ghp_", "github_pat_", "wba_", "wbt_", "api_key=", "api_key:", "password=", "<|im_start|>", "terminal transcript", "raw conversation"} {
		if strings.Contains(lower, s) {
			return errors.New("summary contains prohibited local or sensitive data")
		}
	}
	return nil
}
func memorySHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

type MemoryProposal struct {
	Schema     string              `json:"schema"`
	Scope      SharedMemoryScope   `json:"scope"`
	Content    SharedMemoryContent `json:"content"`
	Supersedes string              `json:"supersedes,omitempty"`
	// ClientID is a durable per-author idempotency key, not a claimed author identity.
	ClientID string `json:"clientId"`
}

func (p MemoryProposal) Validate() error {
	if p.Schema != MemorySchema {
		return errors.New("unsupported memory schema")
	}
	if !Identifier(p.ClientID) || p.Supersedes != "" && !Identifier(p.Supersedes) {
		return errors.New("invalid memory identity")
	}
	if err := p.Scope.Validate(); err != nil {
		return err
	}
	return p.Content.Validate()
}

type MemoryCommand struct {
	Schema  string               `json:"schema"`
	Scope   SharedMemoryScope    `json:"scope"`
	Version int64                `json:"version"`
	Action  string               `json:"action"` // edit, approve, publish, delete
	Content *SharedMemoryContent `json:"content,omitempty"`
}

func (c MemoryCommand) Validate() error {
	if c.Schema != MemorySchema || c.Version < 1 {
		return errors.New("unsupported schema or invalid memory version")
	}
	if err := c.Scope.Validate(); err != nil {
		return err
	}
	switch c.Action {
	case "edit":
		if c.Content == nil {
			return errors.New("edit requires content")
		}
		return c.Content.Validate()
	case "approve", "publish", "delete":
		if c.Content != nil {
			return errors.New("action cannot replace content")
		}
	default:
		return errors.New("unsupported memory action")
	}
	return nil
}

type MemoryApproval struct {
	MemberID string    `json:"memberId"`
	DeviceID string    `json:"deviceId,omitempty"`
	Version  int64     `json:"version"`
	At       time.Time `json:"at"`
}
type SharedMemoryRecord struct {
	Schema         string              `json:"schema"`
	ID             string              `json:"id"`
	Scope          SharedMemoryScope   `json:"scope"`
	Content        SharedMemoryContent `json:"content"`
	Version        int64               `json:"version"`
	State          string              `json:"state"`
	AuthorID       string              `json:"authorId"`
	AuthorDeviceID string              `json:"authorDeviceId,omitempty"`
	UpdatedBy      string              `json:"updatedBy"`
	Approval       *MemoryApproval     `json:"approval,omitempty"`
	Supersedes     string              `json:"supersedes,omitempty"`
	SupersededBy   string              `json:"supersededBy,omitempty"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}

func (r SharedMemoryRecord) Validate() error {
	if r.Schema != MemorySchema || !Identifier(r.ID) || !Identifier(r.AuthorID) || !Identifier(r.UpdatedBy) || r.AuthorDeviceID != "" && !Identifier(r.AuthorDeviceID) || r.Version < 1 || r.CreatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return errors.New("invalid memory record identity, version, or timestamps")
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.Supersedes != "" && !Identifier(r.Supersedes) || r.SupersededBy != "" && !Identifier(r.SupersededBy) {
		return errors.New("invalid memory supersession identity")
	}
	content := r.Content
	if r.State == "deleted" && content.Summary == "" {
		content.Summary = "deleted"
	}
	if err := content.Validate(); err != nil {
		return err
	}
	if r.State == "deleted" {
		if r.Content.Summary != "" || r.Approval != nil || r.SupersededBy != "" {
			return errors.New("invalid deleted memory record")
		}
		return nil
	}
	if r.State == "suggested" {
		if r.Approval != nil || r.SupersededBy != "" {
			return errors.New("invalid suggested memory record")
		}
		return nil
	}
	if r.State != "approved" && r.State != "published" && r.State != "superseded" {
		return errors.New("unsupported memory state")
	}
	if r.Approval == nil || r.Approval.MemberID == r.AuthorID || !Identifier(r.Approval.MemberID) || r.Approval.DeviceID != "" && !Identifier(r.Approval.DeviceID) || r.Approval.Version < 1 || r.Approval.At.IsZero() || r.Content.Verification != "verified" {
		return errors.New("invalid memory approval")
	}
	switch r.State {
	case "approved":
		if r.Approval.Version != r.Version || r.SupersededBy != "" {
			return errors.New("approval does not cover the current memory version")
		}
	case "published":
		if r.Approval.Version+1 != r.Version || r.SupersededBy != "" {
			return errors.New("publication does not match the approved memory version")
		}
	case "superseded":
		if r.Approval.Version+2 != r.Version || r.SupersededBy == "" {
			return errors.New("invalid superseded memory record")
		}
	}
	return nil
}

// MemoryPage is one bounded exact-scope keyset page. A full replacement read
// starts with an empty cursor, collects every page, then replaces the old view.
type MemoryPage struct {
	Schema  string               `json:"schema"`
	Records []SharedMemoryRecord `json:"records"`
	More    bool                 `json:"more"`
	Next    string               `json:"next,omitempty"`
}

// DecodeMemory rejects unknown nested fields, trailing values and unsupported versions.
func DecodeMemory(raw []byte, out any) error {
	if len(raw) > 256<<10 {
		return errors.New("memory message too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing memory data")
	}
	switch v := out.(type) {
	case *MemoryEventPage:
		if v.Schema != MemorySchema || len(v.Events)+len(v.Tracking) > MaxMemoryBatch {
			return errors.New("unsupported or oversized publication page")
		}
		for _, e := range append(v.Events, v.Tracking...) {
			if err := e.Validate(); err != nil {
				return err
			}
		}
	case *MemoryReceipt:
		if !Identifier(v.EventID) || v.Sequence < 1 {
			return errors.New("invalid publication receipt")
		}
		return v.Record.Validate()
	case *MemoryProposal:
		return v.Validate()
	case *MemoryCommand:
		return v.Validate()
	case *MemoryShares:
		if v.Schema != MemorySchema || len(v.Shares) > MaxMemoryBatch || v.More && (len(v.Shares) == 0 || !Identifier(v.Next) || v.Next != v.Shares[len(v.Shares)-1].ID) || !v.More && v.Next != "" {
			return errors.New("unsupported or oversized shares page")
		}
		for _, s := range v.Shares {
			if err := s.Validate(); err != nil {
				return err
			}
		}
	case *MemoryPage:
		if v.Schema != MemorySchema || len(v.Records) > MaxMemoryBatch || v.More && (len(v.Records) == 0 || !Identifier(v.Next) || v.Next != v.Records[len(v.Records)-1].ID) || !v.More && v.Next != "" {
			return errors.New("unsupported or oversized memory page")
		}
		for _, r := range v.Records {
			if err := r.Validate(); err != nil {
				return err
			}
		}
	case *SharedMemoryRecord:
		return v.Validate()
	default:
		return fmt.Errorf("unsupported memory DTO %T", out)
	}
	return nil
}

// MemoryShare is an immutable, locally reviewed outgoing proposal, explicitly
// bound to one workspace. Approval here permits sharing, never Team policy.
type MemoryShare struct {
	Schema     string         `json:"schema"`
	ID         string         `json:"id"`
	Proposal   MemoryProposal `json:"proposal"`
	ApprovedAt time.Time      `json:"approvedAt"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}
type MemoryShares struct {
	Schema string        `json:"schema"`
	Shares []MemoryShare `json:"shares"`
	More   bool          `json:"more"`
	Next   string        `json:"next,omitempty"`
}

func (s MemoryShare) Validate() error {
	if s.Schema != MemorySchema || !Identifier(s.ID) || s.ApprovedAt.IsZero() || !s.ExpiresAt.After(s.ApprovedAt) || s.ExpiresAt.Sub(s.ApprovedAt) > 30*24*time.Hour {
		return errors.New("invalid memory sharing approval")
	}
	if s.Proposal.Content.Classification != "team" {
		return errors.New("only team-classified records may cross the local bridge")
	}
	return s.Proposal.Validate()
}
