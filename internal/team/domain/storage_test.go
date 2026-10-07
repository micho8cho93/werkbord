package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestTopologiesAreDescribedHonestly(t *testing.T) {
	for _, c := range []struct {
		voters, tolerates, quorum int
		level                     TopologyLevel
		ha                        bool
		mustSay                   string
	}{
		{1, 0, 1, TopologySingle, false, "no high availability"},
		{2, 0, 2, TopologyTwo, false, "loss of either"},
		{3, 1, 2, TopologyRecommended, true, "recommended"},
		{4, 1, 3, TopologyEven, true, "no resilience"},
		{5, 2, 3, TopologyAdvanced, true, "any two"},
		{7, 3, 4, TopologyLarge, true, "tolerates the loss of 3"},
	} {
		d := DescribeTopology(c.voters, 0, c.voters)
		if d.FaultTolerance != c.tolerates || d.Quorum != c.quorum || d.Level != c.level || d.HighAvailability != c.ha || !d.Writable {
			t.Errorf("%d voters: %+v", c.voters, d)
		}
		if !strings.Contains(d.Summary+d.Recommendation, c.mustSay) {
			t.Errorf("%d voters: %q does not say %q", c.voters, d.Summary+d.Recommendation, c.mustSay)
		}
	}
	// Two hosts are no better than one against a failure, and the description says so.
	if two := DescribeTopology(2, 0, 2); two.HighAvailability {
		t.Error("two hosts are described as highly available")
	}
	// With the quorum gone the workspace is described as read-only, with the numbers.
	lost := DescribeTopology(3, 0, 1)
	if lost.Writable || !strings.Contains(lost.Summary, "READ-ONLY") || !strings.Contains(lost.Summary, "1 of 3") {
		t.Errorf("%+v", lost)
	}
	// And replicas that do not vote do not change any of it.
	if d := DescribeTopology(3, 2, 3); d.Quorum != 2 || d.NonVoters != 2 {
		t.Errorf("%+v", d)
	}
	if QuorumOf(0) != 0 || QuorumOf(1) != 1 || QuorumOf(3) != 2 || QuorumOf(5) != 3 {
		t.Error("quorum")
	}
	if d := DescribeTopology(0, 0, 0); d.Level != TopologyNone || d.Writable {
		t.Errorf("%+v", d)
	}
}

func hosts(spec string) []StorageHost {
	// "a+ b+ c-" : + voter reachable, - voter unreachable, "r" replica
	var out []StorageHost
	for _, f := range strings.Fields(spec) {
		h := StorageHost{NodeID: f[:1], Voter: true, Reachable: true}
		switch f[1:] {
		case "-":
			h.Reachable = false
		case "r":
			h.Voter = false
		}
		out = append(out, h)
	}
	return out
}

func TestARemovalThatWouldLeaveNoQuorumIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		cluster string
		remove  string
		ok      bool
	}{
		"one of three healthy":               {"a+ b+ c+", "c", true},
		"the leader of three":                {"a+ b+ c+", "a", true},
		"one of three, another down":         {"a+ b+ c-", "a", false}, // 2 left, one up: needs 2
		"the down one of three":              {"a+ b+ c-", "c", true},  // 2 left, both up
		"one of two":                         {"a+ b+", "a", true},
		"one of two, the other down":         {"a+ b-", "a", false},
		"the down one of two":                {"a+ b-", "b", false}, // quorum of 2 is 2, 1 up: no quorum to change anything
		"the only host":                      {"a+", "a", false},
		"one of five":                        {"a+ b+ c+ d+ e+", "e", true},
		"one of five, two down":              {"a+ b+ c+ d- e-", "a", false},
		"a down one of five, two down":       {"a+ b+ c+ d- e-", "d", true},
		"one of five, two down, quorum is 3": {"a+ b+ c+ d- e-", "b", false},
		"no quorum now":                      {"a+ b- c-", "a", false},
		"a replica of a healthy cluster":     {"a+ b+ c+ dr", "d", true},
		"a replica when the quorum is lost":  {"a+ b- c- dr", "d", false},
		"a node that is not there":           {"a+ b+ c+", "z", false},
		"one of four":                        {"a+ b+ c+ d+", "d", true},
		"one of four with one down":          {"a+ b+ c+ d-", "a", true}, // b, c and d remain, and b and c answer: 2 of 3
	} {
		err := CheckRemoval(hosts(c.cluster), c.remove)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v (want ok=%v)", name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: the error is not a conflict: %v", name, err)
		}
	}
}

func TestRemovalWarnsWhenItWeakensTheCluster(t *testing.T) {
	if w := RemovalWarning(hosts("a+ b+ c+"), "c"); !strings.Contains(w, "tolerates the loss of 0") {
		t.Errorf("%q", w)
	}
	if w := RemovalWarning(hosts("a+ b+ c+ d+ e+"), "e"); !strings.Contains(w, "tolerates the loss of 1") {
		t.Errorf("five to four goes from tolerating two failures to one, and must say so: %q", w)
	}
	if w := RemovalWarning(hosts("a+ b+ c+ d+"), "d"); w != "" {
		t.Errorf("four to three loses nothing: %q", w)
	}
	if w := RemovalWarning(hosts("a+ b+ c+ dr"), "d"); w != "" {
		t.Errorf("a replica: %q", w)
	}
}
