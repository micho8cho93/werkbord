package server

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"devboard/internal/team/connector"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/localwerkbord"
)

// syncView is where synchronization with the person's own Werkbord stands, for this computer's screens.
type syncView struct {
	// Active: the runner is connected and tickets are being synchronized.
	Active  bool                   `json:"active"`
	Tickets []connector.TicketSync `json:"tickets"`
	Problem string                 `json:"problem,omitempty"`
	At      time.Time              `json:"at,omitempty"`
}

// synchronize copies the member's held tickets into their own Werkbord, as the connector does, once their runner is
// connected: every project they are on except those they turned off. The Werkbord grant it uses is the narrow one the
// person handed this workspace (execution-local-v1), which can import text and read projected status, never start a run.
// A failure is shown beside the tickets; it does not make the workspace itself look broken.
func (d *Daemon) synchronize(ctx context.Context, host *hostclient.Client, bridge *localwerkbord.Client, mat *pki.Material, member string) {
	d.mu.Lock()
	j := d.journal
	d.mu.Unlock()
	if j == nil {
		var err error
		if j, err = devicestate.OpenSyncJournal(ctx, filepath.Join(d.workspaceConfig().DataDir, "sync.db")); err != nil {
			d.setSync(syncView{Problem: "synchronization could not start: " + err.Error()})
			return
		}
		d.mu.Lock()
		if d.journal == nil {
			d.journal = j
		} else {
			_ = j.Close()
			j = d.journal
		}
		d.mu.Unlock()
	}
	off := map[string]bool{}
	for _, id := range d.state.Settings().SyncOff {
		off[id] = true
	}
	c := &connector.Connector{WorkspaceID: mat.Meta.WorkspaceID, MemberID: member, DeviceID: mat.Host.DeviceID(), Host: host, Local: bridge, Journal: j, All: true, Off: off}
	err := c.Tick(ctx)
	v := syncView{Active: true, Tickets: []connector.TicketSync{}, At: time.Now()}
	for _, st := range c.Status {
		v.Tickets = append(v.Tickets, st)
	}
	sort.Slice(v.Tickets, func(a, b int) bool {
		return v.Tickets[a].ProjectID+v.Tickets[a].TicketID < v.Tickets[b].ProjectID+v.Tickets[b].TicketID
	})
	if err != nil {
		v.Problem = err.Error()
	}
	d.setSync(v)
}

func (d *Daemon) setSync(v syncView) {
	d.mu.Lock()
	d.sync = v
	d.mu.Unlock()
}

// closeJournal releases the synchronization journal when the workspace stops or is left.
func (d *Daemon) closeJournal() {
	d.mu.Lock()
	j := d.journal
	d.journal, d.sync = nil, syncView{}
	d.mu.Unlock()
	if j != nil {
		_ = j.Close()
	}
}
