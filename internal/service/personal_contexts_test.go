package service

import "testing"

func TestDisplayContextsAreValidatedAndReplaced(t *testing.T) {
	p := &Personal{}
	if got := p.DisplayContexts(); len(got) != 0 {
		t.Fatalf("a fresh service lists no Teams, got %v", got)
	}
	if err := p.SetDisplayContexts([]DisplayContext{{ID: "team:main", Name: "Acme"}}); err != nil {
		t.Fatal(err)
	}
	if got := p.DisplayContexts(); len(got) != 1 || got[0].Name != "Acme" {
		t.Fatalf("got %v", got)
	}
	for _, bad := range [][]DisplayContext{
		{{ID: "has space", Name: "x"}},
		{{ID: "team:a", Name: ""}},
		{{ID: "team:a", Name: "x"}, {ID: "team:a", Name: "y"}},
	} {
		if p.SetDisplayContexts(bad) == nil {
			t.Fatalf("%v should be refused", bad)
		}
	}
	if got := p.DisplayContexts(); len(got) != 1 {
		t.Fatalf("a refused list must leave the last good one, got %v", got)
	}
}
