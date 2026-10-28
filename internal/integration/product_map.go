package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const ProductMapSchema = "werkbord.product-map/v1"
const MaxProductMapBytes = 512 << 10

// ProductMapContent is generated documentation, never instructions or executable
// content. Evidence names immutable source locations; no source bytes are shared.
type ProductMapContent struct {
	Features      []ProductFeature      `json:"features"`
	Relationships []ProductRelationship `json:"relationships"`
	Limitations   []string              `json:"limitations"`
}
type ProductFeature struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Scenarios   []string          `json:"scenarios"`
	Evidence    []ProductEvidence `json:"evidence"`
	Inferred    bool              `json:"inferred"`
}
type ProductEvidence struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}
type ProductRelationship struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Description string `json:"description"`
}
type ProductMapSnapshot struct {
	Schema         string            `json:"schema"`
	ID             string            `json:"id"`
	Version        int64             `json:"version"`
	Repository     string            `json:"repository"`
	Branch         string            `json:"branch"`
	Commit         string            `json:"commit"`
	Tree           string            `json:"tree"`
	SourceDigest   string            `json:"sourceDigest"`
	Rules          int               `json:"rules"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	CreatedAt      time.Time         `json:"createdAt"`
	FilesRead      int               `json:"filesRead"`
	FilesAvailable int               `json:"filesAvailable"`
	Content        ProductMapContent `json:"content"`
}

var mapID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var digestSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)
var branchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,249}$`)
var mapSHA = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)

func mapText(s string, n int) bool {
	return utf8.ValidString(s) && len(s) <= n && !strings.ContainsAny(s, "\x00\r")
}

// ProductSourcePath is deliberately stricter than a filesystem path: these
// references may cross machines but never open absolute or credential paths.
func ProductSourcePath(p string) bool {
	if p == "" || len(p) > 512 || !utf8.ValidString(p) || path.IsAbs(p) || path.Clean(p) != p || strings.ContainsAny(p, "\\\x00\r\n:") {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, s := range strings.Split(strings.ToLower(p), "/") {
		if s == ".." || s == "." || strings.HasPrefix(s, ".") || s == "node_modules" || s == "vendor" || s == "credentials" || s == "secrets" || strings.Contains(s, "secret") || strings.Contains(s, "credential") || strings.HasSuffix(s, ".pem") || strings.HasSuffix(s, ".key") || strings.HasSuffix(s, ".p12") || strings.HasSuffix(s, ".pfx") || s == "agents.md" || s == "claude.md" || s == "gemini.md" {
			return false
		}
	}
	return true
}
func (c ProductMapContent) Validate() error {
	if len(c.Features) == 0 || len(c.Features) > 300 || len(c.Relationships) > 1000 || len(c.Limitations) > 50 {
		return errors.New("invalid product map size")
	}
	ids := map[string]bool{}
	for _, f := range c.Features {
		if !mapID.MatchString(f.ID) || ids[f.ID] || strings.TrimSpace(f.Name) == "" || !mapText(f.Name, 160) || !mapText(f.Description, 8000) || len(f.Scenarios) > 40 || len(f.Evidence) == 0 || len(f.Evidence) > 80 {
			return errors.New("invalid product feature")
		}
		ids[f.ID] = true
		for _, s := range f.Scenarios {
			if s == "" || !mapText(s, 2000) {
				return errors.New("invalid scenario")
			}
		}
		for _, e := range f.Evidence {
			if !ProductSourcePath(e.Path) || e.Start < 1 || e.End < e.Start || e.End-e.Start > 5000 {
				return errors.New("invalid source reference")
			}
		}
	}
	for _, r := range c.Relationships {
		if !ids[r.From] || !ids[r.To] || r.From == r.To || !mapText(r.Description, 1000) {
			return errors.New("invalid relationship")
		}
	}
	for _, s := range c.Limitations {
		if !mapText(s, 2000) {
			return errors.New("invalid limitation")
		}
	}
	raw, _ := json.Marshal(c)
	if len(raw) > MaxProductMapBytes {
		return errors.New("product map exceeds storage limit")
	}
	return nil
}
func (s ProductMapSnapshot) Validate() error {
	if s.Schema != ProductMapSchema || !mapID.MatchString(s.ID) || s.Version < 1 || s.Repository == "" || !mapText(s.Repository, 600) || !branchName.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || strings.Contains(s.Branch, "//") || strings.HasSuffix(s.Branch, "/") || strings.HasSuffix(s.Branch, ".") || !mapSHA.MatchString(s.Commit) || !mapSHA.MatchString(s.Tree) || !digestSHA.MatchString(s.SourceDigest) || s.Rules < 1 || s.CreatedAt.IsZero() || s.FilesRead < 0 || s.FilesAvailable < s.FilesRead || !mapText(s.Provider, 80) || !mapText(s.Model, 160) {
		return errors.New("invalid product map snapshot")
	}
	return s.Content.Validate()
}
func DecodeProductMap(raw []byte, dst any) error {
	if len(raw) > MaxProductMapBytes {
		return errors.New("product map exceeds size limit")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("expected one product map document")
	}
	return nil
}
