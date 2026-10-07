package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Resilience is the workspace's health in the words an administrator needs, not the infrastructure's: how many
// hosts are up, whether the workspace can be written to, whether it can be reached from outside, whether it is
// backed up. Every warning in it says what is wrong and what to do about it.
//
// It is computed from facts the workspace already records (the registry, the devices' profiles, the database's own
// report, the network's checks), by one pure function (AssessResilience), so what an administrator is told can be
// tested without a network or a database. Nothing here contacts a device.

// CheckState is how a line of the report reads at a glance.
type CheckState string

const (
	// CheckOK: nothing to do.
	CheckOK CheckState = "ok"
	// CheckWarn: works, but weaker than it should be.
	CheckWarn CheckState = "warn"
	// CheckBad: something is broken.
	CheckBad CheckState = "bad"
	// CheckUnknown: nothing has been checked yet.
	CheckUnknown CheckState = "unknown"
)

// Check is one line of the report: "Hosts: 3/3 online".
type Check struct {
	Label string     `json:"label"`
	Value string     `json:"value"`
	State CheckState `json:"state"`
}

// Severity orders advice.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) rank() int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	}
	return 2
}

// Advice codes: stable names a screen can attach an action to.
const (
	AdviceCodeNoHost           = "no_workspace_host"
	AdviceCodeOneHost          = "one_workspace_host"
	AdviceCodeTwoHosts         = "two_workspace_hosts"
	AdviceCodeEvenHosts        = "even_workspace_hosts"
	AdviceCodeReadOnly         = "read_only"
	AdviceCodeHostsOffline     = "hosts_offline"
	AdviceCodeSingleFile       = "data_in_one_file"
	AdviceCodeNoNetwork        = "no_private_network"
	AdviceCodeNoConnectivity   = "no_connectivity_host"
	AdviceCodeConnectivityDown = "connectivity_not_reachable"
	AdviceCodeOneConnectivity  = "one_connectivity_host"
	AdviceCodeHostSleeps       = "host_sleeps"
	AdviceCodeNoBackups        = "backups_not_set_up"
	AdviceCodeBackupsStale     = "backups_stale"
	AdviceCodeBackupsFailed    = "backups_failed"
	AdviceCodeNoRunner         = "no_runner_online"
	AdviceCodeNoDiscovery      = "no_discovery_host"
)

// AdviceAction says what an administrator can do about a piece of advice, so a screen can offer it as a button.
// The workspace never does it for them.
type AdviceAction struct {
	// Kind is one of: add_hosts, add_connectivity_host, set_up_backups, review_device, bring_hosts_back, back_up_now.
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// DeviceID names the device the action is about, when it is about one.
	DeviceID string `json:"deviceId,omitempty"`
}

// Advice is one thing an administrator should know, in words.
type Advice struct {
	Code     string        `json:"code"`
	Severity Severity      `json:"severity"`
	Title    string        `json:"title"`
	Detail   string        `json:"detail,omitempty"`
	Action   *AdviceAction `json:"action,omitempty"`
}

// DeviceHealth is one device as the devices and hosts screens show it.
type DeviceHealth struct {
	DeviceID           string         `json:"deviceId"`
	Name               string         `json:"name"`
	OwnerID            string         `json:"ownerId"`
	Online             bool           `json:"online"`
	Capabilities       []Capability   `json:"capabilities"`
	HostStatus         HostStatus     `json:"hostStatus"`
	ConnectivityStatus HostStatus     `json:"connectivityStatus"`
	Profile            *DeviceProfile `json:"profile,omitempty"`
	// HostFit says whether it would make a good Workspace Host.
	HostFit HostFit `json:"hostFit"`
}

// HostCounts says how many Workspace Hosts there are and how many answer.
type HostCounts struct {
	Configured int `json:"configured"`
	Online     int `json:"online"`
}

// Resilience is the report.
type Resilience struct {
	// Headline is the whole state in one sentence.
	Headline string `json:"headline"`
	// Level is the worst state of any line: ok, warn or bad.
	Level CheckState `json:"level"`
	// The lines, in the order a person reads them.
	Hosts        Check `json:"hosts"`
	Quorum       Check `json:"quorum"`
	Database     Check `json:"database"`
	Connectivity Check `json:"connectivityHosts"`
	RemoteAccess Check `json:"remoteAccess"`
	Backups      Check `json:"backups"`
	Runners      Check `json:"runners"`
	// WorkspaceHosts are the counts behind the first line; ConnectivityHosts the number of Connectivity Hosts.
	WorkspaceHosts    HostCounts `json:"workspaceHosts"`
	ConnectivityHosts int        `json:"connectivityHostCount"`
	// Writable is whether the workspace accepts changes now.
	Writable bool `json:"writable"`
	// Advice is what to do, the most urgent first.
	Advice []Advice `json:"advice"`
	// Devices are the workspace's devices, hosts first.
	Devices []DeviceHealth `json:"devices"`
}

// ResilienceInput is what AssessResilience judges.
type ResilienceInput struct {
	Now      time.Time
	Devices  []Device
	Profiles map[string]DeviceProfile
	Storage  StorageStatus
	// Network is nil when the workspace has no private network.
	Network *NetworkHealth
}

// BackupFreshness is how old the newest good backup may be before it is called stale. Backups are taken every day by
// default, so two days means one missed.
const BackupFreshness = 48 * time.Hour

// AssessResilience turns what the workspace knows into the report.
func AssessResilience(in ResilienceInput) Resilience {
	r := Resilience{Writable: in.Storage.Writable || in.Storage.State == StorageSingleFile, Advice: []Advice{}, Devices: []DeviceHealth{}}
	now := in.Now

	// ---- the devices ----
	reachableNode := map[string]bool{}
	for _, h := range in.Storage.Hosts {
		if h.Reachable {
			reachableNode[h.NodeID] = true
		}
	}
	var runnersTotal, runnersOnline, connectivity int
	for _, d := range in.Devices {
		if d.Revoked() {
			continue
		}
		var prof *DeviceProfile
		if p, ok := in.Profiles[d.ID]; ok {
			p := p
			prof = &p
		}
		online := d.OnlineAt(now)
		dh := DeviceHealth{DeviceID: d.ID, Name: d.Name, OwnerID: d.MemberID, Online: online, Capabilities: d.Capabilities,
			HostStatus: d.HostStatus, ConnectivityStatus: d.ConnectivityStatus, Profile: prof, HostFit: HostFitOf(prof)}
		if dh.Capabilities == nil {
			dh.Capabilities = []Capability{}
		}
		r.Devices = append(r.Devices, dh)
		if d.Has(CapabilityWorkspaceHost) && d.HostStatus != HostNone {
			r.WorkspaceHosts.Configured++
			if online || reachableNode[d.ID] {
				r.WorkspaceHosts.Online++
			}
		}
		if d.Has(CapabilityConnectivityHost) && d.ConnectivityStatus != HostNone {
			connectivity++
		}
		if d.Has(CapabilityRunner) {
			runnersTotal++
			if online {
				runnersOnline++
			}
		}
	}
	r.ConnectivityHosts = connectivity
	sort.SliceStable(r.Devices, func(i, j int) bool {
		a, b := r.Devices[i], r.Devices[j]
		ra, rb := deviceRank(a), deviceRank(b)
		if ra != rb {
			return ra < rb
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	// ---- hosts, quorum, database ----
	hostLine := fmt.Sprintf("%d/%d online", r.WorkspaceHosts.Online, r.WorkspaceHosts.Configured)
	hostState := CheckOK
	switch {
	case r.WorkspaceHosts.Configured == 0:
		hostLine, hostState = "none", CheckBad
	case r.WorkspaceHosts.Online < r.WorkspaceHosts.Configured:
		hostState = CheckWarn
	}
	r.Hosts = Check{Label: "Hosts", Value: hostLine, State: hostState}

	voters := in.Storage.Topology.Voters
	if in.Storage.State == StorageSingleFile {
		voters = 0
	}
	switch {
	case in.Storage.State == StorageSingleFile:
		r.Quorum = Check{Label: "Quorum", Value: "not applicable: the data is in one file", State: CheckWarn}
		r.Database = Check{Label: "Database", Value: "one file, with no copy but its backups", State: CheckWarn}
	case in.Storage.ReadOnly || !in.Storage.Writable:
		r.Quorum = Check{Label: "Quorum", Value: "lost", State: CheckBad}
		r.Database = Check{Label: "Database", Value: "read-only", State: CheckBad}
	case voters == 1:
		r.Quorum = Check{Label: "Quorum", Value: "healthy, with no fault tolerance (one host)", State: CheckWarn}
		r.Database = Check{Label: "Database", Value: "healthy", State: CheckOK}
	case voters == 2:
		r.Quorum = Check{Label: "Quorum", Value: "healthy, with no fault tolerance (two hosts)", State: CheckWarn}
		r.Database = Check{Label: "Database", Value: "healthy", State: CheckOK}
	case in.Storage.Topology.ReachableVoters < voters:
		r.Quorum = Check{Label: "Quorum", Value: "healthy, with fewer hosts than usual", State: CheckWarn}
		r.Database = Check{Label: "Database", Value: "healthy", State: CheckOK}
	default:
		r.Quorum = Check{Label: "Quorum", Value: "healthy", State: CheckOK}
		r.Database = Check{Label: "Database", Value: "healthy", State: CheckOK}
	}

	// ---- connectivity ----
	connState := CheckOK
	switch {
	case connectivity == 0:
		connState = CheckWarn
	case connectivity == 1:
		connState = CheckWarn
	}
	r.Connectivity = Check{Label: "Connectivity Hosts", Value: fmt.Sprintf("%d", connectivity), State: connState}
	switch {
	case in.Network == nil:
		r.RemoteAccess = Check{Label: "Remote access", Value: "not available: the workspace has no private network", State: CheckWarn}
	case in.Network.RemoteAccess == RemoteAccessAvailable:
		r.RemoteAccess = Check{Label: "Remote access", Value: "available", State: CheckOK}
	default:
		r.RemoteAccess = Check{Label: "Remote access", Value: "not guaranteed", State: CheckWarn}
	}

	// ---- backups ----
	r.Backups = backupCheck(in.Storage.Backup, now)

	// ---- runners ----
	switch {
	case runnersTotal == 0:
		r.Runners = Check{Label: "Runners", Value: "none registered", State: CheckUnknown}
	case runnersOnline == 0:
		r.Runners = Check{Label: "Runners", Value: fmt.Sprintf("0/%d online", runnersTotal), State: CheckWarn}
	default:
		r.Runners = Check{Label: "Runners", Value: fmt.Sprintf("%d/%d online", runnersOnline, runnersTotal), State: CheckOK}
	}

	r.Advice = adviceFor(in, r, runnersTotal, runnersOnline)
	sort.SliceStable(r.Advice, func(i, j int) bool { return r.Advice[i].Severity.rank() < r.Advice[j].Severity.rank() })
	r.Level, r.Headline = headline(r)
	return r
}

func deviceRank(d DeviceHealth) int {
	switch {
	case containsCap(d.Capabilities, CapabilityWorkspaceHost) && d.HostStatus != HostNone:
		return 0
	case containsCap(d.Capabilities, CapabilityConnectivityHost) && d.ConnectivityStatus != HostNone:
		return 1
	}
	return 2
}

func containsCap(cs []Capability, c Capability) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

func backupCheck(b *BackupStatus, now time.Time) Check {
	switch {
	case b == nil || !b.Configured:
		return Check{Label: "Backups", Value: "not set up", State: CheckWarn}
	case b.LastAt == 0:
		return Check{Label: "Backups", Value: "none taken yet", State: CheckWarn}
	case !b.LastOK:
		return Check{Label: "Backups", Value: "the last backup failed", State: CheckBad}
	}
	age := now.Sub(time.UnixMilli(b.LastAt))
	if age > BackupFreshness {
		return Check{Label: "Backups", Value: "stale: the newest is " + roughAge(age) + " old", State: CheckWarn}
	}
	return Check{Label: "Backups", Value: "current", State: CheckOK}
}

func roughAge(d time.Duration) string {
	switch {
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

var numberWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

// NumberWord writes a small count as a word, so a warning reads as a sentence ("Two hosts are offline").
func NumberWord(n int) string {
	if n >= 0 && n < len(numberWords) {
		return numberWords[n]
	}
	return fmt.Sprintf("%d", n)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func adviceFor(in ResilienceInput, r Resilience, runnersTotal, runnersOnline int) []Advice {
	var out []Advice
	add := func(a Advice) { out = append(out, a) }

	// Hosts and data.
	switch {
	case in.Storage.State == StorageSingleFile:
		add(Advice{Code: AdviceCodeSingleFile, Severity: SeverityWarning, Title: "The workspace's data is in one file on one computer.",
			Detail: "It has no copy but its backups, and it stops when that computer does. Move it into a group of Workspace Hosts.",
			Action: &AdviceAction{Kind: "add_hosts", Label: "Add Workspace Hosts"}})
	case r.WorkspaceHosts.Configured == 0:
		add(Advice{Code: AdviceCodeNoHost, Severity: SeverityCritical, Title: "No Workspace Host is configured.",
			Detail: "Nothing is keeping the workspace available.", Action: &AdviceAction{Kind: "add_hosts", Label: "Add a Workspace Host"}})
	case in.Storage.Topology.Voters == 1 || (in.Storage.State != StorageSingleFile && in.Storage.Topology.Voters == 0 && r.WorkspaceHosts.Configured == 1):
		add(Advice{Code: AdviceCodeOneHost, Severity: SeverityWarning, Title: "Only one Workspace Host is configured.",
			Detail: "If it is off, nobody can use the workspace, and if its disk is lost the data is gone except for backups. Add two more hosts (three in all) so the loss of one does not stop the workspace.",
			Action: &AdviceAction{Kind: "add_hosts", Label: "Add two more hosts"}})
	case in.Storage.Topology.Voters == 2:
		add(Advice{Code: AdviceCodeTwoHosts, Severity: SeverityWarning, Title: "Only two Workspace Hosts are configured.",
			Detail: "A change needs both of them, so losing either stops the workspace: two hosts are no more available than one. Add a third.",
			Action: &AdviceAction{Kind: "add_hosts", Label: "Add a third host"}})
	case in.Storage.Topology.Voters >= 4 && in.Storage.Topology.Voters%2 == 0:
		add(Advice{Code: AdviceCodeEvenHosts, Severity: SeverityInfo, Title: fmt.Sprintf("%s Workspace Hosts vote.", capitalise(NumberWord(in.Storage.Topology.Voters))),
			Detail: in.Storage.Topology.Recommendation})
	}

	// Hosts that are down.
	if in.Storage.State != StorageSingleFile && in.Storage.Topology.Voters > 0 {
		down := in.Storage.Topology.Voters - in.Storage.Topology.ReachableVoters
		switch {
		case in.Storage.ReadOnly || !in.Storage.Writable:
			title := fmt.Sprintf("%s %s offline. Workspace is read-only until quorum returns.", capitalise(NumberWord(maxInt(down, 1))), plural(maxInt(down, 1), "host is", "hosts are"))
			add(Advice{Code: AdviceCodeReadOnly, Severity: SeverityCritical, Title: title,
				Detail: "Everyone can still read the workspace, but nothing can be changed until enough hosts are back: changes taken on too few hosts could be lost or disagree. Switch the offline hosts on, and check their network.",
				Action: &AdviceAction{Kind: "bring_hosts_back", Label: "See which hosts are offline"}})
		case down > 0:
			add(Advice{Code: AdviceCodeHostsOffline, Severity: SeverityWarning,
				Title:  fmt.Sprintf("%s %s offline.", capitalise(NumberWord(down)), plural(down, "Workspace Host is", "Workspace Hosts are")),
				Detail: "The workspace still works, but it can lose fewer hosts than usual before it becomes read-only.",
				Action: &AdviceAction{Kind: "bring_hosts_back", Label: "See which hosts are offline"}})
		}
	}

	// Remote access.
	switch {
	case in.Network == nil:
		add(Advice{Code: AdviceCodeNoNetwork, Severity: SeverityInfo, Title: "The workspace has no private network.",
			Detail: "Devices can use it only from the network it is on."})
	case r.ConnectivityHosts == 0:
		add(Advice{Code: AdviceCodeNoConnectivity, Severity: SeverityWarning,
			Title:  "No Connectivity Host is reachable from outside your network. Local use works, but remote access cannot be guaranteed.",
			Detail: "A Connectivity Host is a machine of yours that stays on and can be reached from the Internet. It lets devices in different places find and reach each other. Two are better, so one can be down.",
			Action: &AdviceAction{Kind: "add_connectivity_host", Label: "Make a host a Connectivity Host"}})
	case in.Network.RemoteAccess != RemoteAccessAvailable:
		add(Advice{Code: AdviceCodeConnectivityDown, Severity: SeverityWarning,
			Title:  "No Connectivity Host is reachable from outside your network. Local use works, but remote access cannot be guaranteed.",
			Detail: "A host behind a home router, a company firewall or carrier-grade NAT may not be reachable from elsewhere even with a public-looking address. Check that its address is public and its port is open.",
			Action: &AdviceAction{Kind: "add_connectivity_host", Label: "Check your Connectivity Hosts"}})
	case r.ConnectivityHosts == 1:
		add(Advice{Code: AdviceCodeOneConnectivity, Severity: SeverityInfo, Title: "Only one Connectivity Host.",
			Detail: "If it is offline, devices in different places lose each other. Add a second.",
			Action: &AdviceAction{Kind: "add_connectivity_host", Label: "Add a second Connectivity Host"}})
	}
	if in.Network != nil && !hasDiscovery(in.Network.Hosts) {
		add(Advice{Code: AdviceCodeNoDiscovery, Severity: SeverityWarning, Title: "No host helps devices find each other.",
			Detail: "Say where a host can be reached from the other devices and let it help them find each other."})
	}

	// Hosts that are laptops, or sleep.
	for _, d := range r.Devices {
		isHost := containsCap(d.Capabilities, CapabilityWorkspaceHost) && d.HostStatus != HostNone
		isConn := containsCap(d.Capabilities, CapabilityConnectivityHost) && d.ConnectivityStatus != HostNone
		if !(isHost || isConn) || d.Profile == nil || d.HostFit.Ideal {
			continue
		}
		role := "Workspace Host"
		if !isHost {
			role = "Connectivity Host"
		}
		if !d.HostFit.Possible {
			continue
		}
		reason := AdviceSleeps
		if len(d.HostFit.Reasons) > 0 {
			reason = d.HostFit.Reasons[0]
		}
		add(Advice{Code: AdviceCodeHostSleeps, Severity: SeverityWarning,
			Title:  fmt.Sprintf("%s is a %s, and %s", d.Name, role, lowerFirst(reasonAbout(reason, role))),
			Detail: "A host that sleeps takes the workspace's availability down with it. A desktop computer, a Mac mini or a server that stays on is a better choice.",
			Action: &AdviceAction{Kind: "review_device", Label: "Review " + d.Name, DeviceID: d.DeviceID}})
	}

	// Backups.
	switch {
	case in.Storage.Backup == nil || !in.Storage.Backup.Configured:
		add(Advice{Code: AdviceCodeNoBackups, Severity: SeverityWarning, Title: "Backups are not set up.",
			Detail: "Hosts keep copies of each other's data, which protects against a machine failing but not against a mistake or a lost workspace. Choose a folder on a disk or share you own for backups.",
			Action: &AdviceAction{Kind: "set_up_backups", Label: "Set up backups"}})
	case r.Backups.State == CheckBad:
		add(Advice{Code: AdviceCodeBackupsFailed, Severity: SeverityCritical, Title: "The last backup failed.",
			Detail: strings.TrimSpace(in.Storage.Backup.LastError), Action: &AdviceAction{Kind: "back_up_now", Label: "Back up now"}})
	case r.Backups.State == CheckWarn:
		add(Advice{Code: AdviceCodeBackupsStale, Severity: SeverityWarning, Title: "Backups are out of date.", Detail: "The newest good backup is " + strings.TrimPrefix(r.Backups.Value, "stale: the newest is "),
			Action: &AdviceAction{Kind: "back_up_now", Label: "Back up now"}})
	}

	// Runners.
	if runnersTotal > 0 && runnersOnline == 0 {
		add(Advice{Code: AdviceCodeNoRunner, Severity: SeverityInfo, Title: "No runner is online.",
			Detail: "Runners do the work. Scheduled and remote work needs at least one runner to stay online: a computer that is on and running Werkbord."})
	}
	return out
}

func hasDiscovery(hosts []HostNetworkStatus) bool {
	for _, h := range hosts {
		if h.Discovery {
			return true
		}
	}
	return false
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// reasonAbout turns a device-centred reason ("This device sleeps automatically. It is not ideal as a Workspace Host.")
// into one about a named device, by dropping the sentence the name replaces.
func reasonAbout(reason, role string) string {
	switch reason {
	case AdviceSleeps:
		return "it sleeps automatically. That is not ideal for a " + role + "."
	case AdviceLaptop:
		return "it is a laptop. A laptop that travels or is shut is not ideal for a " + role + "."
	}
	return strings.TrimSpace(reason)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func headline(r Resilience) (CheckState, string) {
	level := CheckOK
	for _, c := range []Check{r.Hosts, r.Quorum, r.Database, r.RemoteAccess, r.Backups} {
		switch {
		case c.State == CheckBad:
			level = CheckBad
		case c.State == CheckWarn && level == CheckOK:
			level = CheckWarn
		}
	}
	for _, a := range r.Advice {
		if a.Severity == SeverityCritical {
			level = CheckBad
		}
	}
	switch level {
	case CheckBad:
		if !r.Writable {
			return level, "The workspace is read-only."
		}
		return level, "The workspace needs attention."
	case CheckWarn:
		return level, "The workspace works, and could be more resilient."
	}
	return level, "The workspace is resilient."
}
