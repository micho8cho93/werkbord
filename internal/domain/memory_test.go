package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validMemoryContent() MemoryContent {
	return MemoryContent{Type: "build_command", Authority: MemoryObservation, Source: MemoryProvenance{Type: MemorySourceImport, Reference: "manual-import:1"}, Summary: "Run go test"}
}
func TestMemoryValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*MemoryContent)
	}{
		{"empty summary", func(c *MemoryContent) { c.Summary = " " }},
		{"large summary", func(c *MemoryContent) { c.Summary = strings.Repeat("é", MaxMemorySummaryBytes) }},
		{"invalid UTF8", func(c *MemoryContent) { c.Summary = string([]byte{0xff}) }},
		{"large details", func(c *MemoryContent) {
			c.Details = json.RawMessage(`{"s":"` + strings.Repeat("x", MaxMemoryDetailsBytes) + `"}`)
		}},
		{"array", func(c *MemoryContent) { c.Details = json.RawMessage(`[]`) }},
		{"null", func(c *MemoryContent) { c.Details = json.RawMessage(`null`) }},
		{"trailing JSON", func(c *MemoryContent) { c.Details = json.RawMessage(`{} {}`) }},
		{"unknown authority", func(c *MemoryContent) { c.Authority = "confident" }},
		{"unknown source", func(c *MemoryContent) { c.Source.Type = "model" }},
		{"no provenance", func(c *MemoryContent) { c.Source.Reference = "" }},
		{"policy without approval", func(c *MemoryContent) { c.Authority = MemoryPolicy }},
		{"decision without approval", func(c *MemoryContent) { c.Authority = MemoryDecision }},
		{"secret eligible", func(c *MemoryContent) { c.Sensitivity = MemorySecret; c.SharingEligible = true }},
		{"sensitive eligible", func(c *MemoryContent) { c.Sensitivity = MemorySensitive; c.SharingEligible = true }},
		{"superseded without link", func(c *MemoryContent) { c.State = MemorySuperseded }},
		{"link without superseded", func(c *MemoryContent) { c.SupersededBy = "mem_other" }},
		{"short SHA", func(c *MemoryContent) { c.Source.GitSHA = "abc123" }},
		{"git without SHA", func(c *MemoryContent) { c.Source.Type = MemorySourceGit }},
		{"run without ID", func(c *MemoryContent) { c.Source.Type = MemorySourceRun }},
		{"handoff without ID", func(c *MemoryContent) { c.Source.Type = MemorySourceHandoff }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validMemoryContent()
			tt.edit(&c)
			if _, err := c.Normalized(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want invalid, got %v", err)
			}
		})
	}
	for _, authority := range []MemoryAuthority{MemoryObservation, MemoryPreference, MemoryDecision, MemoryPolicy} {
		c := validMemoryContent()
		c.Authority = authority
		c.Source = MemoryProvenance{Type: MemorySourceUser, Reference: "user:1", ApprovedBy: "local_user", ApprovalRef: "approval:1"}
		n, err := c.Normalized()
		if err != nil {
			t.Fatal(err)
		}
		if n.State != MemoryNeedsVerification || n.Sensitivity != MemoryPrivate || n.SharingEligible {
			t.Fatalf("unsafe defaults: %+v", n)
		}
	}
}
func TestMemoryFingerprintPreservesAuthorityAndEvidence(t *testing.T) {
	a := validMemoryContent()
	a.Details = json.RawMessage(`{"z":9007199254740993,"a":true}`)
	b := a
	b.Details = json.RawMessage(`{ "a":true, "z":9007199254740993 }`)
	a, err := a.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	b, err = b.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if MemoryFingerprint(a) != MemoryFingerprint(b) {
		t.Fatal("JSON key ordering changes deduplication")
	}
	if !strings.Contains(string(a.Details), "9007199254740993") {
		t.Fatal("integer precision lost")
	}
	b.State = MemoryHistorical
	if MemoryFingerprint(a) != MemoryFingerprint(b) {
		t.Fatal("lifecycle should not create new evidence")
	}
	b.Authority = MemoryPreference
	if MemoryFingerprint(a) == MemoryFingerprint(b) {
		t.Fatal("observation merged with preference")
	}
	b = a
	b.Source.Reference = "different"
	if MemoryFingerprint(a) == MemoryFingerprint(b) {
		t.Fatal("different provenance merged")
	}
	b = a
	b.SharingEligible = true
	if MemoryFingerprint(a) == MemoryFingerprint(b) {
		t.Fatal("privacy merged")
	}
	c := validMemoryContent()
	d := c
	d.Details = json.RawMessage(`{}`)
	c, _ = c.Normalized()
	d, _ = d.Normalized()
	if MemoryFingerprint(c) != MemoryFingerprint(d) {
		t.Fatal("empty details inconsistent")
	}
}
func TestMemoryLimitsAndQueries(t *testing.T) {
	if err := DefaultMemoryLimits().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, l := range []MemoryLimits{{}, {Records: MaxMemoryRecords + 1, Bytes: MaxMemoryRecordBytes, HistoryRetention: 24 * time.Hour}, {Records: 1, Bytes: MaxMemoryBytes + 1, HistoryRetention: 24 * time.Hour}, {Records: 1, Bytes: MaxMemoryRecordBytes, HistoryRetention: time.Hour}} {
		if !errors.Is(l.Validate(), ErrInvalid) {
			t.Fatalf("accepted %+v", l)
		}
	}
	for _, q := range []MemoryQuery{{Limit: 101}, {Limit: -1}, {Text: strings.Repeat("x", 257)}, {Text: " "}, {Text: strings.Repeat("x ", 17)}, {State: "bad"}, {Authority: "bad"}, {After: &MemoryCursor{ID: "mem_x"}}} {
		if !errors.Is(ValidateMemoryQuery(q), ErrInvalid) {
			t.Fatalf("accepted %+v", q)
		}
	}
}

func TestMemoryGitProvenancePathsAndBranches(t *testing.T) {
	for _, branch := range []string{"main", "HEAD", "refs/heads/", "refs/heads/a..b", "refs/heads/a\x00b", "refs/heads/a//b", "refs/heads/a.lock", "refs/tags/v1"} {
		c := validMemoryContent()
		c.Source.Branch = branch
		if _, err := c.Normalized(); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted branch %q: %v", branch, err)
		}
	}
	for _, paths := range [][]string{{"../secret"}, {"/tmp/file"}, {"a/../b"}, {"."}, {"a", "a"}, {"a\\b"}, {"a\x00b"}} {
		c := validMemoryContent()
		c.Source.Paths = paths
		if _, err := c.Normalized(); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted paths %q: %v", paths, err)
		}
	}
	original := validMemoryContent()
	original.Source.Branch = "refs/remotes/origin/main"
	original.Source.Paths = []string{"src/b", "src/a"}
	normalized, err := original.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Source.Paths[0] != "src/a" || original.Source.Paths[0] != "src/b" {
		t.Fatal("normalization mutated caller or did not sort")
	}
	reversed := normalized
	reversed.Source.Paths = []string{"src/b", "src/a"}
	reversed, err = reversed.Normalized()
	if err != nil || MemoryFingerprint(normalized) != MemoryFingerprint(reversed) {
		t.Fatal("path order changed identity", err)
	}
}
