package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"devboard/internal/integration"
	"devboard/internal/team/config"
	"devboard/internal/team/connector"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/server"
)

type connectorConfig struct {
	Version            int                  `json:"version"`
	StateDir           string               `json:"stateDir"`
	RunnerBase         string               `json:"runnerBase"`
	ExecutionTokenFile string               `json:"executionTokenFile,omitempty"`
	AccessTokenFile    string               `json:"accessTokenFile"`
	Workspaces         []connectorWorkspace `json:"workspaces"`
}
type connectorWorkspace struct {
	DataDir        string            `json:"dataDir"`
	KeyStorage     string            `json:"keyStorage"`
	PassphraseFile string            `json:"passphraseFile,omitempty"`
	Bases          []string          `json:"bases,omitempty"`
	Projects       map[string]string `json:"projects"`
}

func loadConnectorExecution(ctx context.Context, base, tokenFile string) (connector.ExecutionLocal, error) {
	if tokenFile == "" {
		return nil, nil
	}
	raw, err := privateConnectorFile(tokenFile, 4096)
	if err != nil {
		return nil, err
	}
	client, err := localwerkbord.New(base, strings.TrimSpace(string(raw)))
	clear(raw)
	if err != nil {
		return nil, err
	}
	if err := client.RequireDispatchAccess(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func privateConnectorFile(path string, limit int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 || fi.Size() > limit {
		return nil, errors.New("connector files must be private regular files within the size limit")
	}
	return os.ReadFile(path)
}

func cmdConnector(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if os.Geteuid() == 0 {
		return errors.New("run the connector as your login user; networking and Workspace Host services are separate")
	}
	if len(args) == 0 {
		return errors.New("usage: werkbord-team connector connect|join|run|status|resume [flags]")
	}
	if args[0] == "join" {
		return cmdDeviceJoinAs(ctx, cfg, args[1:], stdout, stderr, true)
	}
	fs := flag.NewFlagSet("connector "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	if args[0] == "connect" {
		execution := fs.Bool("execution", false, "mint dispatch-only access for locally approved scheduled work")
		base := fs.String("runner", localwerkbord.DefaultBase, "your controller's loopback address")
		full := fs.String("controller-token-file", "", "controller credential used once to mint narrow access")
		access := fs.String("access-file", "", "private file to save the revocable local access grant")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *full == "" || *access == "" || filepath.Clean(*full) == filepath.Clean(*access) {
			return errors.New("use distinct --controller-token-file and --access-file paths")
		}
		raw, err := privateConnectorFile(*full, 4096)
		if err != nil {
			return err
		}
		defer clear(raw)
		if err := os.MkdirAll(filepath.Dir(*access), 0700); err != nil {
			return err
		}
		// Reserve the destination before minting: an existing file must not
		// revoke the grant currently used by a running connector.
		f, err := os.OpenFile(*access, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		saved := false
		defer func() {
			_ = f.Close()
			if !saved {
				_ = os.Remove(*access)
			}
		}()
		var token string
		if *execution {
			token, err = localwerkbord.ConnectDispatch(ctx, *base, strings.TrimSpace(string(raw)))
		} else {
			token, err = localwerkbord.ConnectSync(ctx, *base, strings.TrimSpace(string(raw)))
		}
		clear(raw)
		if err != nil {
			return err
		}
		_, err = f.WriteString(token + "\n")
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		saved = true
		fmt.Fprintln(stdout, "Saved revocable local access. Configure connector workspaces and opted-in projects, then install the user service.")
		return nil
	}
	if args[0] != "run" && args[0] != "status" && args[0] != "resume" {
		return errors.New("usage: werkbord-team connector connect|join|run|status|resume [flags]")
	}
	path := fs.String("config", "", "private connector JSON configuration")
	workspaceID := fs.String("workspace", "", "workspace identity to resume")
	ticketID := fs.String("ticket", "", "ticket identity to resume after local review")
	once := fs.Bool("once", false, "reconcile once and exit")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	raw, err := privateConnectorFile(*path, 64<<10)
	if err != nil {
		return err
	}
	var cc connectorConfig
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cc); err != nil {
		return err
	}
	if cc.Version != 1 || !filepath.IsAbs(cc.StateDir) || !filepath.IsAbs(cc.AccessTokenFile) || len(cc.Workspaces) == 0 || len(cc.Workspaces) > 16 {
		return errors.New("connector configuration requires version 1, absolute user paths and 1–16 workspaces")
	}
	if err := os.MkdirAll(cc.StateDir, 0700); err != nil {
		return err
	}
	// A login service and a foreground run may not share one journal writer.
	if args[0] == "run" || args[0] == "resume" {
		unlock, err := lockConnector(filepath.Join(cc.StateDir, "connector.lock"))
		if err != nil {
			return err
		}
		defer unlock()
	}
	j, err := devicestate.OpenSyncJournal(ctx, filepath.Join(cc.StateDir, "sync.db"))
	if err != nil {
		return err
	}
	defer j.Close()
	if args[0] == "resume" {
		if !integration.Identifier(*workspaceID) || !integration.Identifier(*ticketID) {
			return errors.New("resume requires --workspace and --ticket identities")
		}
		as, err := j.Associations(ctx, *workspaceID)
		if err != nil {
			return err
		}
		for _, a := range as {
			if a.TicketID == *ticketID {
				a.LastObservation = ""
				if err := j.Suspend(ctx, &a, ""); err != nil {
					return err
				}
				fmt.Fprintln(stdout, "Cleared suspension after local review. The next run rechecks current membership, assignment and repository before reporting.")
				return nil
			}
		}
		return errors.New("no matching local association")
	}
	if args[0] == "status" {
		for _, wc := range cc.Workspaces {
			_, mat, err := connectorVault(cfg, wc)
			if err != nil {
				return err
			}
			as, err := j.Associations(ctx, mat.Meta.WorkspaceID)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(as, "", "  ")
			fmt.Fprintln(stdout, string(b))
		}
		return nil
	}
	token, err := privateConnectorFile(cc.AccessTokenFile, 4096)
	if err != nil {
		return err
	}
	local, err := localwerkbord.New(cc.RunnerBase, strings.TrimSpace(string(token)))
	clear(token)
	if err != nil {
		return err
	}
	if err := local.RequireIntegrationAccess(ctx); err != nil {
		return err
	}
	execution, err := loadConnectorExecution(ctx, cc.RunnerBase, cc.ExecutionTokenFile)
	if err != nil {
		return err
	}
	cs := []*connector.Connector{}
	seen := map[string]bool{}
	for _, wc := range cc.Workspaces {
		v, mat, err := connectorVault(cfg, wc)
		if err != nil {
			return err
		}
		if seen[mat.Meta.WorkspaceID] {
			return errors.New("duplicate workspace in connector configuration")
		}
		seen[mat.Meta.WorkspaceID] = true
		for p, local := range wc.Projects {
			if !integration.Identifier(p) || local != "" && !integration.Identifier(local) {
				return errors.New("project selections must be identifiers")
			}
		}
		member, err := v.Secret("member")
		if err != nil {
			return err
		}
		credential, err := v.DeviceToken()
		if err != nil {
			return err
		}
		info, err := v.JoinInfo()
		if err != nil {
			return err
		}
		bases := wc.Bases
		if len(bases) == 0 {
			for _, addr := range info.APIAddrs {
				bases = append(bases, "http://"+net.JoinHostPort(addr, strconv.Itoa(info.APIPort)))
			}
		}
		network, err := netip.ParsePrefix(mat.Meta.NetworkPrefix)
		if err != nil {
			return err
		}
		host, err := hostclient.New(hostclient.Options{Signer: mat.Host, WorkspaceID: mat.Meta.WorkspaceID, UserID: string(member), Token: credential, Bases: bases, Network: network, Timeout: 8 * time.Second})
		if err != nil {
			return err
		}
		cs = append(cs, &connector.Connector{WorkspaceID: mat.Meta.WorkspaceID, MemberID: string(member), DeviceID: mat.Host.DeviceID(), Host: host, Local: local, Execution: execution, Journal: j, Projects: wc.Projects})
	}
	// Each workspace has its own retry schedule. An offline team never stops another.
	runOne := func(c *connector.Connector) error { return c.Tick(ctx) }
	if *once {
		var errs []error
		for _, c := range cs {
			errs = append(errs, runOne(c))
		}
		return errors.Join(errs...)
	}
	done := make(chan struct{}, len(cs))
	for _, c := range cs {
		go func(c *connector.Connector) {
			defer func() { done <- struct{}{} }()
			attempt := 0
			for ctx.Err() == nil {
				err := runOne(c)
				delay := 3 * time.Second
				if err != nil {
					fmt.Fprintf(stderr, "workspace %s: %v\n", c.WorkspaceID, err)
					delay = time.Second * time.Duration(1<<min(attempt, 8))
					if delay > 5*time.Minute {
						delay = 5 * time.Minute
					}
					attempt++
				} else {
					attempt = 0
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}(c)
	}
	for range cs {
		<-done
	}
	return ctx.Err()
}

func connectorVault(base config.Config, w connectorWorkspace) (*pki.Vault, *pki.Material, error) {
	if !filepath.IsAbs(w.DataDir) {
		return nil, nil, errors.New("workspace dataDir must be an absolute user-owned path")
	}
	base.DataDir, base.SecureStorageID, base.PassphraseFile = w.DataDir, w.DataDir, w.PassphraseFile
	base.KeyStorage = w.KeyStorage
	if base.KeyStorage == "" {
		base.KeyStorage = "os"
	}
	if base.KeyStorage != "os" && base.KeyStorage != "file" {
		return nil, nil, errors.New("unsupported key storage")
	}
	sealer, err := server.SealerFor(base)
	if err != nil {
		return nil, nil, err
	}
	v, err := pki.OpenVault(base.PKIDir(), sealer)
	if err != nil {
		return nil, nil, err
	}
	mat, err := v.Load(time.Now())
	if err != nil {
		return nil, nil, err
	}
	if mat.Meta.Authority {
		return nil, nil, errors.New("connector refuses Workspace Host authority material; enroll a separate user device")
	}
	return v, mat, nil
}
