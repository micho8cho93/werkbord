package workspaces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// PersonalResponse preserves conflict/status information without exposing credentials.
type PersonalResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

var personalPath = regexp.MustCompile(`^/api/personal/v1/(workspaces(?:/pws_[a-z2-7]{16})?|collections(?:/pcl_[a-z2-7]{16}(?:/records(?:/prec_[a-z2-7]{16})?)?)?)(?:\?limit=100(?:&after=(?:pws|pcl|prec)_[a-z2-7]{16})?)?$`)

var personalLifecyclePath = regexp.MustCompile(`^/api/personal/v1/(?:workspaces/pws_[a-z2-7]{16}/(?:deletion-preview|rollback|history|revisions/[1-9][0-9]{0,9})|collections/pcl_[a-z2-7]{16}/usage)$`)

// PersonalWorkspaceCall is a finite owner API transport, never an arbitrary HTTP proxy.
// The controller retains schema, loopback, owner-token and revision enforcement.
func (p *Individual) PersonalWorkspaceCall(ctx context.Context, method, path string, body json.RawMessage) (PersonalResponse, error) {
	var out PersonalResponse
	lifecycle := personalLifecyclePath.FindStringSubmatch(path)
	if (!personalPath.MatchString(path) && lifecycle == nil) || len(body) > 64<<10 {
		return out, errors.New("invalid personal workspace request")
	}
	entity := strings.Contains(path, "/pws_") || strings.Contains(path, "/pcl_") && !strings.HasSuffix(path, "/records")
	switch method {
	case "GET":
		if len(body) != 0 && string(body) != "null" {
			return out, errors.New("reads take no body")
		}
	case "POST", "PUT", "DELETE":
		if strings.Contains(path, "?") || !json.Valid(body) || lifecycle == nil && (method == "POST") == entity {
			return out, errors.New("invalid personal workspace operation")
		}
	default:
		return out, errors.New("unsupported personal workspace operation")
	}
	if lifecycle != nil {
		want := "GET"
		if strings.HasSuffix(path, "/deletion-preview") || strings.HasSuffix(path, "/rollback") {
			want = "POST"
		}
		if method != want {
			return out, errors.New("unsupported personal lifecycle operation")
		}
	}
	a, err := p.Access(ctx)
	if err != nil {
		return out, err
	}
	base, err := CheckBase(a.Base)
	if err != nil {
		return out, err
	}
	if a.Token == "" {
		return out, errors.New("Werkbord's owner credential is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := p.hc.Do(req)
	if err != nil {
		return out, errors.New("Cannot reach your saved workspaces on this computer")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return out, errors.New("personal workspace response is too large or unavailable")
	}
	if len(raw) == 0 {
		raw = []byte("null")
	}
	if !json.Valid(raw) {
		return out, errors.New("invalid personal workspace response")
	}
	return PersonalResponse{Status: res.StatusCode, Body: raw}, nil
}

// PersonalWorkspace reads the saved owner definition for association checks.
func (p *Individual) PersonalWorkspace(ctx context.Context, id string) (PersonalResponse, error) {
	return p.PersonalWorkspaceCall(ctx, "GET", "/api/personal/v1/workspaces/"+id, nil)
}
