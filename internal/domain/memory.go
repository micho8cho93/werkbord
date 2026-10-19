package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MemorySchemaVersion         = 1
	MaxMemorySummaryBytes       = 4096
	MaxMemoryDetailsBytes       = 16384
	MaxMemoryRecordBytes        = 32768
	MaxMemoryRecords            = 100000
	MaxMemoryBytes        int64 = 256 << 20
)

// MemoryAuthority describes evidence, not an interchangeable confidence score.
// Nothing in this package grants a record permission to act on the application.
type MemoryAuthority string

const (
	MemoryObservation MemoryAuthority = "factual_observation"
	MemoryPreference  MemoryAuthority = "inferred_preference"
	MemoryDecision    MemoryAuthority = "explicit_decision"
	MemoryPolicy      MemoryAuthority = "approved_policy"
)

type MemoryState string

const (
	MemoryCurrent           MemoryState = "current"
	MemoryNeedsVerification MemoryState = "needs_verification"
	MemoryHistorical        MemoryState = "historical"
	MemorySuperseded        MemoryState = "superseded"
)

type MemorySensitivity string

const (
	MemoryPrivate   MemorySensitivity = "private"
	MemorySensitive MemorySensitivity = "sensitive"
	MemorySecret    MemorySensitivity = "secret"
)

type MemorySourceType string

const (
	MemorySourceUser    MemorySourceType = "user"
	MemorySourceGit     MemorySourceType = "git"
	MemorySourceRun     MemorySourceType = "run"
	MemorySourceHandoff MemorySourceType = "handoff"
	MemorySourceImport  MemorySourceType = "import"
)

// MemoryScope is an exact partition: empty project/repository means personal,
// not a wildcard. Repository is the canonical remote identity, never a path/name.
type MemoryScope struct {
	OwnerID    string `json:"ownerId"`
	ProjectID  string `json:"projectId,omitempty"`
	Repository string `json:"repository,omitempty"`
}

type MemoryProvenance struct {
	Type        MemorySourceType `json:"type"`
	Reference   string           `json:"reference"`
	GitSHA      string           `json:"gitSha,omitempty"`
	Branch      string           `json:"branch,omitempty"` // full refs/heads/... or refs/remotes/...
	Paths       []string         `json:"paths,omitempty"`  // repository-relative files or directories
	PullRequest string           `json:"pullRequest,omitempty"`
	RunID       string           `json:"runId,omitempty"`
	HandoffID   string           `json:"handoffId,omitempty"`
	ApprovedBy  string           `json:"approvedBy,omitempty"`
	ApprovalRef string           `json:"approvalRef,omitempty"`
}

// MemoryContent is the replaceable part of a record. Type is a caller-defined
// bounded identifier (e.g. build_command); Authority has closed semantics.
type MemoryContent struct {
	Type        string            `json:"type"`
	Authority   MemoryAuthority   `json:"authority"`
	Source      MemoryProvenance  `json:"source"`
	Summary     string            `json:"summary"`
	Details     json.RawMessage   `json:"details,omitempty"`
	State       MemoryState       `json:"state"`
	Sensitivity MemorySensitivity `json:"sensitivity"`
	// Eligibility is descriptive only; publication still requires an exact local grant and an allowlisted projection.
	Pinned          bool       `json:"pinned"`
	SharingEligible bool       `json:"sharingEligible"`
	VerifiedAt      *time.Time `json:"verifiedAt,omitempty"`
	SupersededBy    string     `json:"supersededBy,omitempty"`
	DuplicateOf     string     `json:"duplicateOf,omitempty"`
}

type Memory struct {
	ID    string      `json:"id"`
	Scope MemoryScope `json:"scope"`
	MemoryContent
	Fingerprint   string    `json:"fingerprint"`
	SchemaVersion int       `json:"schemaVersion"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// MemoryQuery always operates in one exact scope, newest creation first. After
// is a keyset cursor (created milliseconds and ID), never an SQL/FTS expression.
type MemoryCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type MemoryQuery struct {
	// IncludeCandidates is used only by bounded context retrieval, never list authorization.
	IncludeCandidates bool
	Text              string
	Type              string
	Authority         MemoryAuthority
	State             MemoryState
	Limit             int
	After             *MemoryCursor
}
type MemoryPage struct {
	Records []Memory      `json:"records"`
	Next    *MemoryCursor `json:"next,omitempty"`
}
type MemoryUsage struct {
	Records int
	Bytes   int64
}

// MemoryLimits apply across all scopes of one owner. Global hard ceilings also
// apply. Retention only deletes unreferenced historical/superseded records.
type MemoryLimits struct {
	Records          int
	Bytes            int64
	HistoryRetention time.Duration
}

func DefaultMemoryLimits() MemoryLimits {
	return MemoryLimits{Records: 10000, Bytes: 32 << 20, HistoryRetention: 180 * 24 * time.Hour}
}
func (l MemoryLimits) Validate() error {
	if l.Records < 1 || l.Records > MaxMemoryRecords || l.Bytes < MaxMemoryRecordBytes || l.Bytes > MaxMemoryBytes || l.HistoryRetention < 24*time.Hour || l.HistoryRetention > 10*365*24*time.Hour {
		return fmt.Errorf("%w: invalid memory limits", ErrInvalid)
	}
	return nil
}

func memoryIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func (s MemoryScope) Validate() error {
	if !memoryIdentifier(s.OwnerID) || s.ProjectID != "" && !memoryIdentifier(s.ProjectID) || len(s.Repository) > 2000 {
		return fmt.Errorf("%w: invalid memory scope", ErrInvalid)
	}
	return nil
}
func (a MemoryAuthority) Valid() bool {
	return a == MemoryObservation || a == MemoryPreference || a == MemoryDecision || a == MemoryPolicy
}
func (s MemoryState) Valid() bool {
	return s == MemoryCurrent || s == MemoryNeedsVerification || s == MemoryHistorical || s == MemorySuperseded
}

// Normalized applies conservative defaults and canonical JSON object encoding.
func (c MemoryContent) Normalized() (MemoryContent, error) {
	c.Source.Paths = append([]string(nil), c.Source.Paths...)
	sort.Strings(c.Source.Paths)
	c.Summary = strings.TrimSpace(c.Summary)
	if c.State == "" {
		c.State = MemoryNeedsVerification
	}
	if c.Sensitivity == "" {
		c.Sensitivity = MemoryPrivate
	}
	if len(c.Details) > MaxMemoryDetailsBytes || !utf8.Valid(c.Details) {
		return c, fmt.Errorf("%w: memory details too large", ErrInvalid)
	}
	if len(c.Details) == 0 {
		c.Details = json.RawMessage("{}")
	}
	if len(c.Details) > 0 {
		// UseNumber avoids rounding integers during canonicalization.
		dec := json.NewDecoder(bytes.NewReader(c.Details))
		dec.UseNumber()
		var obj map[string]any
		if !json.Valid(c.Details) || dec.Decode(&obj) != nil || obj == nil {
			return c, fmt.Errorf("%w: memory details must be a JSON object", ErrInvalid)
		}
		c.Details, _ = json.Marshal(obj)
	}
	if c.VerifiedAt != nil {
		v := c.VerifiedAt.UTC().Truncate(time.Millisecond)
		c.VerifiedAt = &v
	}
	return c, c.Validate()
}
func (c MemoryContent) Validate() error {
	bad := func(s string) error { return fmt.Errorf("%w: %s", ErrInvalid, s) }
	if !memoryIdentifier(c.Type) || !c.Authority.Valid() || !c.State.Valid() {
		return bad("invalid memory type, authority or state")
	}
	if strings.TrimSpace(c.Summary) == "" || len(c.Summary) > MaxMemorySummaryBytes || !utf8.ValidString(c.Summary) || strings.ContainsRune(c.Summary, 0) {
		return bad("invalid memory summary")
	}
	if len(c.Details) > MaxMemoryDetailsBytes || len(c.Details) > 0 && (!json.Valid(c.Details) || bytes.TrimSpace(c.Details)[0] != '{') {
		return bad("invalid memory details")
	}
	if c.Sensitivity != MemoryPrivate && c.Sensitivity != MemorySensitive && c.Sensitivity != MemorySecret {
		return bad("invalid memory sensitivity")
	}
	if c.SharingEligible && c.Sensitivity != MemoryPrivate {
		return bad("sensitive memory cannot be sharing eligible")
	}
	p := c.Source
	if p.Branch != "" && (len(p.Branch) > 1024 || !utf8.ValidString(p.Branch) || strings.HasSuffix(p.Branch, "/") || strings.HasSuffix(p.Branch, ".") || strings.HasSuffix(p.Branch, ".lock") || strings.Contains(p.Branch, "//") || !(strings.HasPrefix(p.Branch, "refs/heads/") || strings.HasPrefix(p.Branch, "refs/remotes/")) || strings.ContainsAny(p.Branch, " \t\r\n\x00~^:?*[\\") || strings.Contains(p.Branch, "..") || strings.Contains(p.Branch, "@{")) {
		return bad("branch must be an unambiguous full Git ref")
	}
	if len(p.Paths) > 64 {
		return bad("too many memory source paths")
	}
	for i, name := range p.Paths {
		if len(name) == 0 || len(name) > 2000 || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\\") || path.IsAbs(name) || name == "." || name == ".." || strings.HasPrefix(name, "../") || path.Clean(name) != name || i > 0 && name == p.Paths[i-1] {
			return bad("source paths must be unique repository-relative paths")
		}
	}
	if p.Type != MemorySourceUser && p.Type != MemorySourceGit && p.Type != MemorySourceRun && p.Type != MemorySourceHandoff && p.Type != MemorySourceImport {
		return bad("invalid memory source")
	}
	if strings.TrimSpace(p.Reference) == "" {
		return bad("memory provenance reference required")
	}
	for _, s := range []string{p.Reference, p.PullRequest, p.ApprovalRef} {
		if len(s) > 2000 || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return bad("invalid provenance reference")
		}
	}
	for _, s := range []string{p.RunID, p.HandoffID, p.ApprovedBy, c.SupersededBy, c.DuplicateOf} {
		if s != "" && !memoryIdentifier(s) {
			return bad("invalid memory reference ID")
		}
	}
	if p.GitSHA != "" {
		if len(p.GitSHA) != 40 && len(p.GitSHA) != 64 {
			return bad("Git SHA must be a full lowercase commit ID")
		}
		b, err := hex.DecodeString(p.GitSHA)
		if err != nil || len(b) != 20 && len(b) != 32 || p.GitSHA != strings.ToLower(p.GitSHA) {
			return bad("Git SHA must be a full lowercase commit ID")
		}
	}
	if p.HandoffID != "" && p.RunID == "" || p.Type == MemorySourceGit && p.GitSHA == "" || p.Type == MemorySourceRun && p.RunID == "" || p.Type == MemorySourceHandoff && (p.RunID == "" || p.HandoffID == "") {
		return bad("source evidence is missing")
	}
	if (c.Authority == MemoryDecision || c.Authority == MemoryPolicy) && (p.Type != MemorySourceUser || p.ApprovedBy == "" || p.ApprovalRef == "") {
		return bad("decisions and policies require explicit user provenance and approval references")
	}
	if (c.State == MemorySuperseded) != (c.SupersededBy != "") || c.SupersededBy != "" && c.DuplicateOf != "" {
		return bad("invalid supersession references")
	}
	return nil
}

// MemoryFingerprint deduplicates exact normalized evidence, including authority,
// provenance and privacy. Lifecycle/verification/links never silently merge claims.
func MemoryFingerprint(c MemoryContent) string {
	raw, _ := json.Marshal(struct {
		Type            string
		Authority       MemoryAuthority
		Source          MemoryProvenance
		Summary         string
		Details         json.RawMessage
		Sensitivity     MemorySensitivity
		SharingEligible bool
	}{c.Type, c.Authority, c.Source, c.Summary, c.Details, c.Sensitivity, c.SharingEligible})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidateMemoryQuery bounds work and accepts only structured filters/cursors.
func ValidateMemoryQuery(q MemoryQuery) error {
	bad := func() error { return fmt.Errorf("%w: invalid memory query", ErrInvalid) }
	if q.Limit < 0 || q.Limit > 100 {
		return bad()
	}
	if len(q.Text) > 256 || !utf8.ValidString(q.Text) || strings.ContainsRune(q.Text, 0) {
		return bad()
	}
	if len(strings.Fields(q.Text)) > 16 || q.Text != "" && strings.TrimSpace(q.Text) == "" {
		return bad()
	}
	if q.Type != "" && !memoryIdentifier(q.Type) || q.Authority != "" && !q.Authority.Valid() || q.State != "" && !q.State.Valid() {
		return bad()
	}
	if q.After != nil && (!memoryIdentifier(q.After.ID) || q.After.CreatedAt.IsZero()) {
		return bad()
	}
	return nil
}
