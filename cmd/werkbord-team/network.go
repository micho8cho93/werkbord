package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"devboard/internal/enrollment"
	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/server"
)

// The commands for the workspace's private network. Those that run on the administrator's
// computer (status, invite, approve, deny, revoke, promote) are ordinary requests to the
// Team server's API with the administrator's token, and do nothing the console could not.
// Those that run on a host (join, collect, node) act on that host's own key vault and
// network files. Nothing here contacts anything but the workspace's own machines.

const networkUsage = `usage: werkbord-team network <command>

  status                       the network's hosts, what is missing, and whether remote access can be relied on
  invite [flags]               make an invitation (a link and a QR code) for a person's device, or for a new host
  pending                      list the devices waiting for approval
  approve <id> | deny <id>     decide about a waiting device
  approval auto|admin          whether a joining device waits for an administrator (default: admin)
  node                         run the network node of a host that joined but does not hold the workspace's data

The server is $WERKBORD_TEAM_SERVER (default http://127.0.0.1:7430); your token is $WERKBORD_TEAM_TOKEN.
Run "werkbord-team network <command> -h" for flags. See docs/TEAM_NETWORK.md.
`

type apiFlags struct {
	server string
	token  string
}

func (a *apiFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&a.server, "server", firstEnv("WERKBORD_TEAM_SERVER", "http://127.0.0.1:7430"), "the Team server")
	a.token = os.Getenv("WERKBORD_TEAM_TOKEN")
}

func (a *apiFlags) client() (base string, hc *http.Client, err error) {
	if a.token == "" {
		return "", nil, errors.New("set WERKBORD_TEAM_TOKEN to your Team token")
	}
	if base, err = parseBase(a.server); err != nil {
		return "", nil, fmt.Errorf("--server: %w", err)
	}
	return base, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func cmdNetwork(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, networkUsage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "status":
		return cmdNetworkStatus(ctx, args[1:], stdout, stderr)
	case "invite":
		return cmdNetworkInvite(ctx, args[1:], stdout, stderr)
	case "pending":
		return cmdNetworkPending(ctx, args[1:], stdout, stderr)
	case "approve", "deny":
		return cmdNetworkDecide(ctx, args[0], args[1:], stdout, stderr)
	case "approval":
		return cmdNetworkApproval(ctx, args[1:], stdout, stderr)
	case "node":
		return cmdNetworkNode(ctx, cfg, args[1:], stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, networkUsage)
		return nil
	}
	fmt.Fprint(stderr, networkUsage)
	return fmt.Errorf("unknown network command %q", args[0])
}

func cmdNetworkStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("network status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var h struct {
		domain.NetworkHealth
		Node map[string]any `json:"node"`
	}
	if err := doJSON(ctx, hc, http.MethodGet, base+"/api/team/v1/network", a.token, nil, &h); err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(h)
	}
	fmt.Fprintf(stdout, "Private network %s\n  workspace fingerprint  %s\n  enrollment approval    %s\n  revoked certificates   %d\n\n", h.Settings.NetworkPrefix, h.Settings.Fingerprint, h.Settings.EnrollmentApproval, h.RevokedCertificates)
	fmt.Fprintf(stdout, "Remote access: %s\n\n", strings.ReplaceAll(h.RemoteAccess, "_", " "))
	for _, host := range h.Hosts {
		roles := []string{}
		if host.WorkspaceHost {
			roles = append(roles, "Workspace Host")
		}
		if host.ConnectivityHost {
			roles = append(roles, "Connectivity Host")
		}
		if host.Discovery {
			roles = append(roles, "discovery")
		}
		if host.Relay {
			roles = append(roles, "relay")
		}
		fmt.Fprintf(stdout, "  %-24s %-8s %-14s %s\n", host.Name, onlineWord(host.Online), strings.ReplaceAll(string(host.Reachability), "_", " "), strings.Join(roles, ", "))
		for _, e := range host.Endpoints {
			fmt.Fprintf(stdout, "      %-32s %s\n", e.Endpoint, e.Kind)
		}
		if host.LastExternalOKAt != nil {
			fmt.Fprintf(stdout, "      last successful check from another device: %s\n", host.LastExternalOKAt.Local().Format(time.RFC3339))
		}
	}
	if len(h.Warnings) > 0 {
		fmt.Fprintln(stdout, "\nWhat to fix:")
		for _, w := range h.Warnings {
			fmt.Fprintf(stdout, "  - %s\n", w)
		}
	}
	if st, ok := h.Node["state"].(string); ok {
		fmt.Fprintf(stdout, "\nThis host's network node: %s", st)
		if r, ok := h.Node["lastError"].(string); ok && r != "" {
			fmt.Fprintf(stdout, " (%s)", r)
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func onlineWord(on bool) string {
	if on {
		return "online"
	}
	return "offline"
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, strings.Split(v, ",")...); return nil }

func cmdNetworkInvite(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("network invite", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	label := fs.String("label", "", "who the invitation is for (for the administrators' own reference)")
	forMember := fs.String("for-member", "", "add a device to this existing member (an ID) instead of making a new member")
	role := fs.String("role", "member", "what a new member becomes: member or admin (admin needs the owner)")
	hours := fs.Float64("hours", 24, "how long the invitation works")
	approval := fs.Bool("require-approval", false, "an administrator must approve the device even if the workspace does not require it")
	qr := fs.Bool("qr", false, "also print the invitation as a QR code")
	var caps listFlag
	fs.Var(&caps, "capability", "what the device will be: runner, workspace_host, connectivity_host (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var out struct {
		Invitation  domain.EnrollInvitation `json:"invitation"`
		Link        string                  `json:"link"`
		Fingerprint string                  `json:"fingerprint"`
	}
	body := map[string]any{"label": *label, "forMemberId": *forMember, "role": *role, "capabilities": []string(caps), "expiresInHours": *hours, "requireApproval": *approval}
	if err := doJSON(ctx, hc, http.MethodPost, base+"/api/team/v1/enrollment-invitations", a.token, body, &out); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Invitation %s, valid until %s, for one device.\n\n  %s\n\n", out.Invitation.ID, out.Invitation.ExpiresAt.Local().Format(time.RFC3339), out.Link)
	fmt.Fprintf(stdout, "Workspace fingerprint (read it out, so the person joining can check it is the workspace they expect):\n\n  %s\n\n", out.Fingerprint)
	if *qr {
		q, err := qrcode.New(out.Link, qrcode.Low)
		if err != nil {
			return fmt.Errorf("the invitation is too long for a QR code: %w", err)
		}
		fmt.Fprintln(stdout, q.ToSmallString(false))
	}
	fmt.Fprintln(stdout, "This link is the only copy: it is not stored. Anyone who has it can use it once, until it expires; give it only to the person it is for.")
	return nil
}

func cmdNetworkPending(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("network pending", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var list []domain.Enrollment
	if err := doJSON(ctx, hc, http.MethodGet, base+"/api/team/v1/enrollments", a.token, nil, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(stdout, "Nothing is waiting for approval.")
		return nil
	}
	for _, e := range list {
		fmt.Fprintf(stdout, "%s  %-20s device %q  key %s  from %s  %s\n", e.ID, e.MemberName, e.DeviceName, e.Fingerprint, e.RemoteAddr, e.CreatedAt.Local().Format(time.RFC3339))
	}
	fmt.Fprintln(stdout, "\nCompare the key fingerprint with what the person reads out from their own computer, then `werkbord-team network approve <id>` or `deny <id>`.")
	return nil
}

func cmdNetworkDecide(ctx context.Context, verb string, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("network "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: werkbord-team network %s <enrollment id>", verb)
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var e domain.Enrollment
	if err := doJSON(ctx, hc, http.MethodPost, base+"/api/team/v1/enrollments/"+pos[0]+"/"+verb, a.token, map[string]any{}, &e); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s (%s)\n", e.ID, e.State, e.DeviceName)
	return nil
}

func cmdNetworkApproval(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("network approval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || (pos[0] != "auto" && pos[0] != "admin") {
		return errors.New("usage: werkbord-team network approval auto|admin")
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	if err := doJSON(ctx, hc, http.MethodPut, base+"/api/team/v1/network/approval", a.token, map[string]any{"approval": pos[0]}, nil); err != nil {
		return err
	}
	if pos[0] == "admin" {
		fmt.Fprintln(stdout, "From now on every device that joins waits for an administrator (`werkbord-team network pending`).")
	} else {
		fmt.Fprintln(stdout, "From now on a valid invitation is enough; whoever made it already decided.")
	}
	return nil
}

// ---- device join, revoke, list ----

const deviceUsage = `usage: werkbord-team device <command>

  join <link> [flags]     make this computer a host of the workspace the link is for
  list                    the workspace's devices
  revoke <id>             end a device: its credential stops working at once and its network certificate is refused

A member's own computer joins with Werkbord (the individual product), which holds that device's own key; this command is
for the machines that run the workspace: Workspace Hosts and Connectivity Hosts.
`

func cmdDevice(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, deviceUsage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "join":
		return cmdDeviceJoin(ctx, cfg, args[1:], stdout, stderr)
	case "list":
		return cmdDeviceList(ctx, args[1:], stdout, stderr)
	case "revoke":
		return cmdDeviceRevoke(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, deviceUsage)
		return nil
	}
	fmt.Fprint(stderr, deviceUsage)
	return fmt.Errorf("unknown device command %q", args[0])
}

func cmdDeviceList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("device list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var ds []domain.Device
	if err := doJSON(ctx, hc, http.MethodGet, base+"/api/team/v1/devices", a.token, nil, &ds); err != nil {
		return err
	}
	for _, d := range ds {
		state := "active"
		if d.Revoked() {
			state = "revoked"
		}
		caps := []string{}
		for _, c := range d.Capabilities {
			caps = append(caps, string(c))
		}
		fmt.Fprintf(stdout, "%s  %-24s %-8s %s\n", d.ID, d.Name, state, strings.Join(caps, ","))
	}
	return nil
}

func cmdDeviceRevoke(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("device revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: werkbord-team device revoke <device id>")
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var d domain.Device
	if err := doJSON(ctx, hc, http.MethodPost, base+"/api/team/v1/devices/"+pos[0]+"/revoke", a.token, map[string]any{}, &d); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s (%s) is revoked. Its credential no longer works; the hosts add its certificate to the network's blocklist within a minute.\n", d.ID, d.Name)
	return nil
}

func cmdDeviceJoin(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	return cmdDeviceJoinAs(ctx, cfg, args, stdout, stderr, false)
}

func cmdDeviceJoinAs(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer, memberDevice bool) error {
	fs := flag.NewFlagSet("device join", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	name := fs.String("name", hostLabel(), "a name for this host")
	memberName := fs.String("member-name", "", "your member name (connector enrollment)")
	expect := fs.String("expect-fingerprint", "", "the workspace's fingerprint, as the person who invited you read it out (recommended)")
	wait := fs.Duration("wait", 10*time.Minute, "how long to wait for an administrator to approve, if the workspace requires it")
	passphrase := fs.String("passphrase-file", cfg.PassphraseFile, "seal this host's keys with the passphrase in this file instead of a key kept beside them")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: werkbord-team device join <werkbord://join/…> [flags]")
	}
	cfg.PassphraseFile = *passphrase
	inv, err := enrollment.Parse(pos[0], time.Now())
	if err != nil {
		return err
	}
	isHost := false
	for _, c := range inv.Capabilities {
		isHost = isHost || c == string(domain.CapabilityWorkspaceHost) || c == string(domain.CapabilityConnectivityHost)
	}
	if !isHost && !memberDevice {
		return errors.New("this invitation is for a member's own device: join it with Werkbord, which holds that device's own key. `werkbord-team device join` is for the machines that run the workspace (an invitation made with --capability workspace_host or connectivity_host)")
	}
	if memberDevice && isHost {
		return errors.New("a user connector requires a member device invitation, never a host invitation")
	}
	if memberDevice && *expect == "" {
		return errors.New("connector enrollment requires --expect-fingerprint")
	}
	if _, err := os.Stat(cfg.PKIDir()); err == nil {
		return fmt.Errorf("%s exists: this data directory already belongs to a workspace; use another --data-dir", cfg.PKIDir())
	}
	fmt.Fprintf(stdout, "Joining %q (fingerprint %s) through %s\n", inv.WorkspaceName, inv.Fingerprint, strings.Join(inv.Endpoints, ", "))
	if *expect == "" {
		fmt.Fprintln(stdout, "No --expect-fingerprint was given: this host will trust whichever workspace this invitation names. Compare the fingerprint above with the one the administrator reads out.")
	}
	sealer, err := server.SealerFor(cfg)
	if err != nil {
		return err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return err
	}
	keys, err := pki.NewHostKeys()
	if err != nil {
		return err
	}
	_, netPub, err := keys.NetworkKeyPEMs()
	if err != nil {
		return err
	}
	fail := func(err error) error { _ = os.RemoveAll(cfg.PKIDir()); return err }
	res, err := enrollment.Join(ctx, inv, enrollment.JoinParams{Signer: keys, DeviceName: *name, MemberName: *memberName, NetworkPublicKeyPEM: string(netPub), SealingPublicKey: keys.SealingPublicKey(),
		ExpectFingerprint: *expect, Wait: *wait})
	if err != nil {
		if errors.Is(err, enrollment.ErrPending) {
			return fail(fmt.Errorf("an administrator has not approved this host yet (run `werkbord-team network pending` there); the invitation is spent, so ask for a new one if you stop waiting: %w", err))
		}
		return fail(err)
	}
	meta := pki.Meta{WorkspaceID: inv.WorkspaceID, WorkspaceName: inv.WorkspaceName, Fingerprint: inv.Fingerprint}
	if p, err := netPrefixOf(res.Network.OverlayAddr, res.Network.Node); err == nil {
		meta.NetworkPrefix = p
	}
	if err := v.CreateJoined(meta, keys, []byte(res.Network.CACertificate)); err != nil {
		return fail(err)
	}
	if err := v.SaveSecret("member", []byte(res.Response.MemberID)); err != nil {
		return fail(err)
	}
	if err := v.SaveDeviceToken(res.Response.DeviceToken); err != nil {
		return fail(err)
	}
	if err := v.SaveJoinInfo(pki.JoinInfo{Endpoints: inv.Endpoints, APIAddrs: res.Network.APIAddrs, APIPort: apiPortOf(cfg), Node: res.Network.Node}); err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(cfg.NodeDir(), 0o700); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.NodeDir(), "node.crt"), []byte(res.Network.NodeCertificate), 0o600); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "\nThis host (%s) joined %q as device %s at %s on the private network.\n", *name, inv.WorkspaceName, res.Response.DeviceID, res.Network.OverlayAddr)
	fmt.Fprintln(stdout, "Its keys are sealed in", cfg.PKIDir())
	if memberDevice {
		fmt.Fprintln(stdout, "Add this user-owned device directory to your connector configuration. Run the Nebula network node separately with the required OS privileges; the connector itself needs none.")
		return nil
	}
	fmt.Fprintln(stdout, "\nNext: run `werkbord-team network node` to bring this host onto the network. To make it a Workspace Host that can take over the workspace's authority,")
	fmt.Fprintf(stdout, "an administrator runs `werkbord-team host promote %s`, then this host runs `werkbord-team host collect`.\n", res.Response.DeviceID)
	return nil
}

func hostLabel() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "workspace host"
}

func apiPortOf(cfg config.Config) int {
	_, p, err := net.SplitHostPort(cfg.Addr)
	n, _ := strconv.Atoi(p)
	if err != nil || n == 0 {
		return overlay.APIPort
	}
	return n
}

// netPrefixOf says the range of the private network, from the device's own address and the description it was sent.
func netPrefixOf(addr string, node json.RawMessage) (string, error) {
	c, err := server.DecodeNodeConfig(node)
	if err != nil {
		return "", err
	}
	a, err := parseAddr(addr)
	if err != nil {
		return "", err
	}
	p, err := a.Prefix(c.PrefixBits)
	return p.String(), err
}

// ---- hosts: handing on the authority ----

const hostUsage = `usage: werkbord-team host <command>

  promote <device id>     (an administrator) make a device that joined as a Workspace Host a holder of the workspace's keys and of a copy
                          of its data: seals the keys, and what its database node needs to join the cluster, to that device
  collect                 (on that host) collect and store what was sealed to this host; then run "werkbord-team serve" on it: its
                          database node joins the cluster, catches up, is checked, and only then becomes a voting host
  remove <device id>      (an administrator) take a Workspace Host out of the cluster with a checked membership change; refused when it
                          would leave the workspace without a quorum

A Workspace Host holds the workspace's signing key and the network authority's, and a copy of all its data: it is a high-trust machine.
Three hosts are recommended. See docs/TEAM_NETWORK.md and docs/TEAM_STORAGE.md.
`

func cmdHost(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, hostUsage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "promote":
		return cmdHostPromote(ctx, args[1:], stdout, stderr)
	case "remove":
		return cmdHostRemove(ctx, args[1:], stdout, stderr)
	case "collect":
		return cmdHostCollect(ctx, cfg, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, hostUsage)
		return nil
	}
	fmt.Fprint(stderr, hostUsage)
	return fmt.Errorf("unknown host command %q", args[0])
}

func cmdHostPromote(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("host promote", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: werkbord-team host promote <device id>")
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	if err := doJSON(ctx, hc, http.MethodPost, base+"/api/team/v1/devices/"+pos[0]+"/provision", a.token, map[string]any{}, nil); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "The workspace's keys, and what that host's database node needs to join the cluster, are sealed to %s and waiting for it. Run `werkbord-team host collect` on that host, then `werkbord-team serve`.\n", pos[0])
	return nil
}

func cmdHostRemove(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("host remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: werkbord-team host remove <device id>")
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	hc.Timeout = 3 * time.Minute
	var out struct {
		Device  domain.Device `json:"device"`
		Warning string        `json:"warning"`
	}
	if err := doJSON(ctx, hc, http.MethodDelete, base+"/api/team/v1/devices/"+pos[0]+"/replica", a.token, nil, &out); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s left the workspace's database cluster by a membership change and is no longer a Workspace Host. It is not revoked: it is still a device on the network. Its copy of the data stays on its disk, where it is yours to remove.\n", out.Device.Name)
	if out.Warning != "" {
		fmt.Fprintf(stdout, "\nNote: %s\n", out.Warning)
	}
	return nil
}

func cmdHostCollect(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("host collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	serverURL := fs.String("server", "", "a Workspace Host's API (default: one of the addresses this host was given when it joined, reached over the private network)")
	passphrase := fs.String("passphrase-file", cfg.PassphraseFile, "the passphrase file this host's keys are sealed with")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.PassphraseFile = *passphrase
	sealer, err := server.SealerFor(cfg)
	if err != nil {
		return err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return err
	}
	mat, err := v.Load(time.Now())
	if err != nil {
		return fmt.Errorf("opening this host's keys: %w", err)
	}
	if mat.Meta.Authority {
		return errors.New("this host already holds the workspace's keys")
	}
	token, err := v.DeviceToken()
	if err != nil {
		return err
	}
	info, err := v.JoinInfo()
	if err != nil {
		return err
	}
	var bases []string
	if *serverURL != "" {
		bases = []string{*serverURL}
	} else {
		for _, addr := range info.APIAddrs {
			bases = append(bases, "http://"+net.JoinHostPort(addr, strconv.Itoa(info.APIPort)))
		}
	}
	if len(bases) == 0 {
		return errors.New("this host does not know where a Workspace Host's API is: give --server")
	}
	member, err := v.Secret("member")
	if err != nil {
		return fmt.Errorf("this pre-gate host must re-enroll to obtain a device-bound API identity: %w", err)
	}
	prefix, err := netip.ParsePrefix(mat.Meta.NetworkPrefix)
	if err != nil {
		return err
	}
	hc, err := hostclient.New(hostclient.Options{Bases: bases, Token: token, Signer: mat.Host, WorkspaceID: mat.Meta.WorkspaceID, UserID: string(member), Network: prefix, Timeout: 10 * time.Second})
	if err != nil {
		return err
	}
	sealed, err := hc.CollectProvision(ctx)
	if err != nil {
		return fmt.Errorf("could not collect from any Workspace Host (is `werkbord-team network node` running on this host?): %w", err)
	}
	ca, err := v.CACertificate()
	if err != nil {
		return err
	}
	secrets, err := mat.Host.OpenSecrets(time.Now(), mat.Meta.WorkspaceID, mat.Meta.Fingerprint, string(ca), sealed)
	if err != nil {
		return fmt.Errorf("what was sent is not what this host enrolled with, and was not stored: %w", err)
	}
	if err := v.Promote(secrets, time.Now()); err != nil {
		return err
	}
	if err := hc.AckProvision(ctx); err != nil {
		fmt.Fprintf(stderr, "the keys are stored here, but telling the workspace failed (%v); run this again to try once more\n", err)
	}
	fmt.Fprintf(stdout, "This host now holds the workspace's keys (fingerprint %s). It is a high-trust machine: protect it as you would the first host.\n", mat.Meta.Fingerprint)
	if v.HasStorage() {
		fmt.Fprintln(stdout, "\nNext: run `werkbord-team serve` on this host. Its database node will join the workspace's cluster, take a full copy, be checked against the cluster's data, and only then vote; until it has, it is \"joining\" in `werkbord-team storage status`.")
	}
	return nil
}

// ---- running the node of a host that only joined ----

func cmdNetworkNode(ctx context.Context, cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("network node", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	passphrase := fs.String("passphrase-file", cfg.PassphraseFile, "the passphrase file this host's keys are sealed with")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.PassphraseFile = *passphrase
	log, err := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	sealer, err := server.SealerFor(cfg)
	if err != nil {
		return err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return err
	}
	src, err := newHTTPSource(v, cfg, log)
	if err != nil {
		return err
	}
	return server.RunDeviceNode(ctx, cfg, log, src)
}

// httpSource is where a host that only joined learns what its node should be: what it was sent when it joined, until
// it can ask a Workspace Host's API over the network, and after that what that says.
type httpSource struct {
	v     *pki.Vault
	cfg   config.Config
	log   *slog.Logger
	token string
	info  pki.JoinInfo
	hc    *hostclient.Client
	last  domain.NodeConfig
}

func newHTTPSource(v *pki.Vault, cfg config.Config, log *slog.Logger) (*httpSource, error) {
	token, err := v.DeviceToken()
	if err != nil {
		return nil, err
	}
	info, err := v.JoinInfo()
	if err != nil {
		return nil, err
	}
	last, err := server.DecodeNodeConfig(info.Node)
	if err != nil {
		return nil, err
	}
	mat, err := v.Load(time.Now())
	if err != nil {
		return nil, err
	}
	member, err := v.Secret("member")
	if err != nil {
		return nil, fmt.Errorf("this pre-gate host must re-enroll to obtain a device-bound API identity: %w", err)
	}
	prefix, err := netip.ParsePrefix(mat.Meta.NetworkPrefix)
	if err != nil {
		return nil, err
	}
	var bases []string
	for _, addr := range info.APIAddrs {
		bases = append(bases, "http://"+net.JoinHostPort(addr, strconv.Itoa(info.APIPort)))
	}
	hc, err := hostclient.New(hostclient.Options{Bases: bases, Token: token, Signer: mat.Host, WorkspaceID: mat.Meta.WorkspaceID, UserID: string(member), Network: prefix, Timeout: 15 * time.Second})
	if err != nil {
		return nil, err
	}
	return &httpSource{v: v, cfg: cfg, log: log, token: token, info: info, last: last, hc: hc}, nil
}

func (s *httpSource) get(ctx context.Context, method, path string) (*enrollment.NetworkBundle, error) {
	var b enrollment.NetworkBundle
	err := s.hc.Do(ctx, method, strings.TrimPrefix(path, hostclient.APIPrefix), bodyFor(method), &b)
	return &b, err
}

func bodyFor(method string) any {
	if method == http.MethodPost {
		return map[string]any{}
	}
	return nil
}

// NodeConfig asks the workspace what this host's node should be; if no Workspace Host can be reached (the network is
// not up yet, which is why it is being asked), it is what it last was.
func (s *httpSource) NodeConfig(ctx context.Context) (domain.NodeConfig, error) {
	if b, err := s.get(ctx, http.MethodGet, "/api/team/v1/network/config"); err == nil {
		if c, err := server.DecodeNodeConfig(b.Node); err == nil {
			s.last = c
			s.info.Node = b.Node
			s.info.APIAddrs = append([]string(nil), b.APIAddrs...)
			if len(s.info.APIAddrs) == 0 {
				s.info.APIAddrs = s.last.APIAddrs
			}
			_ = s.v.SaveJoinInfo(s.info)
		}
	} else {
		s.log.Debug("could not ask a Workspace Host for this host's network configuration; using the last one", "err", err)
	}
	return s.last, nil
}

// Certificate is the one on disk while it has over a third of its life left and carries the groups its device's roles call
// for (a device that was made a Workspace Host has to be let through to the other hosts, and the network's policy goes by
// the groups in the certificate); otherwise a new one, asked for over the network.
func (s *httpSource) Certificate(ctx context.Context, cfg domain.NodeConfig) ([]byte, time.Time, error) {
	path := filepath.Join(s.cfg.NodeDir(), "node.crt")
	b, err := os.ReadFile(path)
	if err == nil {
		if info, err := pki.ReadNodeCert(b); err == nil && time.Until(info.NotAfter) > pki.DefaultNodeValidity/3 && server.CertGroupsMatch(info.Groups, cfg.Capabilities) {
			return b, info.NotAfter, nil
		}
	}
	nb, rerr := s.get(ctx, http.MethodPost, "/api/team/v1/network/certificate")
	if rerr != nil {
		if b != nil {
			if info, err := pki.ReadNodeCert(b); err == nil && time.Until(info.NotAfter) > 0 {
				s.log.Warn("could not renew this host's certificate; using the one it has, which is close to expiring", "err", rerr)
				return b, info.NotAfter, nil
			}
		}
		return nil, time.Time{}, fmt.Errorf("this host's certificate is missing or expired and could not be renewed (re-join the workspace if it cannot be): %w", rerr)
	}
	if err := os.WriteFile(path, []byte(nb.NodeCertificate), 0o600); err != nil {
		return nil, time.Time{}, err
	}
	s.log.Info("this host's certificate for the network was renewed", "expires", time.Unix(nb.ExpiresAt, 0).Format(time.RFC3339))
	return []byte(nb.NodeCertificate), time.Unix(nb.ExpiresAt, 0), nil
}

func parseAddr(s string) (netip.Addr, error) { return netip.ParseAddr(s) }

// parseArgs parses flags and returns the arguments that are not flags, wherever they stand: the link
// comes first in `device join <link> --name x`, as it reads best.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return rest, nil
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// deviceSource is where a host that joined as a device learns what its node should be, if this host did.
func deviceSource(cfg config.Config, log *slog.Logger) (*httpSource, bool) {
	if _, err := os.Stat(filepath.Join(cfg.PKIDir(), "join.json")); err != nil {
		return nil, false
	}
	sealer, err := server.SealerFor(cfg)
	if err != nil {
		return nil, false
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return nil, false
	}
	src, err := newHTTPSource(v, cfg, log)
	if err != nil {
		return nil, false
	}
	return src, true
}
