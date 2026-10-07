package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"devboard/internal/team/domain"
)

// fakeStorage stands in for the cluster, so that these tests are about the workspace's rules for hosts and not about
// rqlite (which is tested where it is, with real clusters, in internal/team/store/replicated and internal/team/server).
type fakeStorage struct {
	mu       sync.Mutex
	hosts    []domain.StorageHost
	planned  []HostToAdd
	removed  []string
	failWith error
	warning  string
}

func (f *fakeStorage) Status(context.Context) domain.StorageStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	voters := 0
	for _, h := range f.hosts {
		if h.Voter {
			voters++
		}
	}
	return domain.StorageStatus{State: domain.StorageReplicated, Writable: true, Topology: domain.DescribeTopology(voters, len(f.hosts)-voters, voters), Hosts: f.hosts}
}

func (f *fakeStorage) PlanHost(_ context.Context, h HostToAdd) (StoragePlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return StoragePlan{}, f.failWith
	}
	f.planned = append(f.planned, h)
	return StoragePlan{ClusterID: "cl", AppPassword: "a", AdminPassword: "b", NodePassword: "c", NodeID: h.NodeID, HTTPAddr: h.OverlayAddr + ":4001", RaftAddr: h.OverlayAddr + ":4002", JoinRaft: []string{"10.0.0.1:4002"}}, nil
}

func (f *fakeStorage) RemoveHost(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return "", f.failWith
	}
	f.removed = append(f.removed, id)
	return f.warning, nil
}

func (f *fakeStorage) Backup(context.Context) (domain.BackupStatus, error) {
	return domain.BackupStatus{Configured: true, Destination: "/b", Count: 1, LastOK: true}, nil
}

func withStorage(t *testing.T) (*netWorld, *fakeStorage) {
	t.Helper()
	n := withNetwork(t)
	fs := &fakeStorage{hosts: []domain.StorageHost{{NodeID: "first", Voter: true, Reachable: true, Leader: true}}}
	n.svc.SetStorage(fs)
	return n, fs
}

func hostDevice(t *testing.T, n *netWorld, name string) (id string, token string) {
	t.Helper()
	r := n.invite(n.owner, EnrollInviteInput{ForMemberID: n.owner.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
	resp := n.mustJoin(r, laptop(t, name), "")
	return resp.DeviceID, resp.DeviceToken
}

func TestAddingAHostMakesTheClusterReadyAndSealsWhatTheNodeNeedsToTheDevice(t *testing.T) {
	n, fs := withStorage(t)
	id, token := hostDevice(t, n, "second")
	a := n.deviceActor(token)
	if err := n.svc.ProvisionHost(bg, n.owner, id); err != nil {
		t.Fatal(err)
	}
	if len(fs.planned) != 1 || fs.planned[0].NodeID != id || fs.planned[0].OverlayAddr == "" {
		t.Fatalf("the cluster was not asked to plan the host: %+v", fs.planned)
	}
	// The existing hosts, and only they, are what the new one joins through.
	if len(fs.planned[0].ExistingHosts) != 1 {
		t.Errorf("existing hosts: %v", fs.planned[0].ExistingHosts)
	}
	blob, err := n.svc.CollectProvision(bg, a)
	if err != nil || !strings.Contains(string(blob), ":storage:"+id+":") || !strings.Contains(string(blob), "join=10.0.0.1:4002") {
		t.Fatalf("what was sealed to the device: %q %v", blob, err)
	}
	// Collecting the keys does not make it an active host: its node has still to join the cluster and be checked.
	if err := n.svc.AcknowledgeProvision(bg, a); err != nil {
		t.Fatal(err)
	}
	dev, _ := n.svc.GetDevice(bg, n.owner, id)
	if dev.HostStatus != domain.HostJoining {
		t.Fatalf("a host whose database has not joined yet is %s", dev.HostStatus)
	}
	// The host says so itself when it has joined, caught up and been checked, and not before.
	if err := n.svc.ActivateLocalHost(bg, n.owner.Workspace.ID, id); err != nil {
		t.Fatal(err)
	}
	dev, _ = n.svc.GetDevice(bg, n.owner, id)
	if dev.HostStatus != domain.HostActive {
		t.Fatalf("%s", dev.HostStatus)
	}
	// Activating is repeatable, and does nothing to a device that is not a host.
	if err := n.svc.ActivateLocalHost(bg, n.owner.Workspace.ID, id); err != nil {
		t.Fatal(err)
	}
}

func TestAHostIsNotAddedWhenTheClusterCannotTakeIt(t *testing.T) {
	n, fs := withStorage(t)
	id, token := hostDevice(t, n, "second")
	fs.failWith = errors.New("no quorum")
	if err := n.svc.ProvisionHost(bg, n.owner, id); err == nil {
		t.Fatal("a host was provisioned while the cluster could not take it")
	}
	if _, err := n.svc.CollectProvision(bg, n.deviceActor(token)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("something was left for the device: %v", err)
	}
}

func TestRemovingAHostChecksFirstThenTakesTheRoleAndLeavesTheDeviceBe(t *testing.T) {
	n, fs := withStorage(t)
	id, _ := hostDevice(t, n, "second")
	if err := n.svc.ProvisionHost(bg, n.owner, id); err != nil {
		t.Fatal(err)
	}
	bo, _ := n.member(n.owner, "Bo")
	if _, err := n.svc.RemoveWorkspaceHost(bg, bo, id); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member removed a host: %v", err)
	}
	// The cluster refuses (it would leave no quorum): the device keeps its role and what is waiting for it.
	fs.failWith = domain.ErrConflict
	if _, err := n.svc.RemoveWorkspaceHost(bg, n.owner, id); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("%v", err)
	}
	dev, _ := n.svc.GetDevice(bg, n.owner, id)
	if !dev.Has(domain.CapabilityWorkspaceHost) {
		t.Fatal("a refused removal took the role")
	}
	fs.failWith, fs.warning = nil, "After this the workspace has 2 voting Workspace Host(s)"
	out, err := n.svc.RemoveWorkspaceHost(bg, n.owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if out.Warning != fs.warning || out.Device.Has(domain.CapabilityWorkspaceHost) || out.Device.HostStatus != domain.HostNone || out.Device.Revoked() {
		t.Fatalf("%+v", out)
	}
	if len(fs.removed) != 1 || fs.removed[0] != id {
		t.Fatalf("%v", fs.removed)
	}
	// It is not a host any more, so it cannot be removed again as one.
	if _, err := n.svc.RemoveWorkspaceHost(bg, n.owner, id); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("%v", err)
	}
}

func TestADeviceThatHoldsTheDataCannotBeRevokedBeforeItIsRemoved(t *testing.T) {
	n, fs := withStorage(t)
	id, _ := hostDevice(t, n, "second")
	if err := n.svc.ProvisionHost(bg, n.owner, id); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.RevokeDevice(bg, n.owner, id); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a cluster member was revoked without being removed first: %v", err)
	}
	if _, err := n.svc.RemoveWorkspaceHost(bg, n.owner, id); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.RevokeDevice(bg, n.owner, id); err != nil {
		t.Fatalf("after the removal: %v", err)
	}
	_ = fs
}

func TestWithoutAClusterTheHostOperationsSaySoAndTheStatusIsHonest(t *testing.T) {
	n := withNetwork(t) // one file
	if _, err := n.svc.RemoveWorkspaceHost(bg, n.owner, "tdv_x"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("%v", err)
	}
	if _, err := n.svc.StorageBackup(bg, n.owner); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("%v", err)
	}
	st, err := n.svc.StorageStatus(bg, n.owner)
	if err != nil || st.State != domain.StorageSingleFile || len(st.Notes) == 0 || !strings.Contains(st.Notes[0], "no copy but its backups") {
		t.Fatalf("%+v %v", st, err)
	}
	// The brief answer anyone may have holds nothing about hosts.
	if b := n.svc.StorageBriefStatus(bg); b.ReadOnly || b.State != domain.StorageSingleFile {
		t.Fatalf("%+v", b)
	}
	// Only someone who sees devices sees the report.
	bo, _ := n.member(n.owner, "Bo")
	if _, err := n.svc.StorageStatus(bg, bo); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member saw where the data is kept: %v", err)
	}
}

func TestTheStorageReportAndBackupAreForAdministrators(t *testing.T) {
	n, _ := withStorage(t)
	bo, _ := n.member(n.owner, "Bo")
	if _, err := n.svc.StorageBackup(bg, bo); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member backed up the workspace: %v", err)
	}
	b, err := n.svc.StorageBackup(bg, n.owner)
	if err != nil || b.Count != 1 {
		t.Fatalf("%+v %v", b, err)
	}
	st, err := n.svc.StorageStatus(bg, n.owner)
	if err != nil || st.Topology.Level != domain.TopologySingle {
		t.Fatalf("%+v %v", st, err)
	}
}
