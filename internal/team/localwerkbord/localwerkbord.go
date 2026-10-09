// Package localwerkbord is Team's narrow, authenticated bridge to the person's own Werkbord on this computer.
//
// Team never executes developer work. When the person's own device is asked, by the person, to open a ticket or start an
// approved task, the Team daemon on that device passes the request on to the Werkbord that person already runs here, and
// that Werkbord applies its own execution policies and approvals. This package is that last step, and it is deliberately
// small:
//
//   - It talks to an address on this computer and to nothing else: the connection itself is refused if it is not to a
//     loopback address, whatever name was given, and redirects are not followed.
//   - It holds a *local access token* (internal/localaccess in the individual product), not Werkbord's own token. That
//     token is accepted by Werkbord on a short list of routes and cannot register repositories, change settings, touch Git,
//     pair runners or choose how a run runs. The bridge asks only for what that list allows, and never sends a field that
//     would name an agent, a model, a policy, instructions, a runner or a schedule.
//   - It has no way to run a command, read a file or reach a path: there is no such method, and nothing it sends has a field
//     that could carry one.
//
// The controller's own token is used once, by Connect, to be given a local access token, and is not kept.
package localwerkbord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"devboard/internal/integration"
	"devboard/internal/team/domain"
)

// DefaultBase is where Werkbord listens on this computer unless it was told otherwise.
const DefaultBase = "http://127.0.0.1:7420"

// maxResponse bounds what is read from Werkbord.
const maxResponse = 4 << 20

// Errors the bridge reports in words a person can act on.
var (
	ErrNotRunning   = errors.New("Werkbord is not running on this computer")
	ErrAccessDenied = errors.New("Werkbord no longer accepts this program's access; connect it again")
	ErrNotAnswering = errors.New("what answered is not Werkbord")
)

// Client is a connection to the person's own Werkbord.
type Client struct {
	base  string
	token string
	hc    *http.Client
}

// loopbackOnly dials only a literal loopback address. The base address is made one (CheckBase turns "localhost" into
// 127.0.0.1), so nothing is ever resolved by name: there is no name that could be made to point somewhere else.
func loopbackOnly(d *net.Dialer) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("localwerkbord: %s is not on this computer", address)
		}
		return d.DialContext(ctx, network, address)
	}
}

// CheckBase accepts an http address on this computer and returns it, without a trailing slash and with "localhost" written
// as 127.0.0.1.
func CheckBase(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not an http address like %s", s, DefaultBase)
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && !(ip != nil && ip.IsLoopback()) {
		return "", fmt.Errorf("%q is not on this computer: Team talks only to the Werkbord that runs here", s)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	port := u.Port()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return "http://" + host, nil
}

func newHTTP() *http.Client {
	d := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil, // never through a proxy
			DialContext:           loopbackOnly(d),
			MaxIdleConns:          2,
			IdleConnTimeout:       30 * time.Second,
			ResponseHeaderTimeout: 25 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// New connects to Werkbord at base with a local access token.
func New(base, token string) (*Client, error) {
	b, err := CheckBase(base)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(token, "wba_") {
		return nil, errors.New("localwerkbord: a narrow local access token is required")
	}
	return &Client{base: b, token: token, hc: newHTTP()}, nil
}

// IntegrationProjects reads only the versioned discovery projection.
func (c *Client) IntegrationProjects(ctx context.Context) ([]Project, error) {
	var out integration.Projects
	if err := c.do(ctx, "GET", "/api/integration/v1/projects", nil, &out); err != nil {
		return nil, err
	}
	if out.Schema != integration.Schema {
		return nil, ErrNotAnswering
	}
	projects := []Project{}
	for _, p := range out.Projects {
		if !integration.Identifier(p.ID) {
			return nil, ErrNotAnswering
		}
		projects = append(projects, Project{ID: p.ID, Name: p.Name, Remotes: p.Remotes})
	}
	return projects, nil
}
func (c *Client) RequireIntegrationAccess(ctx context.Context) error {
	var entry struct {
		Scope string `json:"scope"`
	}
	if err := c.do(ctx, "GET", "/api/local-access/self", nil, &entry); err != nil {
		return err
	}
	if entry.Scope != "integration-v1" {
		return errors.New("connector requires an integration-v1 grant; reconnect with connector connect")
	}
	return nil
}

func (c *Client) Import(ctx context.Context, in integration.Import) (integration.Imported, error) {
	var out integration.Imported
	err := c.do(ctx, "POST", "/api/integration/v1/import", in, &out)
	if err == nil && (out.Schema != integration.Schema || !integration.Identifier(out.TaskID) || out.ProjectID != in.ProjectID) {
		err = ErrNotAnswering
	}
	return out, err
}
func (c *Client) Snapshot(ctx context.Context, pid, tid string) (integration.Snapshot, error) {
	var out integration.Snapshot
	if err := checkID(pid); err != nil {
		return out, err
	}
	if err := checkID(tid); err != nil {
		return out, err
	}
	err := c.do(ctx, "GET", "/api/integration/v1/projects/"+pid+"/tasks/"+tid+"/status", nil, &out)
	if err == nil && (out.Schema != integration.Schema || !out.Execution.Valid()) {
		err = ErrNotAnswering
	}
	return out, err
}
func (c *Client) Events(ctx context.Context, pid, tid string, after int64) (integration.Feed, error) {
	var out integration.Feed
	if err := checkID(pid); err != nil {
		return out, err
	}
	if err := checkID(tid); err != nil {
		return out, err
	}
	err := c.do(ctx, "GET", "/api/integration/v1/projects/"+pid+"/tasks/"+tid+"/events?after="+strconv.FormatInt(after, 10), nil, &out)
	if err == nil && out.Schema != integration.Schema {
		err = ErrNotAnswering
	}
	return out, err
}

// Base is the address this client talks to.
func (c *Client) Base() string { return c.base }

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return call(ctx, c.hc, c.base, c.token, method, path, body, out)
}

func call(ctx context.Context, hc *http.Client, base, token, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hc.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && !strings.Contains(err.Error(), "is not on this computer") {
			return ErrNotRunning
		}
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return ErrAccessDenied
	case res.StatusCode >= 300:
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return &StatusError{Code: res.StatusCode, Message: e.Error.Message}
		}
		return &StatusError{Code: res.StatusCode, Message: "Werkbord answered " + strconv.Itoa(res.StatusCode)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// StatusError is Werkbord refusing or failing a request, in its own words.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string { return e.Message }

// IsNotFound reports whether Werkbord said it has no such thing.
func IsNotFound(err error) bool {
	var s *StatusError
	return errors.As(err, &s) && s.Code == http.StatusNotFound
}

// ---- being given access ----

// Health is what Werkbord says about itself, to anyone who asks.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Probe asks whether a Werkbord answers at base, without any credential. It is how the Team app notices that Werkbord is
// installed and running on this computer.
func Probe(ctx context.Context, base string) (Health, error) {
	b, err := CheckBase(base)
	if err != nil {
		return Health{}, err
	}
	var h Health
	if err := call(ctx, newHTTP(), b, "", "GET", "/api/health", nil, &h); err != nil {
		return Health{}, err
	}
	if h.Status == "" || h.Version == "" {
		return Health{}, ErrNotAnswering
	}
	return h, nil
}

// Connect is given access to the Werkbord at base, under name, using the controller's own token, which the caller read with
// the person's say-so and which is not kept: what comes back is a local access token, narrower and revocable, and that is
// all this package ever holds on to.
func Connect(ctx context.Context, base, controllerToken, name string) (string, error) {
	return connect(ctx, base, controllerToken, name, "")
}
func ConnectSync(ctx context.Context, base, controllerToken string) (string, error) {
	return connect(ctx, base, controllerToken, "Werkbord Team connector", "integration-v1")
}
func connect(ctx context.Context, base, controllerToken, name, scope string) (string, error) {
	b, err := CheckBase(base)
	if err != nil {
		return "", err
	}
	if controllerToken == "" {
		return "", errors.New("localwerkbord: no token to be given access with")
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := call(ctx, newHTTP(), b, controllerToken, "POST", "/api/local-access", map[string]string{"name": name, "scope": scope}, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.Token, "wba_") {
		return "", ErrNotAnswering
	}
	return out.Token, nil
}

// Self says whether this program's access is still good, and under what name.
func (c *Client) Self(ctx context.Context) (name string, err error) {
	var out struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, "GET", "/api/local-access/self", nil, &out); err != nil {
		return "", err
	}
	return out.Name, nil
}

// ---- projects and tasks ----

// Project is one of the person's own Werkbord projects.
type Project struct {
	ID      string
	Name    string
	Remotes []string
}

// Projects lists the person's projects with the Git remotes each has.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var list struct {
		Projects []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Repository *struct {
				Remotes []struct {
					URL string `json:"url"`
				} `json:"remotes"`
			} `json:"repository"`
		} `json:"projects"`
	}
	if err := c.do(ctx, "GET", "/api/projects", nil, &list); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(list.Projects))
	for _, p := range list.Projects {
		pr := Project{ID: p.ID, Name: p.Name}
		if p.Repository != nil {
			for _, r := range p.Repository.Remotes {
				pr.Remotes = append(pr.Remotes, r.URL)
			}
		}
		out = append(out, pr)
	}
	return out, nil
}

// ProjectFor picks the person's project whose Git remote is the repository, which is how a Team project is matched to
// the person's own checkout. It says what is wrong in words when there is none or more than one.
func (c *Client) ProjectFor(ctx context.Context, repository string) (Project, error) {
	want := domain.RepositoryKey(repository)
	if want == "" {
		return Project{}, errors.New("this Team project has no repository address, so there is nothing to match your Werkbord projects against")
	}
	ps, err := c.Projects(ctx)
	if err != nil {
		return Project{}, err
	}
	var match []Project
	for _, p := range ps {
		for _, r := range p.Remotes {
			if domain.RepositoryKey(r) == want {
				match = append(match, p)
				break
			}
		}
	}
	switch len(match) {
	case 0:
		return Project{}, fmt.Errorf("none of your Werkbord projects uses %s: add the repository to your Werkbord first", repository)
	case 1:
		return match[0], nil
	}
	return Project{}, fmt.Errorf("%d of your Werkbord projects use %s: this one cannot choose between them", len(match), repository)
}

// NewTask is what a person would type to make a task. There is no field for how it runs or when.
type NewTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SourceRef   string `json:"sourceRef,omitempty"`
	WorkBranch  string `json:"workBranch,omitempty"`
	BaseBranch  string `json:"baseBranch,omitempty"`
}

// MaxDescription is the longest description Werkbord takes, in bytes.
const MaxDescription = 256000

// CreateTask makes a task in one of the person's projects, in Werkbord's backlog, to be started there.
func (c *Client) CreateTask(ctx context.Context, projectID string, t NewTask) (string, error) {
	if len(t.Description) > MaxDescription {
		return "", fmt.Errorf("the ticket's text is %d bytes, more than a Werkbord task holds (%d)", len(t.Description), MaxDescription)
	}
	if err := checkID(projectID); err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, "POST", "/api/projects/"+projectID+"/tasks", t, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// StartRun starts a task's run with the task's own configuration. Nothing is sent that could choose an agent, a model, a
// policy, instructions or a runner; Werkbord's execution policies and the agent's own permission prompts decide the rest.
func (c *Client) StartRun(ctx context.Context, projectID, taskID string) (string, error) {
	if err := checkID(projectID); err != nil {
		return "", err
	}
	if err := checkID(taskID); err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, "POST", "/api/projects/"+projectID+"/tasks/"+taskID+"/runs", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// checkID refuses an identifier that could be anything but one: it goes into a path.
func checkID(id string) error {
	if id == "" || len(id) > 128 {
		return errors.New("an identifier is required")
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("%q is not an identifier", id)
		}
	}
	return nil
}

// ---- runs and questions ----

// FindRun says which project a run is in.
func (c *Client) FindRun(ctx context.Context, runID string) (projectID string, err error) {
	if err := checkID(runID); err != nil {
		return "", err
	}
	ps, err := c.Projects(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range ps {
		if err := c.do(ctx, "GET", "/api/projects/"+p.ID+"/runs/"+runID, nil, nil); err == nil {
			return p.ID, nil
		} else if !IsNotFound(err) {
			return "", err
		}
	}
	return "", &StatusError{Code: http.StatusNotFound, Message: "no such run on this computer"}
}

// StopRun stops a run.
func (c *Client) StopRun(ctx context.Context, runID string) error {
	pid, err := c.FindRun(ctx, runID)
	if err != nil {
		return err
	}
	return c.do(ctx, "POST", "/api/projects/"+pid+"/runs/"+runID+"/stop", map[string]any{}, nil)
}

// Question is an agent's question, as much of it as a person needs to answer.
type Question struct {
	ID            string   `json:"id"`
	RunID         string   `json:"runId"`
	Prompt        string   `json:"prompt"`
	Options       []string `json:"options"`
	AllowFreeText bool     `json:"allowFreeText"`
	State         string   `json:"state"`
}

// Answer answers one of an agent's questions with one of the options it offered (by position or exactly as written), or in
// words when the question allows it.
func (c *Client) Answer(ctx context.Context, runID, questionID, option, reply string) error {
	if err := checkID(questionID); err != nil {
		return err
	}
	pid, err := c.FindRun(ctx, runID)
	if err != nil {
		return err
	}
	var q Question
	if err := c.do(ctx, "GET", "/api/projects/"+pid+"/questions/"+questionID, nil, &q); err != nil {
		return err
	}
	if q.RunID != runID {
		return &StatusError{Code: http.StatusNotFound, Message: "that question belongs to another run"}
	}
	if q.State != "pending" {
		return &StatusError{Code: http.StatusConflict, Message: "that question has already been answered or closed"}
	}
	answer := reply
	if option != "" {
		if i, err := strconv.Atoi(option); err == nil && i >= 0 && i < len(q.Options) {
			answer = q.Options[i]
		} else {
			for _, o := range q.Options {
				if o == option {
					answer = o
				}
			}
		}
		if answer == "" {
			return &StatusError{Code: http.StatusBadRequest, Message: "that is not one of the options the agent offered"}
		}
	}
	if option == "" && len(q.Options) > 0 && !q.AllowFreeText {
		return &StatusError{Code: http.StatusBadRequest, Message: "the agent asked for one of its options, not for words"}
	}
	return c.do(ctx, "POST", "/api/projects/"+pid+"/questions/"+questionID+"/answer", map[string]string{"answer": answer}, nil)
}

// Status is what a runner says it is doing. It names no task, branch or path.
type Status struct {
	Controller string `json:"controller"`
	Version    string `json:"version,omitempty"`
	Projects   int    `json:"projects"`
	ActiveRuns int    `json:"activeRuns"`
	NeedsInput int    `json:"needsInput"`
	Runner     bool   `json:"runnerOnline"`
}

// Status summarises what this Werkbord is doing now.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var cc struct {
		Runners []struct {
			Online bool `json:"online"`
			Kind   string
		} `json:"runners"`
		Projects  []json.RawMessage `json:"projects"`
		Questions []json.RawMessage `json:"questions"`
		Runs      []json.RawMessage `json:"runs"`
	}
	if err := c.do(ctx, "GET", "/api/control-center", nil, &cc); err != nil {
		return Status{}, err
	}
	st := Status{Controller: "online", Projects: len(cc.Projects), ActiveRuns: len(cc.Runs), NeedsInput: len(cc.Questions)}
	for _, r := range cc.Runners {
		st.Runner = st.Runner || r.Online
	}
	return st, nil
}
