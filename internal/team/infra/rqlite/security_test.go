//go:build !windows

package rqlite_test

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"werkbord/internal/team/infra/rqlite"
	"werkbord/internal/team/infra/rqlite/rqlitetest"
)

// Wire constants are from the pinned upstream cluster/proto/message.proto and
// cluster/service.go: mux byte 2, LE uint64 length, command field 1/type 11,
// BackupRequest field 5 with binary format 2, credentials field 4. Keeping this
// tiny negative probe here avoids linking a second rqlite implementation.
func TestBackupStreamDenialClosesBeforeDatabaseBytesAndNilRequestDoesNotCrash(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	n := c.Node(0)
	for _, tc := range []struct {
		name, user, pass, want string
		request                bool
	}{
		{"unauthenticated", "", "", "unauthorized", true},
		{"node cannot back up", rqlite.UserNode, c.Creds.Node, "unauthorized", true},
		{"nil backup request", rqlite.UserAdmin, c.Creds.Admin, "BackupRequest is nil", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := []byte{0x08, 0x0b}
			if tc.request {
				command = append(command, 0x2a, 0x02, 0x08, 0x02)
			}
			if tc.user != "" {
				credentials := append([]byte{0x0a, byte(len(tc.user))}, []byte(tc.user)...)
				credentials = append(credentials, 0x12, byte(len(tc.pass)))
				credentials = append(credentials, []byte(tc.pass)...)
				command = append(command, 0x22, byte(len(credentials)))
				command = append(command, credentials...)
			}
			conn, err := net.DialTimeout("tcp", n.RaftBind.String(), 3*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			frame := make([]byte, 9)
			frame[0] = 2
			binary.LittleEndian.PutUint64(frame[1:], uint64(len(command)))
			frame = append(frame, command...)
			if _, err = conn.Write(frame); err != nil {
				t.Fatal(err)
			}
			var header [8]byte
			if _, err = io.ReadFull(conn, header[:]); err != nil {
				t.Fatal(err)
			}
			size := binary.LittleEndian.Uint64(header[:])
			if size == 0 || size > 1024 {
				t.Fatalf("invalid denial frame length %d", size)
			}
			body := make([]byte, int(size))
			if _, err = io.ReadFull(conn, body); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Fatal("backup probe was not explicitly refused")
			}
			var extra [1]byte
			count, err := conn.Read(extra[:])
			if count != 0 || err != io.EOF {
				t.Fatal("denied backup did not close before streaming database bytes")
			}
			if _, err = n.Admin().Health(ctxT(t, 5*time.Second)); err != nil {
				t.Fatal("denied or malformed backup stopped the database")
			}
		})
	}
}

func TestQueryEndpointCannotAttachAndReadAnotherSQLiteFile(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	n := c.Node(0)
	path := filepath.Join(t.TempDir(), "unrelated.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE secret(value TEXT); INSERT INTO secret VALUES('must-not-cross-query-boundary')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	statements := []string{"ATTACH DATABASE '" + strings.ReplaceAll(path, "'", "''") + "' AS unrelated", "SELECT value FROM unrelated.secret"}
	data, _ := json.Marshal(statements)
	req, _ := http.NewRequest("POST", "http://"+n.HTTP.String()+"/db/query", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(rqlite.UserApp, c.Creds.App)
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8192))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Results []struct {
			Error string `json:"error"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &result) != nil || len(result.Results) != 2 || result.Results[0].Error == "" || result.Results[1].Error == "" || bytes.Contains(body, []byte("must-not-cross-query-boundary")) {
		t.Fatal("query-only endpoint did not refuse cross-file attachment and reads")
	}
}
