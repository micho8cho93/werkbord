package shell

import (
	"strings"
	"testing"
)

func TestAnalyticsReadsOneWorkspaceByTheAppsOwnListAndNothingElse(t *testing.T) {
	r := newRig(t)
	got, err := r.Analytics("personal", "prj_abc", 30)
	if err != nil || string(got) != `{"created":5}` {
		t.Fatalf("personal: %s %v", got, err)
	}
	if got, err := r.Analytics("team:main", "", 7); err != nil || string(got) != `{"created":3}` {
		t.Fatalf("team: %s %v", got, err)
	}
	if strings.Join(r.gr.reads, "|") != "prj_abc 30" || strings.Join(r.tm.analytics, "|") != "team:main  7" {
		t.Fatalf("reads: %v %v", r.gr.reads, r.tm.analytics)
	}
}

func TestAnalyticsRefusesWhatThePagesNeverAsk(t *testing.T) {
	r := newRig(t)
	for name, c := range map[string]struct {
		id, project string
		days        int
	}{
		"a range that is not offered":  {"personal", "", 14},
		"no range":                     {"personal", "", 0},
		"a project that is a path":     {"personal", "../x", 30},
		"a project with a query":       {"personal", "p&days=1", 30},
		"a workspace that is not here": {"team:gone", "", 30},
		"an address instead of an id":  {"http://127.0.0.1:7420", "", 30},
		"a project too long to be one": {"personal", strings.Repeat("a", 81), 30},
	} {
		if _, err := r.Analytics(c.id, c.project, c.days); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(r.gr.reads) != 0 || len(r.tm.analytics) != 0 {
		t.Fatalf("a refused request reached a workspace: %v %v", r.gr.reads, r.tm.analytics)
	}
}
