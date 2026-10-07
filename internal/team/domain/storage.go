package domain

import (
	"fmt"
	"sort"
	"strings"
)

// A workspace's data is held by its Workspace Hosts. These are the rules about how many of them there are
// and what that means, written once, in the workspace's own words, so that every status the API gives and
// every refusal it makes says the same honest thing. Raft's arithmetic is rqlite's; what is here is what a
// person should be told about it.

// TopologyLevel says how a number of voting hosts should be described.
type TopologyLevel string

const (
	// TopologySingle: one host. Valid, and no fault tolerance.
	TopologySingle TopologyLevel = "single"
	// TopologyNoQuorum: two hosts. Two copies, and neither can be lost without stopping writes.
	TopologyTwo TopologyLevel = "two_hosts"
	// TopologyRecommended: three hosts, which tolerate the loss of one.
	TopologyRecommended TopologyLevel = "recommended"
	// TopologyAdvanced: five hosts, which tolerate the loss of two.
	TopologyAdvanced TopologyLevel = "advanced"
	// TopologyEven: an even number of four or more, which is no more tolerant than one fewer.
	TopologyEven TopologyLevel = "even"
	// TopologyLarge: an odd number above five.
	TopologyLarge TopologyLevel = "large"
	// TopologyNone: no voting host at all (an empty workspace, or a status that could not be read).
	TopologyNone TopologyLevel = "none"
)

// Topology describes a set of Workspace Hosts that vote.
type Topology struct {
	// Voters is how many hosts vote; NonVoters how many hold a copy without voting (read-only replicas,
	// which include a host that is being added and has not yet caught up).
	Voters    int `json:"voters"`
	NonVoters int `json:"nonVoters"`
	// Quorum is how many voters must be up for a write to be safe: a majority.
	Quorum int `json:"quorum"`
	// FaultTolerance is how many voters can be lost with writes continuing.
	FaultTolerance int `json:"faultTolerance"`
	// ReachableVoters is how many voters answer now.
	ReachableVoters int `json:"reachableVoters"`
	// Writable is whether a quorum of voters is reachable: whether authoritative writes can happen.
	Writable bool          `json:"writable"`
	Level    TopologyLevel `json:"level"`
	// HighAvailability is whether the loss of one host leaves the workspace writable.
	HighAvailability bool `json:"highAvailability"`
	// Summary is what to tell a person: what this arrangement gives, and what it does not.
	Summary string `json:"summary"`
	// Recommendation is what to do to improve it, if anything.
	Recommendation string `json:"recommendation,omitempty"`
}

// QuorumOf is the majority of n voters.
func QuorumOf(n int) int {
	if n <= 0 {
		return 0
	}
	return n/2 + 1
}

// DescribeTopology describes a set of voting hosts, of which reachable answer.
func DescribeTopology(voters, nonVoters, reachable int) Topology {
	t := Topology{Voters: voters, NonVoters: nonVoters, Quorum: QuorumOf(voters), ReachableVoters: reachable}
	if voters > 0 {
		t.FaultTolerance = voters - t.Quorum
	}
	t.Writable = voters > 0 && reachable >= t.Quorum
	t.HighAvailability = t.FaultTolerance >= 1
	switch {
	case voters == 0:
		t.Level = TopologyNone
		t.Summary = "This workspace has no voting Workspace Host."
	case voters == 1:
		t.Level = TopologySingle
		t.Summary = "One Workspace Host: valid, but with no high availability. If this host is down the workspace cannot be used, and if its disk is lost so is the data, except for backups."
		t.Recommendation = "Add two more Workspace Hosts (three in all) so that the loss of one does not stop the workspace."
	case voters == 2:
		t.Level = TopologyTwo
		t.Summary = "Two Workspace Hosts: two copies of the data, but a write needs both of them, so the loss of either leaves no safe way to write. This is no more available than one host, and only safer for the data itself."
		t.Recommendation = "Add a third Workspace Host: three tolerate the loss of one."
	case voters == 3:
		t.Level = TopologyRecommended
		t.Summary = "Three Workspace Hosts, the recommended arrangement: any two can serve, so the workspace keeps working, for reading and writing, when one host is lost."
	case voters == 5:
		t.Level = TopologyAdvanced
		t.Summary = "Five Workspace Hosts: the workspace keeps working when any two are lost. This is for teams that need it; it costs more machines to keep healthy and every write waits for three of them."
	case voters%2 == 0:
		t.Level = TopologyEven
		t.Summary = fmt.Sprintf("%d Workspace Hosts vote: a write needs %d of them, so the workspace tolerates the loss of %d, which %d hosts would tolerate too.", voters, t.Quorum, t.FaultTolerance, voters-1)
		t.Recommendation = fmt.Sprintf("Use %d or %d voting hosts: an even number adds a machine to keep healthy and no resilience.", voters-1, voters+1)
	default:
		t.Level = TopologyLarge
		t.Summary = fmt.Sprintf("%d Workspace Hosts vote: a write needs %d of them, and the workspace tolerates the loss of %d. Every write waits for that many hosts to confirm it.", voters, t.Quorum, t.FaultTolerance)
	}
	if voters > 0 && !t.Writable {
		t.Summary += fmt.Sprintf(" Right now %d of %d voting hosts answer and %d are needed, so the workspace is READ-ONLY: nothing is accepted until enough of them are back, because writes accepted on a minority could be lost or disagree.", reachable, voters, t.Quorum)
	}
	return t
}

// StorageHost is one Workspace Host's copy of the workspace, as the cluster reports it.
type StorageHost struct {
	// NodeID is the host's node in the cluster: its device's ID.
	NodeID    string `json:"nodeId"`
	Voter     bool   `json:"voter"`
	Reachable bool   `json:"reachable"`
	Leader    bool   `json:"leader"`
	Error     string `json:"error,omitempty"`
}

// CheckRemoval says whether taking host id out of the cluster's voters leaves it able to write, and if
// it would not, why. A removal changes who votes; the cluster must be able to make the change (a quorum
// is reachable now), and afterwards a quorum of the voters that remain must answer, or the workspace would
// be left without one and could not be written to again without a manual recovery that can lose data.
// A host that does not vote (a read-only replica) can always be removed from a working cluster.
func CheckRemoval(hosts []StorageHost, id string) error {
	var target *StorageHost
	voters, reachable := 0, 0
	for i := range hosts {
		if hosts[i].Voter {
			voters++
			if hosts[i].Reachable {
				reachable++
			}
		}
		if hosts[i].NodeID == id {
			target = &hosts[i]
		}
	}
	if target == nil {
		return fmt.Errorf("%w: %s is not a member of the workspace's cluster", ErrNotFound, id)
	}
	if reachable < QuorumOf(voters) {
		return fmt.Errorf("%w: only %d of the %d voting Workspace Hosts answer and %d are needed, so the cluster cannot change its membership safely; bring a host back first", ErrConflict, reachable, voters, QuorumOf(voters))
	}
	if !target.Voter {
		return nil
	}
	if voters == 1 {
		return fmt.Errorf("%w: this is the workspace's only voting Workspace Host: removing it would remove the workspace's data; add another host first", ErrConflict)
	}
	remaining, remainingUp := voters-1, reachable
	if target.Reachable {
		remainingUp--
	}
	if remainingUp < QuorumOf(remaining) {
		return fmt.Errorf("%w: removing %s would leave %d voting hosts of which only %d answer, and %d are needed: the workspace could not be written to; bring a host back first", ErrConflict, id, remaining, remainingUp, QuorumOf(remaining))
	}
	return nil
}

// RemovalWarning is what to tell an administrator about the arrangement a removal leaves, when it is a
// worse one; empty when it is not.
func RemovalWarning(hosts []StorageHost, id string) string {
	voters, isVoter := 0, false
	for _, h := range hosts {
		if h.Voter {
			voters++
			if h.NodeID == id {
				isVoter = true
			}
		}
	}
	if !isVoter {
		return ""
	}
	left := DescribeTopology(voters-1, 0, voters-1)
	switch {
	case voters-1 <= 0:
		return ""
	case left.FaultTolerance < DescribeTopology(voters, 0, voters).FaultTolerance:
		return fmt.Sprintf("After this the workspace has %d voting Workspace Host(s) and tolerates the loss of %d. %s", left.Voters, left.FaultTolerance, left.Recommendation)
	}
	return ""
}

// SortStorageHosts orders hosts by node ID.
func SortStorageHosts(h []StorageHost) {
	sort.Slice(h, func(i, j int) bool { return strings.Compare(h[i].NodeID, h[j].NodeID) < 0 })
}

// StorageState is where a workspace's data is kept.
type StorageState string

const (
	// StorageReplicated: in a cluster of Workspace Hosts (of one or more), each holding a copy.
	StorageReplicated StorageState = "replicated"
	// StorageSingleFile: in one SQLite file on one host, which is not replicated and is the way
	// Team stored its data before replication: valid for evaluation and for a workspace that has not
	// been moved to the cluster yet.
	StorageSingleFile StorageState = "single_file"
)

// StorageStatus is the workspace's storage as the API reports it. It holds no secret and no path.
type StorageStatus struct {
	State StorageState `json:"state"`
	// Writable is false when the workspace is read-only for want of a quorum; Reason says why.
	Writable bool   `json:"writable"`
	ReadOnly bool   `json:"readOnly"`
	Reason   string `json:"reason,omitempty"`
	// Topology describes the voting hosts. Empty for single_file storage.
	Topology Topology      `json:"topology"`
	Hosts    []StorageHost `json:"hosts"`
	// Leader is the node that currently takes writes.
	Leader string `json:"leader,omitempty"`
	// SchemaVersion is the schema the workspace's data has.
	SchemaVersion int `json:"schemaVersion"`
	// This host's copy: the position of the cluster's history it has applied, and how stale it may be.
	Local LocalCopy `json:"local"`
	// Backup describes the latest backup, if backups are configured.
	Backup *BackupStatus `json:"backup,omitempty"`
	// Program is this host's database program: which release it is and whether it is running.
	Program *DatabaseProgram `json:"program,omitempty"`
	// Notes are things worth an administrator's attention.
	Notes []string `json:"notes,omitempty"`
}

// DatabaseProgram describes the database program a host runs.
type DatabaseProgram struct {
	// Version is the release Team ships and checked the program is; State whether it runs.
	Version string `json:"version"`
	State   string `json:"state"`
	// HashPinned says whether the program was checked against a hash pinned in this build (a published release) or only
	// against its own report of the pinned version and commit and the record of the build (a program built from the pinned source).
	HashPinned bool `json:"hashPinned"`
}

// LocalCopy is this host's own copy of the workspace.
type LocalCopy struct {
	// Position is how many writes this copy has applied; two hosts at the same position hold the same data.
	Position int64 `json:"position"`
	// BehindMillis is how long ago this copy last confirmed it was current; large while the cluster cannot be reached.
	BehindMillis int64 `json:"behindMillis"`
}

// BackupStatus describes a workspace's backups.
type BackupStatus struct {
	Configured  bool   `json:"configured"`
	Destination string `json:"destination,omitempty"`
	LastAt      int64  `json:"lastAt,omitempty"`
	LastOK      bool   `json:"lastOk"`
	LastError   string `json:"lastError,omitempty"`
	Count       int    `json:"count"`
	// VerifiedAt is when the latest backup was last opened and checked.
	VerifiedAt int64 `json:"verifiedAt,omitempty"`
}
