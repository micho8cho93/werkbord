package integration

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ProjectTransfer is an explicit text-only exchange. No execution, local path,
// credential, assignment or history field can enter this contract.
type ProjectTransfer struct {
	Revision   string           `json:"revision"`
	Source     string           `json:"source"`
	Name       string           `json:"name"`
	Repository string           `json:"repository"`
	Tickets    []TransferTicket `json:"tickets"`
}
type TransferTicket struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (p ProjectTransfer) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 || utf8.RuneCountInString(p.Name) > 80 || !utf8.ValidString(p.Name) || transferControl(p.Name, true) {
		return errors.New("project name must fit both boards: at most 80 characters and 120 bytes, on one line")
	}
	if len(p.Source) > 240 || len(p.Tickets) > 500 {
		return errors.New("invalid project transfer (maximum 500 tickets)")
	}
	if p.Repository != "" {
		if _, err := RepositoryIdentity(p.Repository); err != nil {
			return err
		}
	}
	total := 0
	for _, t := range p.Tickets {
		total += len(t.Title) + len(t.Description)
		if strings.TrimSpace(t.Title) == "" || utf8.RuneCountInString(t.Title) > 200 || utf8.RuneCountInString(t.Description) > 20000 || !utf8.ValidString(t.Title+t.Description) || transferControl(t.Title, true) || transferControl(t.Description, false) {
			return errors.New("ticket text is too long or invalid for a project move")
		}
	}
	encoded, _ := json.Marshal(p)
	if total > 700000 || len(encoded) > 900000 {
		return errors.New("project text is too large to move at once")
	}
	return nil
}

// TransferID is stable across retries, including a lost response or app restart.
// Source scopes equal text from distinct projects and destinations independently.
func (p ProjectTransfer) TransferID(prefix string) string {
	p.Revision = ""
	if p.Tickets == nil {
		p.Tickets = []TransferTicket{}
	}
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:10]))
}
func (p ProjectTransfer) Equal(q ProjectTransfer) bool {
	p.Source, q.Source = "", ""
	return p.Revision == q.Revision && p.TransferID("") == q.TransferID("")
}

// SameText ignores source revision and provenance for destination verification.
func (p ProjectTransfer) SameText(q ProjectTransfer) bool {
	p.Revision, q.Revision = "", ""
	return p.Equal(q)
}

func transferControl(s string, singleLine bool) bool {
	for _, r := range s {
		if unicode.IsControl(r) && (singleLine || r != '\n' && r != '\t') {
			return true
		}
	}
	return false
}
