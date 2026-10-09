package domain

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func host(id, name string, online bool, caps ...Capability) Device {
	d := Device{ID: id, Name: name, MemberID: "tmb_1", Capabilities: caps, HostStatus: HostNone, ConnectivityStatus: HostNone}
	for _, c := range caps {
		switch c {
		case CapabilityWorkspaceHost:
			d.HostStatus = HostActive
		case CapabilityConnectivityHost:
			d.ConnectivityStatus = HostActive
		}
	}
	if online {
		seen := t0.Add(-10 * time.Second)
		d.LastSeenAt = &seen
	}
	return d
}

func cluster(voters, reachable int, backup *BackupStatus) StorageStatus {
	st := StorageStatus{State: StorageReplicated, Writable: reachable >= QuorumOf(voters), Topology: DescribeTopology(voters, 0, reachable), Backup: backup}
	st.ReadOnly = !st.Writable
	for i := 0; i < voters; i++ {
		st.Hosts = append(st.Hosts, StorageHost{NodeID: "dev_" + string(rune('a'+i)), Voter: true, Reachable: i < reachable})
	}
	return st
}

func goodBackup() *BackupStatus {
	return &BackupStatus{Configured: true, LastAt: t0.Add(-3 * time.Hour).UnixMilli(), LastOK: true, Count: 5}
}

func TestHostRemovalAdviceUsesLiveClusterCopies(t *testing.T) {
	cases := []struct {
		name    string
		storage StorageStatus
		target  string
		allowed bool
	}{
		{"only host", cluster(1, 1, nil), "dev_a", false},
		{"three healthy hosts", cluster(3, 3, nil), "dev_a", true},
		{"removing an online host leaves too few", cluster(3, 2, nil), "dev_a", false},
		{"removing the offline host keeps both copies", cluster(3, 2, nil), "dev_c", true},
		{"copies cannot be checked", StorageStatus{State: StorageReplicated}, "dev_a", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := AssessResilience(ResilienceInput{Now: t0, Storage: c.storage, Devices: []Device{host(c.target, "Host", true, CapabilityWorkspaceHost)}})
			check := r.Devices[0].Removal
			if check == nil || check.Allowed != c.allowed || (!c.allowed && check.Reason == "") {
				t.Fatalf("unsafe or unexplained removal preview: %+v", check)
			}
		})
	}
}

func net(remote string) *NetworkHealth {
	return &NetworkHealth{RemoteAccess: remote, Hosts: []HostNetworkStatus{{Discovery: true}}}
}

func titles(r Resilience) []string {
	var out []string
	for _, a := range r.Advice {
		out = append(out, a.Title)
	}
	return out
}

func hasAdvice(r Resilience, code string) *Advice {
	for i := range r.Advice {
		if r.Advice[i].Code == code {
			return &r.Advice[i]
		}
	}
	return nil
}

// The healthy case: three hosts up, two Connectivity Hosts confirmed, backups current. Nothing to do.
func TestAHealthyWorkspaceHasNothingToFix(t *testing.T) {
	devs := []Device{
		host("dev_a", "Mac mini", true, CapabilityWorkspaceHost, CapabilityConnectivityHost),
		host("dev_b", "Office server", true, CapabilityWorkspaceHost, CapabilityConnectivityHost),
		host("dev_c", "Desktop", true, CapabilityWorkspaceHost, CapabilityRunner),
	}
	desk := DeviceProfile{DeviceID: "dev_a", Platform: "darwin", Form: FormDesktop}
	r := AssessResilience(ResilienceInput{Now: t0, Devices: devs, Storage: cluster(3, 3, goodBackup()), Network: net(RemoteAccessAvailable),
		Profiles: map[string]DeviceProfile{"dev_a": desk, "dev_b": {DeviceID: "dev_b", Platform: "linux", Form: FormServer}, "dev_c": {DeviceID: "dev_c", Platform: "darwin", Form: FormDesktop}}})
	want := map[string]string{"Hosts": "3/3 online", "Quorum": "healthy", "Database": "healthy", "Connectivity Hosts": "2", "Remote access": "available", "Backups": "current"}
	for _, c := range []Check{r.Hosts, r.Quorum, r.Database, r.Connectivity, r.RemoteAccess, r.Backups} {
		if want[c.Label] != c.Value || c.State != CheckOK {
			t.Errorf("%s: %q (%s), want %q ok", c.Label, c.Value, c.State, want[c.Label])
		}
	}
	if len(r.Advice) != 0 || r.Level != CheckOK || r.Headline != "The workspace is resilient." {
		t.Fatalf("a healthy workspace has advice or is not ok: %v / %s / %s", titles(r), r.Level, r.Headline)
	}
}

// The warnings an administrator is shown, in the words that were asked for.
func TestEachWarningSaysWhatIsWrongAndWhatToDo(t *testing.T) {
	oneHost := []Device{host("dev_a", "Mini", true, CapabilityWorkspaceHost)}
	threeHosts := []Device{host("dev_a", "A", true, CapabilityWorkspaceHost), host("dev_b", "B", false, CapabilityWorkspaceHost), host("dev_c", "C", false, CapabilityWorkspaceHost)}
	cases := []struct {
		name     string
		in       ResilienceInput
		code     string
		title    string
		action   string
		severity Severity
	}{
		{"one host", ResilienceInput{Devices: oneHost, Storage: cluster(1, 1, goodBackup()), Network: net(RemoteAccessAvailable)},
			AdviceCodeOneHost, "Only one Workspace Host is configured.", "add_hosts", SeverityWarning},
		{"two hosts offline", ResilienceInput{Devices: threeHosts, Storage: cluster(3, 1, goodBackup()), Network: net(RemoteAccessAvailable)},
			AdviceCodeReadOnly, "Two hosts are offline. Workspace is read-only until quorum returns.", "bring_hosts_back", SeverityCritical},
		{"one host offline", ResilienceInput{Devices: threeHosts, Storage: cluster(3, 2, goodBackup()), Network: net(RemoteAccessAvailable)},
			AdviceCodeHostsOffline, "One Workspace Host is offline.", "bring_hosts_back", SeverityWarning},
		{"no Connectivity Host", ResilienceInput{Devices: oneHost, Storage: cluster(1, 1, goodBackup()), Network: net(RemoteAccessNotGuaranteed)},
			AdviceCodeNoConnectivity, "No Connectivity Host is reachable from outside your network. Local use works, but remote access cannot be guaranteed.", "add_connectivity_host", SeverityWarning},
		{"no backups", ResilienceInput{Devices: oneHost, Storage: cluster(1, 1, nil), Network: net(RemoteAccessAvailable)},
			AdviceCodeNoBackups, "Backups are not set up.", "set_up_backups", SeverityWarning},
		{"a failed backup", ResilienceInput{Devices: oneHost, Storage: cluster(1, 1, &BackupStatus{Configured: true, LastAt: t0.UnixMilli(), LastOK: false, LastError: "disk full"}), Network: net(RemoteAccessAvailable)},
			AdviceCodeBackupsFailed, "The last backup failed.", "back_up_now", SeverityCritical},
		{"a stale backup", ResilienceInput{Devices: oneHost, Storage: cluster(1, 1, &BackupStatus{Configured: true, LastAt: t0.Add(-5 * 24 * time.Hour).UnixMilli(), LastOK: true}), Network: net(RemoteAccessAvailable)},
			AdviceCodeBackupsStale, "Backups are out of date.", "back_up_now", SeverityWarning},
		{"data in one file", ResilienceInput{Devices: oneHost, Storage: StorageStatus{State: StorageSingleFile, Writable: true}, Network: net(RemoteAccessAvailable)},
			AdviceCodeSingleFile, "The workspace's data is in one file on one computer.", "add_hosts", SeverityWarning},
	}
	for _, c := range cases {
		c.in.Now = t0
		r := AssessResilience(c.in)
		a := hasAdvice(r, c.code)
		if a == nil {
			t.Errorf("%s: no %s advice; got %v", c.name, c.code, titles(r))
			continue
		}
		if a.Title != c.title || a.Severity != c.severity {
			t.Errorf("%s: %q (%s), want %q (%s)", c.name, a.Title, a.Severity, c.title, c.severity)
		}
		if a.Action == nil || a.Action.Kind != c.action {
			t.Errorf("%s: action %+v, want %s: a warning must say what to do", c.name, a.Action, c.action)
		}
	}
}

// A workspace that cannot be written to is not described as healthy anywhere on the page.
func TestAReadOnlyWorkspaceIsNotCalledHealthy(t *testing.T) {
	devs := []Device{host("dev_a", "A", true, CapabilityWorkspaceHost), host("dev_b", "B", false, CapabilityWorkspaceHost), host("dev_c", "C", false, CapabilityWorkspaceHost)}
	r := AssessResilience(ResilienceInput{Now: t0, Devices: devs, Storage: cluster(3, 1, goodBackup()), Network: net(RemoteAccessAvailable)})
	if r.Writable || r.Level != CheckBad || r.Headline != "The workspace is read-only." {
		t.Fatalf("writable=%v level=%s headline=%q", r.Writable, r.Level, r.Headline)
	}
	if r.Quorum.State != CheckBad || r.Quorum.Value != "lost" || r.Database.Value != "read-only" {
		t.Fatalf("quorum %+v database %+v", r.Quorum, r.Database)
	}
	if r.Hosts.Value != "1/3 online" {
		t.Fatalf("hosts %q", r.Hosts.Value)
	}
	if r.Advice[0].Severity != SeverityCritical {
		t.Fatalf("the most urgent advice is not first: %v", titles(r))
	}
}

// A host that is a laptop or sleeps is called out by name; a desktop is not; a device that may not host is not offered.
func TestAHostThatSleepsIsNotIdeal(t *testing.T) {
	devs := []Device{host("dev_a", "Ada's MacBook", true, CapabilityWorkspaceHost), host("dev_b", "Mac mini", true, CapabilityWorkspaceHost), host("dev_c", "Server", true, CapabilityWorkspaceHost)}
	profiles := map[string]DeviceProfile{
		"dev_a": {DeviceID: "dev_a", Platform: "darwin", Form: FormLaptop, Sleeps: true, SleepEvents: 9},
		"dev_b": {DeviceID: "dev_b", Platform: "darwin", Form: FormDesktop},
		"dev_c": {DeviceID: "dev_c", Platform: "linux", Form: FormServer},
	}
	r := AssessResilience(ResilienceInput{Now: t0, Devices: devs, Storage: cluster(3, 3, goodBackup()), Network: net(RemoteAccessAvailable), Profiles: profiles})
	var sleeps []Advice
	for _, a := range r.Advice {
		if a.Code == AdviceCodeHostSleeps {
			sleeps = append(sleeps, a)
		}
	}
	if len(sleeps) != 1 || !strings.Contains(sleeps[0].Title, "Ada's MacBook") || !strings.Contains(sleeps[0].Title, "sleeps automatically") || sleeps[0].Action.DeviceID != "dev_a" {
		t.Fatalf("expected one warning about Ada's MacBook: %+v", sleeps)
	}
	for _, d := range r.Devices {
		switch d.DeviceID {
		case "dev_a":
			if d.HostFit.Ideal || d.HostFit.Reasons[0] != AdviceSleeps {
				t.Errorf("the laptop's own advice: %+v", d.HostFit)
			}
		case "dev_b", "dev_c":
			if !d.HostFit.Ideal {
				t.Errorf("%s should be ideal: %+v", d.Name, d.HostFit)
			}
		}
	}
	// What the device itself is told is exactly the sentence that was asked for.
	if AdviceSleeps != "This device sleeps automatically. It is not ideal as a Workspace Host." {
		t.Fatal("the sentence changed")
	}
}

func TestHostFit(t *testing.T) {
	cases := []struct {
		name            string
		p               *DeviceProfile
		possible, ideal bool
	}{
		{"nothing reported", nil, true, false},
		{"a Mac mini", &DeviceProfile{Platform: "darwin", Form: FormDesktop}, true, true},
		{"a Linux server", &DeviceProfile{Platform: "linux", Form: FormServer}, true, true},
		{"a laptop that does not sleep", &DeviceProfile{Platform: "darwin", Form: FormLaptop}, true, false},
		{"a desktop that sleeps", &DeviceProfile{Platform: "darwin", Form: FormDesktop, Sleeps: true}, true, false},
		{"nobody said", &DeviceProfile{Platform: "linux", Form: FormUnknown}, true, false},
		{"Windows", &DeviceProfile{Platform: "windows", Form: FormDesktop}, false, false},
		{"a Mac mini that already hosts another workspace", &DeviceProfile{Platform: "darwin", Form: FormDesktop, HostConflict: true}, false, false},
	}
	for _, c := range cases {
		got := HostFitOf(c.p)
		if got.Possible != c.possible || got.Ideal != c.ideal {
			t.Errorf("%s: %+v, want possible=%v ideal=%v", c.name, got, c.possible, c.ideal)
		}
		if !got.Ideal && len(got.Reasons) == 0 {
			t.Errorf("%s: not ideal and no reason given", c.name)
		}
	}
}

func TestAProfileIsValidated(t *testing.T) {
	good := DeviceProfile{Platform: "darwin", Form: FormDesktop, Version: "2.7.0"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]DeviceProfile{
		"a platform that is not one": {Platform: "beos", Form: FormDesktop},
		"a form that is not one":     {Platform: "linux", Form: "toaster"},
		"negative sleeps":            {Platform: "linux", Form: FormServer, SleepEvents: -1},
		"a version with a newline":   {Platform: "linux", Form: FormServer, Version: "1\n2"},
		"a long version":             {Platform: "linux", Form: FormServer, Version: strings.Repeat("x", 100)},
	} {
		if err := p.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestNumberWords(t *testing.T) {
	for n, w := range map[int]string{1: "one", 2: "two", 10: "ten", 11: "11"} {
		if NumberWord(n) != w {
			t.Errorf("%d = %q", n, NumberWord(n))
		}
	}
}
