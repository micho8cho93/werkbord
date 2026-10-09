package workspaces

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"devboard/internal/workspace"
)

// Service is a program on this computer that holds several workspaces behind one loopback address and one credential, and
// that answers the neutral questions: which workspaces, and what is each one's summary. The app's window loads each
// workspace's own interface from it.
type Service struct {
	name string
	base string
	// key reads the credential the app uses for it. It reads: it never makes one, so a service that was never set up for
	// this person is not mistaken for one that refuses them.
	key      func() (string, error)
	listPath string
	hc       *http.Client
}

// NewService makes a source for such a program. base is its loopback address; listPath is where it lists its workspaces.
func NewService(name, base, listPath string, key func() (string, error)) (*Service, error) {
	b, err := CheckBase(base)
	if err != nil {
		return nil, err
	}
	if !workspace.ValidPath(listPath) {
		return nil, errors.New("not a path")
	}
	return &Service{name: name, base: b, key: key, listPath: listPath, hc: loopbackClient(4 * time.Second)}, nil
}

// Name implements Source.
func (s *Service) Name() string { return s.name }

// Base is the service's loopback address.
func (s *Service) Base() string { return s.base }

// Key is the credential for the service, if the app has one.
func (s *Service) Key() (string, error) { return s.key() }

// List implements Source.
func (s *Service) List(ctx context.Context) ([]workspace.Listed, error) {
	key, err := s.key()
	if err != nil {
		return nil, ErrNoCredential
	}
	body, err := get(ctx, s.hc, s.base, s.listPath, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	l, err := workspace.DecodeListing(body)
	if err != nil {
		return nil, err
	}
	return l.Workspaces, nil
}

// Frame implements Source. join, an invitation the person opened the app with, rides in the fragment next to the
// credential, where the workspace's page reads it once and removes it from the address.
func (s *Service) Frame(ctx context.Context, l workspace.Listed, join string) (Frame, error) {
	key, err := s.key()
	if err != nil {
		return Frame{}, ErrNoCredential
	}
	u := s.base + l.Root + "#token=" + url.QueryEscape(key)
	if join != "" {
		u += "&join=" + url.QueryEscape(join)
	}
	return Frame{URL: u, Origin: s.base}, nil
}

// Summary implements Source.
func (s *Service) Summary(ctx context.Context, l workspace.Listed) (workspace.Summary, error) {
	key, err := s.key()
	if err != nil {
		return workspace.Summary{}, ErrNoCredential
	}
	body, err := get(ctx, s.hc, s.base, l.SummaryPath, key)
	if err != nil {
		return workspace.Summary{}, err
	}
	defer body.Close()
	sum, _, err := workspace.Decode(body, l.Entry.ID)
	return sum, err
}
