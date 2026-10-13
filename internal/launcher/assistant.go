package launcher

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
)

const maxAssistantWorkspaceAuthorization = 64 << 10

var (
	assistantSessionID = regexp.MustCompile(`^ast_[A-Za-z0-9_-]{1,96}$`)
	assistantRequestID = regexp.MustCompile(`^wreq_[A-Za-z0-9_-]{1,64}$`)
)

// AssistantWorkspaceRequest returns the controller's live approval record for the desktop bridge to compare with the
// exact proposed task arguments. The app never trusts a page's claim that a person approved a write.
func (l *Launcher) AssistantWorkspaceRequest(ctx context.Context, session, request string) ([]byte, error) {
	if !assistantSessionID.MatchString(session) || !assistantRequestID.MatchString(request) {
		return nil, errors.New("invalid assistant workspace request")
	}
	cfg, err := l.Config(ctx)
	if err != nil {
		return nil, errors.New("the controller connection is unavailable")
	}
	token, err := cfg.ResolveToken(false)
	if err != nil || token == "" {
		return nil, errors.New("the controller connection is unavailable")
	}
	base, err := url.Parse(cfg.ControllerURL())
	if err != nil || base.Host == "" || base.Scheme != "http" {
		return nil, errors.New("the controller connection is unavailable")
	}
	base.Path = "/api/assistant/sessions/" + session + "/workspace-requests/" + request
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, errors.New("the controller could not verify this request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := l.opt.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("the controller could not verify this request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("this approved request is no longer active")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAssistantWorkspaceAuthorization+1))
	if err != nil || len(body) > maxAssistantWorkspaceAuthorization {
		return nil, errors.New("the approved request could not be checked")
	}
	return body, nil
}
