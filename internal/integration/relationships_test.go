package integration

import (
	"strings"
	"testing"
	"time"
)

func TestRelationshipTraversalCyclesAndDepth(t *testing.T) {
	a := MemoryTarget{"a", "github.com/acme/a"}
	b := MemoryTarget{"b", "github.com/acme/b"}
	c := MemoryTarget{"c", "github.com/acme/c"}
	d := MemoryTarget{"d", "github.com/acme/d"}
	edges := []RelationshipResult{}
	for i, p := range [][2]MemoryTarget{{a, b}, {b, c}, {c, a}, {c, d}} {
		edges = append(edges, RelationshipResult{ID: string(rune('a' + i)), Relationship: RepositoryRelationship{Source: p[0], Target: p[1], Type: "api_consumer", Evidence: "manual"}})
	}
	if got := TraverseRelationships(a, edges, 1); len(got) != 2 {
		t.Fatal(got)
	}
	got := TraverseRelationships(a, edges, 4)
	if len(got) != 4 {
		t.Fatal(got)
	}
	for _, e := range got {
		if e.Recommendation != RelationshipRecommendation {
			t.Fatal(e)
		}
	}
	// Traversal cannot manufacture an omitted/inaccessible edge or vertex.
	if got := TraverseRelationships(a, edges[:1], 4); len(got) != 1 {
		t.Fatal(got)
	}
	now := time.Now()
	r := edges[0].Relationship
	if RelationshipStatus(r, now) != "provisional" {
		t.Fatal(r)
	}
	r.LastVerified = &now
	if RelationshipStatus(r, now) != "verified" {
		t.Fatal(r)
	}
	if RelationshipStatus(r, now.Add(25*time.Hour)) != "stale" {
		t.Fatal(r)
	}
	r.Target.Repository = "a"
	if r.Validate() == nil {
		t.Fatal("name accepted as identity")
	}
}
func TestManifestDiscoveryNeverUsesNamesOrCredentials(t *testing.T) {
	refs, e := ManifestRepositories("package.json", []byte(`{"dependencies":{"a":"^1.0","b":"github:acme/b","c":"git+https://github.com/acme/c.git#main","d":"https://token@github.com/acme/private.git","e":"file:../private","f":"git+ssh://git@github.com/acme/f.git"}}`))
	if e != nil || strings.Join(refs, ",") != "github.com/acme/c,github.com/acme/f" {
		t.Fatal(refs, e)
	}
	refs, e = ManifestRepositories("go.mod", []byte("module acme/local\nrequire github.com/acme/a v1.0.0\nreplace foo => ../private\n"))
	if e != nil || len(refs) != 0 {
		t.Fatal(refs, e)
	}
	if _, e = ManifestRepositories("package.json", []byte("{")); e == nil {
		t.Fatal("malformed manifest")
	}
}
func TestRelationshipPublicationScopeBinding(t *testing.T) {
	r := RepositoryRelationship{Source: MemoryTarget{"a", "github.com/acme/a"}, Target: MemoryTarget{"b", "github.com/acme/b"}, Type: "package_dependency", Evidence: "package.json", Inferred: true}
	p := MemoryProposal{Schema: MemorySchema, ClientID: "c", Scope: SharedMemoryScope{WorkspaceID: "w", Kind: "project", ProjectID: "a"}, Content: SharedMemoryContent{Kind: "observation", Summary: "dependency", Classification: "team", Verification: "unverified", Source: MemorySource{Kind: "derived", ID: "manifest"}, Relationship: &r}}
	if p.Validate() == nil {
		t.Fatal("single-project relationship accepted")
	}
	p.Scope = SharedMemoryScope{WorkspaceID: "w", Kind: "cross_repository", Targets: []MemoryTarget{r.Source, r.Target}}
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	p.Content.Verification = "verified"
	if p.Validate() == nil {
		t.Fatal("unverified inference upgraded")
	}
}
