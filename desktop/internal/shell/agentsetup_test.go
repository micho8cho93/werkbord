package shell

import (
	"strings"
	"testing"
)

func TestAgentsAndConnectionsReadsOnlyForAPlainProject(t *testing.T) {
	r := newRig(t)
	if got, err := r.AgentsAndConnections("prj_abc"); err != nil || string(got) != `{"agents":[],"tools":[],"usage":null}` {
		t.Fatalf("%s %v", got, err)
	}
	for name, project := range map[string]string{"a path": "../x", "a query": "p?x=1", "an address": "http://127.0.0.1:7420", "empty": "", "too long": strings.Repeat("a", 81)} {
		if _, err := r.AgentsAndConnections(project); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if strings.Join(r.gr.setups, "|") != "prj_abc" {
		t.Fatalf("a refused request reached the workspace: %v", r.gr.setups)
	}
}
