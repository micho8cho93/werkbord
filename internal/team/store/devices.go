package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"devboard/internal/team/domain"
)

const deviceCols = `id, workspace_id, member_id, name, public_key, host_status, connectivity_status, last_seen_at, revoked_at, created_at, updated_at`

func scanDevice(s interface{ Scan(...any) error }) (domain.Device, error) {
	var d domain.Device
	var host, conn string
	var seen, revoked sql.NullInt64
	var created, updated int64
	err := s.Scan(&d.ID, &d.WorkspaceID, &d.MemberID, &d.Name, &d.PublicKey, &host, &conn, &seen, &revoked, &created, &updated)
	d.HostStatus, d.ConnectivityStatus = domain.HostStatus(host), domain.HostStatus(conn)
	d.LastSeenAt, d.RevokedAt = optMS(seen), optMS(revoked)
	d.CreatedAt, d.UpdatedAt = fromMS(created), fromMS(updated)
	d.Capabilities = []domain.Capability{}
	return d, err
}

// InsertDevice records a new device with its capabilities.
func (t *sqlTx) InsertDevice(ctx context.Context, d domain.Device) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO devices (`+deviceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.WorkspaceID, d.MemberID, d.Name, d.PublicKey, string(d.HostStatus), string(d.ConnectivityStatus),
		nullMS(d.LastSeenAt), nullMS(d.RevokedAt), ms(d.CreatedAt), ms(d.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: this device or its key is already registered", domain.ErrConflict)
	}
	if err != nil {
		return err
	}
	return t.writeCapabilities(ctx, d)
}

func (t *sqlTx) writeCapabilities(ctx context.Context, d domain.Device) error {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM device_capabilities WHERE workspace_id = ? AND device_id = ?`, d.WorkspaceID, d.ID); err != nil {
		return err
	}
	for _, c := range d.Capabilities {
		if _, err := t.q.ExecContext(ctx, `INSERT INTO device_capabilities (device_id, workspace_id, capability) VALUES (?, ?, ?)`,
			d.ID, d.WorkspaceID, string(c)); err != nil {
			return err
		}
	}
	return nil
}

// Device returns a device of a workspace, with its capabilities.
func (t *sqlTx) Device(ctx context.Context, workspaceID, id string) (domain.Device, error) {
	d, err := scanDevice(t.q.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	if err != nil {
		return d, notFound(err, "device")
	}
	caps, err := t.capabilities(ctx, workspaceID, []string{id})
	d.Capabilities = caps[id]
	return d, err
}

// Devices lists a workspace's devices, oldest first; with a member ID, only theirs.
func (t *sqlTx) Devices(ctx context.Context, workspaceID, memberID string) ([]domain.Device, error) {
	q := `SELECT ` + deviceCols + ` FROM devices WHERE workspace_id = ?`
	args := []any{workspaceID}
	if memberID != "" {
		q += ` AND member_id = ?`
		args = append(args, memberID)
	}
	return t.deviceList(ctx, workspaceID, q+` ORDER BY created_at, id`, args...)
}

// DevicesWithCapability lists the unrevoked devices that hold a capability.
func (t *sqlTx) DevicesWithCapability(ctx context.Context, workspaceID string, c domain.Capability) ([]domain.Device, error) {
	return t.deviceList(ctx, workspaceID, `SELECT `+deviceCols+` FROM devices WHERE workspace_id = ? AND revoked_at IS NULL
		AND id IN (SELECT device_id FROM device_capabilities WHERE workspace_id = ? AND capability = ?) ORDER BY created_at, id`,
		workspaceID, workspaceID, string(c))
}

func (t *sqlTx) deviceList(ctx context.Context, workspaceID, query string, args ...any) ([]domain.Device, error) {
	rows, err := t.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Device{}
	var ids []string
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
		ids = append(ids, d.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	caps, err := t.capabilities(ctx, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Capabilities = caps[out[i].ID]
	}
	return out, nil
}

// capabilities returns each device's capabilities, sorted; every ID has an entry.
func (t *sqlTx) capabilities(ctx context.Context, workspaceID string, ids []string) (map[string][]domain.Capability, error) {
	out := map[string][]domain.Capability{}
	for _, id := range ids {
		out[id] = []domain.Capability{}
	}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{workspaceID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := t.q.QueryContext(ctx, `SELECT device_id, capability FROM device_capabilities WHERE workspace_id = ? AND device_id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, c string
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = append(out[id], domain.Capability(c))
	}
	for _, cs := range out {
		sort.Slice(cs, func(i, j int) bool { return cs[i] < cs[j] })
	}
	return out, rows.Err()
}

// SaveDevice writes a device's name, capabilities and role statuses. A revoked
// device is never changed: the update is guarded in SQL, so two concurrent writers
// cannot un-revoke it between a read and a write.
func (t *sqlTx) SaveDevice(ctx context.Context, d domain.Device) error {
	res, err := t.q.ExecContext(ctx, `UPDATE devices SET name = ?, host_status = ?, connectivity_status = ?, updated_at = ?
		WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL`,
		d.Name, string(d.HostStatus), string(d.ConnectivityStatus), ms(d.UpdatedAt), d.WorkspaceID, d.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := t.Device(ctx, d.WorkspaceID, d.ID); err != nil {
			return err
		}
		return fmt.Errorf("%w: the device has been revoked", domain.ErrConflict)
	}
	return t.writeCapabilities(ctx, d)
}

// RecordDeviceSeen moves a device's last-seen time forward.
func (t *sqlTx) RecordDeviceSeen(ctx context.Context, workspaceID, id string, at time.Time) error {
	res, err := t.q.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL
		AND (last_seen_at IS NULL OR last_seen_at < ?)`, ms(at), workspaceID, id, ms(at))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err := t.Device(ctx, workspaceID, id) // not found is an error; revoked or not newer is not
		return err
	}
	return nil
}

// RevokeDevice revokes a device and ends its host roles, once.
func (t *sqlTx) RevokeDevice(ctx context.Context, workspaceID, id string, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE devices SET revoked_at = ?, updated_at = ?, host_status = 'none', connectivity_status = 'none'
		WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL`, ms(at), ms(at), workspaceID, id)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err := t.Device(ctx, workspaceID, id)
		return false, err
	}
	return true, nil
}

// ---- device profiles ----

// SaveDeviceProfile records what a device says about itself, replacing what it said before.
func (t *sqlTx) SaveDeviceProfile(ctx context.Context, workspaceID string, p domain.DeviceProfile) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO device_profiles (device_id, workspace_id, platform, form, sleeps, sleep_events, version, other_workspace, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (device_id) DO UPDATE SET platform = excluded.platform, form = excluded.form, sleeps = excluded.sleeps,
			sleep_events = excluded.sleep_events, version = excluded.version, other_workspace = excluded.other_workspace, reported_at = excluded.reported_at
		WHERE device_profiles.workspace_id = excluded.workspace_id`,
		p.DeviceID, workspaceID, p.Platform, string(p.Form), b2i(p.Sleeps), p.SleepEvents, p.Version, b2i(p.HostConflict), ms(p.ReportedAt))
	return err
}

// DeviceProfiles returns every profile in a workspace by device ID.
func (t *sqlTx) DeviceProfiles(ctx context.Context, workspaceID string) (map[string]domain.DeviceProfile, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT device_id, platform, form, sleeps, sleep_events, version, other_workspace, reported_at FROM device_profiles WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.DeviceProfile{}
	for rows.Next() {
		var p domain.DeviceProfile
		var form string
		var sleeps, conflict int
		var at int64
		if err := rows.Scan(&p.DeviceID, &p.Platform, &form, &sleeps, &p.SleepEvents, &p.Version, &conflict, &at); err != nil {
			return nil, err
		}
		p.Form, p.Sleeps, p.HostConflict, p.ReportedAt = domain.DeviceForm(form), sleeps == 1, conflict == 1, fromMS(at)
		out[p.DeviceID] = p
	}
	return out, rows.Err()
}
