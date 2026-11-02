package shell

import (
	"strings"
	"testing"
)

func TestProjectHeadReadsOnlyAPlainProjectsBranchAndCommit(t *testing.T) {
	r := newRig(t)
	got, err := r.ProjectHead("prj_abc")
	if err != nil || string(got) != `{"branch":"main","commit":"a1b2c3d"}` {
		t.Fatalf("%s %v", got, err)
	}
	for name, project := range map[string]string{"a path": "../x", "a query": "p?x=1", "an address": "http://127.0.0.1:7420", "empty": "", "too long": strings.Repeat("a", 81)} {
		if _, err := r.ProjectHead(project); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if strings.Join(r.gr.heads, "|") != "prj_abc" {
		t.Fatalf("a refused request reached the workspace: %v", r.gr.heads)
	}
}
