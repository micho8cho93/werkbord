package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/team/domain"
)

// ---- settings ----

// InsertNetworkSettings records a workspace's private network.
func (t *sqlTx) InsertNetworkSettings(ctx context.Context, s domain.NetworkSettings) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO network_settings (workspace_id, workspace_key, fingerprint, network_prefix, ca_certificate, enrollment_approval, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, s.WorkspaceID, s.WorkspaceKey, s.Fingerprint, s.NetworkPrefix, s.CACertificate, string(s.EnrollmentApproval), ms(s.CreatedAt), ms(s.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: this workspace already has a private network", domain.ErrConflict)
	}
	return err
}

// NetworkSettings returns a workspace's private network settings.
func (t *sqlTx) NetworkSettings(ctx context.Context, workspaceID string) (domain.NetworkSettings, error) {
	var s domain.NetworkSettings
	var approval string
	var created, updated int64
	err := t.q.QueryRowContext(ctx, `SELECT workspace_id, workspace_key, fingerprint, network_prefix, ca_certificate, enrollment_approval, created_at, updated_at
		FROM network_settings WHERE workspace_id = ?`, workspaceID).Scan(&s.WorkspaceID, &s.WorkspaceKey, &s.Fingerprint, &s.NetworkPrefix, &s.CACertificate, &approval, &created, &updated)
	s.EnrollmentApproval, s.CreatedAt, s.UpdatedAt = domain.ApprovalPolicy(approval), fromMS(created), fromMS(updated)
	return s, notFound(err, "private network")
}

// SetEnrollmentApproval changes whether an administrator must approve joining devices.
func (t *sqlTx) SetEnrollmentApproval(ctx context.Context, workspaceID string, p domain.ApprovalPolicy, at time.Time) error {
	res, err := t.q.ExecContext(ctx, `UPDATE network_settings SET enrollment_approval = ?, updated_at = ? WHERE workspace_id = ?`, string(p), ms(at), workspaceID)
	return affected(res, err, "private network")
}

// ---- devices on the network ----

func jsonList(in []string) string {
	if in == nil {
		in = []string{}
	}
	b, _ := json.Marshal(in)
	return string(b)
}

func parseList(s string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func splitCSV(s string) []string {
	out := []string{}
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func capsCSV(cs []domain.Capability) string {
	var out []string
	for _, c := range cs {
		out = append(out, string(c))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func csvCaps(s string) []domain.Capability {
	out := []domain.Capability{}
	for _, f := range splitCSV(s) {
		out = append(out, domain.Capability(f))
	}
	return out
}

const deviceNetCols = `device_id, workspace_id, overlay_addr, network_public_key, sealing_key, groups, discovery, relay, bootstrap_endpoints, network_endpoints, reachability,
	last_check_at, last_external_ok_at, provision_sealed IS NOT NULL, created_at, updated_at`

func scanDeviceNetwork(s interface{ Scan(...any) error }) (domain.DeviceNetwork, error) {
	var n domain.DeviceNetwork
	var groups, boot, netEP, reach string
	var disc, relay, pending int
	var checked, ok sql.NullInt64
	var created, updated int64
	err := s.Scan(&n.DeviceID, &n.WorkspaceID, &n.OverlayAddr, &n.NetworkPublicKey, &n.SealingKey, &groups, &disc, &relay, &boot, &netEP, &reach, &checked, &ok, &pending, &created, &updated)
	n.Groups, n.Discovery, n.Relay = splitCSV(groups), disc == 1, relay == 1
	n.BootstrapEndpoints, n.NetworkEndpoints, n.Reachability = parseList(boot), parseList(netEP), domain.Reachability(reach)
	n.LastCheckAt, n.LastExternalOKAt, n.ProvisionPending = optMS(checked), optMS(ok), pending == 1
	n.CreatedAt, n.UpdatedAt = fromMS(created), fromMS(updated)
	return n, err
}

// InsertDeviceNetwork places a device on the network.
func (t *sqlTx) InsertDeviceNetwork(ctx context.Context, n domain.DeviceNetwork) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO device_network (device_id, workspace_id, overlay_addr, network_public_key, sealing_key, groups, discovery, relay, bootstrap_endpoints, network_endpoints, reachability, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.DeviceID, n.WorkspaceID, n.OverlayAddr, n.NetworkPublicKey, n.SealingKey, strings.Join(n.Groups, ","), b2i(n.Discovery), b2i(n.Relay),
		jsonList(n.BootstrapEndpoints), jsonList(n.NetworkEndpoints), string(n.Reachability), ms(n.CreatedAt), ms(n.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: that address on the network is already taken", domain.ErrConflict)
	}
	return err
}

// DeviceNetwork returns a device's place on the network.
func (t *sqlTx) DeviceNetwork(ctx context.Context, workspaceID, deviceID string) (domain.DeviceNetwork, error) {
	n, err := scanDeviceNetwork(t.q.QueryRowContext(ctx, `SELECT `+deviceNetCols+` FROM device_network WHERE workspace_id = ? AND device_id = ?`, workspaceID, deviceID))
	return n, notFound(err, "device on the network")
}

// DeviceNetworks lists every device on the network.
func (t *sqlTx) DeviceNetworks(ctx context.Context, workspaceID string) ([]domain.DeviceNetwork, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+deviceNetCols+` FROM device_network WHERE workspace_id = ? ORDER BY overlay_addr`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DeviceNetwork{}
	for rows.Next() {
		n, err := scanDeviceNetwork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SaveDeviceNetwork writes a device's groups, roles, endpoints and what is known of its reachability.
func (t *sqlTx) SaveDeviceNetwork(ctx context.Context, n domain.DeviceNetwork) error {
	res, err := t.q.ExecContext(ctx, `UPDATE device_network SET groups = ?, discovery = ?, relay = ?, bootstrap_endpoints = ?, network_endpoints = ?,
		reachability = ?, last_check_at = ?, last_external_ok_at = ?, updated_at = ? WHERE workspace_id = ? AND device_id = ?`,
		strings.Join(n.Groups, ","), b2i(n.Discovery), b2i(n.Relay), jsonList(n.BootstrapEndpoints), jsonList(n.NetworkEndpoints),
		string(n.Reachability), nullMS(n.LastCheckAt), nullMS(n.LastExternalOKAt), ms(n.UpdatedAt), n.WorkspaceID, n.DeviceID)
	return affected(res, err, "device on the network")
}

// SetReachability records a check.
func (t *sqlTx) SetReachability(ctx context.Context, workspaceID, deviceID string, r domain.Reachability, at time.Time, externalOK bool) error {
	q := `UPDATE device_network SET reachability = ?, last_check_at = ?, updated_at = ?`
	args := []any{string(r), ms(at), ms(at)}
	if externalOK {
		q += `, last_external_ok_at = ?`
		args = append(args, ms(at))
	}
	res, err := t.q.ExecContext(ctx, q+` WHERE workspace_id = ? AND device_id = ?`, append(args, workspaceID, deviceID)...)
	return affected(res, err, "device on the network")
}

// SetProvision keeps sealed secrets for a device to collect.
func (t *sqlTx) SetProvision(ctx context.Context, workspaceID, deviceID string, sealed []byte, at time.Time) error {
	res, err := t.q.ExecContext(ctx, `UPDATE device_network SET provision_sealed = ?, updated_at = ? WHERE workspace_id = ? AND device_id = ?`, sealed, ms(at), workspaceID, deviceID)
	return affected(res, err, "device on the network")
}

// Provision returns the sealed secrets waiting for a device.
func (t *sqlTx) Provision(ctx context.Context, workspaceID, deviceID string) ([]byte, error) {
	var b []byte
	err := t.q.QueryRowContext(ctx, `SELECT provision_sealed FROM device_network WHERE workspace_id = ? AND device_id = ?`, workspaceID, deviceID).Scan(&b)
	return b, notFound(err, "device on the network")
}

// ClearProvision removes sealed secrets once collected.
func (t *sqlTx) ClearProvision(ctx context.Context, workspaceID, deviceID string, at time.Time) error {
	res, err := t.q.ExecContext(ctx, `UPDATE device_network SET provision_sealed = NULL, updated_at = ? WHERE workspace_id = ? AND device_id = ?`, ms(at), workspaceID, deviceID)
	return affected(res, err, "device on the network")
}

// ---- certificates ----

// InsertCertificate records a certificate that was issued. Issuing the same certificate
// twice (the same device, key, address, groups and times) makes the same bytes, and so
// the same fingerprint: that is recorded once, and a record of it that was revoked stays revoked.
func (t *sqlTx) InsertCertificate(ctx context.Context, workspaceID string, c domain.NetworkCertificate) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO network_certificates (fingerprint, workspace_id, device_id, issued_at, not_after) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (fingerprint) DO NOTHING`, c.Fingerprint, workspaceID, c.DeviceID, ms(c.IssuedAt), ms(c.NotAfter))
	return err
}

// RevokeCertificates marks a device's certificates revoked.
func (t *sqlTx) RevokeCertificates(ctx context.Context, workspaceID, deviceID string, at time.Time) (int, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE network_certificates SET revoked_at = ? WHERE workspace_id = ? AND device_id = ? AND revoked_at IS NULL`, ms(at), workspaceID, deviceID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Blocklist returns the fingerprints the network must refuse.
func (t *sqlTx) Blocklist(ctx context.Context, workspaceID string, now time.Time) ([]string, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT fingerprint FROM network_certificates WHERE workspace_id = ? AND revoked_at IS NOT NULL AND not_after > ? ORDER BY fingerprint`, workspaceID, ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- device credentials ----

// SetDeviceCredential stores the hash of a device's API token.
func (t *sqlTx) SetDeviceCredential(ctx context.Context, workspaceID, deviceID, tokenHash string, at time.Time) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO device_credentials (device_id, workspace_id, token_hash, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id) DO UPDATE SET token_hash = excluded.token_hash, created_at = excluded.created_at`, deviceID, workspaceID, tokenHash, ms(at))
	return err
}

// DeviceByCredentialHash finds the device a token belongs to.
func (t *sqlTx) DeviceByCredentialHash(ctx context.Context, tokenHash string) (domain.Device, error) {
	var ws, id string
	if err := t.q.QueryRowContext(ctx, `SELECT workspace_id, device_id FROM device_credentials WHERE token_hash = ?`, tokenHash).Scan(&ws, &id); err != nil {
		return domain.Device{}, notFound(err, "device")
	}
	return t.Device(ctx, ws, id)
}

// DeleteDeviceCredential removes a device's token, which signs the device out of the API.
func (t *sqlTx) DeleteDeviceCredential(ctx context.Context, workspaceID, deviceID string) error {
	_, err := t.q.ExecContext(ctx, `DELETE FROM device_credentials WHERE workspace_id = ? AND device_id = ?`, workspaceID, deviceID)
	return err
}

// ---- invitations ----

const enrollInviteCols = `id, workspace_id, label, for_member_id, role, capabilities, require_approval, state, created_by, created_at, expires_at, used_at`

func scanEnrollInvitation(s interface{ Scan(...any) error }) (domain.EnrollInvitation, error) {
	var i domain.EnrollInvitation
	var role, caps, state string
	var req int
	var created, expires int64
	var used sql.NullInt64
	err := s.Scan(&i.ID, &i.WorkspaceID, &i.Label, &i.ForMemberID, &role, &caps, &req, &state, &i.CreatedBy, &created, &expires, &used)
	i.Role, i.Capabilities, i.RequireApproval, i.State = domain.Role(role), csvCaps(caps), req == 1, domain.EnrollInvitationState(state)
	i.CreatedAt, i.ExpiresAt, i.UsedAt = fromMS(created), fromMS(expires), optMS(used)
	return i, err
}

// InsertEnrollInvitation stores an invitation with the hash of its credential.
func (t *sqlTx) InsertEnrollInvitation(ctx context.Context, inv domain.EnrollInvitation, credentialHash []byte) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO enrollment_invitations (id, workspace_id, credential_hash, created_by, label, for_member_id, role, capabilities, require_approval, state, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
		inv.ID, inv.WorkspaceID, credentialHash, inv.CreatedBy, inv.Label, inv.ForMemberID, string(inv.Role), capsCSV(inv.Capabilities), b2i(inv.RequireApproval), ms(inv.CreatedAt), ms(inv.ExpiresAt))
	return err
}

// EnrollInvitationByID finds an invitation, with the hash of its credential.
func (t *sqlTx) EnrollInvitationByID(ctx context.Context, id string) (domain.EnrollInvitation, []byte, error) {
	var hash []byte
	row := t.q.QueryRowContext(ctx, `SELECT `+enrollInviteCols+`, credential_hash FROM enrollment_invitations WHERE id = ?`, id)
	var i domain.EnrollInvitation
	var role, caps, state string
	var req int
	var created, expires int64
	var used sql.NullInt64
	err := row.Scan(&i.ID, &i.WorkspaceID, &i.Label, &i.ForMemberID, &role, &caps, &req, &state, &i.CreatedBy, &created, &expires, &used, &hash)
	i.Role, i.Capabilities, i.RequireApproval, i.State = domain.Role(role), csvCaps(caps), req == 1, domain.EnrollInvitationState(state)
	i.CreatedAt, i.ExpiresAt, i.UsedAt = fromMS(created), fromMS(expires), optMS(used)
	return i, hash, notFound(err, "invitation")
}

// EnrollInvitations lists a workspace's invitations, newest first.
func (t *sqlTx) EnrollInvitations(ctx context.Context, workspaceID string) ([]domain.EnrollInvitation, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+enrollInviteCols+` FROM enrollment_invitations WHERE workspace_id = ? ORDER BY created_at DESC, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.EnrollInvitation{}
	for rows.Next() {
		i, err := scanEnrollInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UseEnrollInvitation uses up an open invitation, once.
func (t *sqlTx) UseEnrollInvitation(ctx context.Context, workspaceID, id string, now time.Time) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE enrollment_invitations SET state = 'used', used_at = ? WHERE workspace_id = ? AND id = ? AND state = 'open' AND expires_at > ?`, ms(now), workspaceID, id, ms(now))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// WithdrawEnrollInvitation stops an open invitation from being used.
func (t *sqlTx) WithdrawEnrollInvitation(ctx context.Context, workspaceID, id string) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE enrollment_invitations SET state = 'withdrawn' WHERE workspace_id = ? AND id = ? AND state = 'open'`, workspaceID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ---- enrollments ----

const enrollCols = `id, workspace_id, invitation_id, member_id, member_name, device_id, device_name, device_key, network_public_key, sealing_key, capabilities, state, delivered_at, decided_by, decided_at, remote_addr, created_at`

func scanEnrollment(s interface{ Scan(...any) error }) (domain.Enrollment, error) {
	var e domain.Enrollment
	var caps, state string
	var delivered, decided sql.NullInt64
	var created int64
	err := s.Scan(&e.ID, &e.WorkspaceID, &e.InvitationID, &e.MemberID, &e.MemberName, &e.DeviceID, &e.DeviceName, &e.DeviceKey, &e.NetworkPublicKey, &e.SealingKey, &caps, &state, &delivered, &e.DecidedBy, &decided, &e.RemoteAddr, &created)
	e.Capabilities, e.State, e.Delivered, e.DecidedAt, e.CreatedAt = csvCaps(caps), domain.EnrollmentState(state), delivered.Valid, optMS(decided), fromMS(created)
	return e, err
}

// InsertEnrollment records a request to join. A second request for one invitation, or
// a device ID already registered, is domain.ErrConflict.
func (t *sqlTx) InsertEnrollment(ctx context.Context, e domain.Enrollment) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO enrollments (`+enrollCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, '', NULL, ?, ?)`,
		e.ID, e.WorkspaceID, e.InvitationID, e.MemberID, e.MemberName, e.DeviceID, e.DeviceName, e.DeviceKey, e.NetworkPublicKey, e.SealingKey, capsCSV(e.Capabilities), string(e.State), e.RemoteAddr, ms(e.CreatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: this invitation was already used", domain.ErrConflict)
	}
	return err
}

// Enrollment returns a request to join.
func (t *sqlTx) Enrollment(ctx context.Context, workspaceID, id string) (domain.Enrollment, error) {
	e, err := scanEnrollment(t.q.QueryRowContext(ctx, `SELECT `+enrollCols+` FROM enrollments WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	return e, notFound(err, "enrollment")
}

// EnrollmentByID finds a request to join in any workspace.
func (t *sqlTx) EnrollmentByID(ctx context.Context, id string) (domain.Enrollment, error) {
	e, err := scanEnrollment(t.q.QueryRowContext(ctx, `SELECT `+enrollCols+` FROM enrollments WHERE id = ?`, id))
	return e, notFound(err, "enrollment")
}

// Enrollments lists a workspace's requests to join, in a state ("" for any).
func (t *sqlTx) Enrollments(ctx context.Context, workspaceID string, state domain.EnrollmentState) ([]domain.Enrollment, error) {
	q := `SELECT ` + enrollCols + ` FROM enrollments WHERE workspace_id = ?`
	args := []any{workspaceID}
	if state != "" {
		q += ` AND state = ?`
		args = append(args, string(state))
	}
	rows, err := t.q.QueryContext(ctx, q+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Enrollment{}
	for rows.Next() {
		e, err := scanEnrollment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DecideEnrollment approves or denies a pending request, once.
func (t *sqlTx) DecideEnrollment(ctx context.Context, workspaceID, id string, state domain.EnrollmentState, memberID, by string, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE enrollments SET state = ?, member_id = CASE WHEN ? <> '' THEN ? ELSE member_id END, decided_by = ?, decided_at = ?
		WHERE workspace_id = ? AND id = ? AND state = 'pending'`, string(state), memberID, memberID, by, ms(at), workspaceID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SetEnrollmentMember records the member the device was enrolled for.
func (t *sqlTx) SetEnrollmentMember(ctx context.Context, workspaceID, id, memberID string) error {
	res, err := t.q.ExecContext(ctx, `UPDATE enrollments SET member_id = ? WHERE workspace_id = ? AND id = ?`, memberID, workspaceID, id)
	return affected(res, err, "enrollment")
}

// MarkEnrollmentDelivered records that the device collected what it was issued.
func (t *sqlTx) MarkEnrollmentDelivered(ctx context.Context, workspaceID, id string, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE enrollments SET delivered_at = ? WHERE workspace_id = ? AND id = ? AND state = 'approved' AND delivered_at IS NULL`, ms(at), workspaceID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
