package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"devboard/internal/team/domain"
)

// `werkbord-team handoff` is "Open in my runner" for the command line. It runs on
// a developer's own computer, as them: it asks the Team server for the context of
// a ticket they hold, and either prints it or creates a task from it in their own
// local Werkbord. It is the developer's hands moving data between two programs
// they already use; neither server is given a way into the other, and a local
// Werkbord is only ever addressed on this computer (a loopback address).

const handoffUsage = `usage: werkbord-team handoff --ticket <WB-142 or id> [flags]

Gets a ticket you hold from your Team server and opens it in YOUR OWN Werkbord on
this computer. Without --runner it prints the handoff (JSON) instead.

flags:
  --server URL          the Team server (default: $WERKBORD_TEAM_SERVER, else http://127.0.0.1:7430)
  --project NAME|ID     the Team project (needed only if the ticket number alone is ambiguous)
  --ticket KEY|ID       the ticket, e.g. WB-142
  --runner URL          your local Werkbord, e.g. http://127.0.0.1:7420 (loopback addresses only)
  --local-project ID    which of your local Werkbord projects (default: the one whose remote is this repository)
  --out FILE            write the handoff JSON to FILE instead of stdout
  --prompt              print only the task text

Secrets come from the environment, never from flags, so they do not end up in shell history:
  WERKBORD_TEAM_TOKEN   your Team token (required)
  DEVBOARD_TOKEN        your local Werkbord's API token (required with --runner; it is sent only to --runner)
`

// maxLocalDescription is the individual Werkbord's limit on a task description.
const maxLocalDescription = 20000

func cmdHandoff(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, handoffUsage) }
	server := fs.String("server", firstEnv("WERKBORD_TEAM_SERVER", "http://127.0.0.1:7430"), "")
	project := fs.String("project", "", "")
	ticket := fs.String("ticket", "", "")
	runner := fs.String("runner", "", "")
	localProject := fs.String("local-project", "", "")
	out := fs.String("out", "", "")
	promptOnly := fs.Bool("prompt", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := os.Getenv("WERKBORD_TEAM_TOKEN")
	switch {
	case *ticket == "":
		return errors.New("--ticket is required (for example --ticket WB-142)")
	case token == "":
		return errors.New("set WERKBORD_TEAM_TOKEN to your Team token")
	}
	team, err := parseBase(*server)
	if err != nil {
		return fmt.Errorf("--server: %w", err)
	}
	hc := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	pid, tid, err := findTicket(ctx, hc, team, token, *project, *ticket)
	if err != nil {
		return err
	}
	var h handoff
	if err := doJSON(ctx, hc, http.MethodPost, team+"/api/team/v1/projects/"+pid+"/tickets/"+tid+"/handoff", token, nil, &h); err != nil {
		return fmt.Errorf("getting the handoff from %s: %w", team, err)
	}
	if h.Schema != "werkbord-team.handoff/v1" {
		return fmt.Errorf("this server sent a handoff of a kind (%q) this program does not understand; update werkbord-team", h.Schema)
	}

	if *runner == "" {
		return writeHandoff(h, *out, *promptOnly, stdout)
	}
	base, err := parseBase(*runner)
	if err != nil {
		return fmt.Errorf("--runner: %w", err)
	}
	if !isLoopbackHost(base) {
		return fmt.Errorf("--runner %s is not on this computer: a handoff only goes to your own Werkbord, so the address must be localhost or 127.0.0.1", *runner)
	}
	local := os.Getenv("DEVBOARD_TOKEN")
	if local == "" {
		return errors.New("set DEVBOARD_TOKEN to your local Werkbord's API token (it is in its data directory, in the file named token)")
	}
	if len(h.Prompt) > maxLocalDescription {
		return fmt.Errorf("the ticket's text is %d bytes, more than a local Werkbord task holds (%d); shorten the ticket, or use --out and attach the file", len(h.Prompt), maxLocalDescription)
	}
	lp := *localProject
	if lp == "" {
		if lp, err = findLocalProject(ctx, hc, base, local, h.Git.Repository); err != nil {
			return err
		}
	}
	var task struct {
		ID string `json:"id"`
	}
	body := map[string]any{"title": h.Ticket.Key + ": " + h.Ticket.Title, "description": h.Prompt}
	if err := doJSON(ctx, hc, http.MethodPost, base+"/api/projects/"+lp+"/tasks", local, body, &task); err != nil {
		return fmt.Errorf("creating the task in your local Werkbord: %w", err)
	}
	fmt.Fprintf(stdout, "Opened %s in your Werkbord at %s as task %s.\n", h.Ticket.Key, base, task.ID)
	fmt.Fprintf(stdout, "Work on branch %s. Start the run from your Werkbord; this ticket is yours, and nothing here ran on anyone else's machine.\n", h.Git.Branch)
	return nil
}

// handoff mirrors the server's Handoff; only what the command uses.
type handoff struct {
	Schema string `json:"schema"`
	Ticket struct {
		Key   string `json:"key"`
		Title string `json:"title"`
	} `json:"ticket"`
	Git struct {
		Repository string `json:"repository"`
		Branch     string `json:"branch"`
	} `json:"git"`
	Prompt string `json:"prompt"`
	raw    json.RawMessage
}

func (h *handoff) UnmarshalJSON(b []byte) error {
	type plain handoff
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*h = handoff(p)
	h.raw = append(json.RawMessage(nil), b...)
	return nil
}

func writeHandoff(h handoff, out string, promptOnly bool, stdout io.Writer) error {
	if promptOnly {
		_, err := fmt.Fprint(stdout, h.Prompt)
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, h.raw, "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	if out == "" {
		_, err := stdout.Write(pretty.Bytes())
		return err
	}
	if err := os.WriteFile(out, pretty.Bytes(), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote the handoff for %s to %s\n", h.Ticket.Key, out)
	return nil
}

func firstEnv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// parseBase checks an http(s) base address and returns it without a trailing slash.
func parseBase(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not an http or https address", s)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

func isLoopbackHost(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// doJSON sends a request with a bearer token and decodes the JSON answer. The
// token goes only to the address it was given for; redirects are not followed, so
// it cannot be handed on.
func doJSON(ctx context.Context, hc *http.Client, method, addr, token string, body, into any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, addr, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return fmt.Errorf("%s (%d)", e.Error.Message, res.StatusCode)
		}
		return fmt.Errorf("answered %d", res.StatusCode)
	}
	return json.Unmarshal(raw, into)
}

// findTicket resolves --project and --ticket to ids using the member's own view.
func findTicket(ctx context.Context, hc *http.Client, team, token, project, ticket string) (pid, tid string, err error) {
	var projects []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := doJSON(ctx, hc, http.MethodGet, team+"/api/team/v1/projects", token, nil, &projects); err != nil {
		return "", "", fmt.Errorf("listing your projects on %s: %w", team, err)
	}
	var candidates []string
	for _, p := range projects {
		if project == "" || p.ID == project || strings.EqualFold(p.Name, project) {
			candidates = append(candidates, p.ID)
		}
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("no project %q among yours", project)
	}
	key := strings.ToUpper(strings.TrimSpace(ticket))
	var found [][2]string
	for _, id := range candidates {
		var board struct {
			Tickets []struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			} `json:"tickets"`
		}
		if err := doJSON(ctx, hc, http.MethodGet, team+"/api/team/v1/projects/"+id+"/board", token, nil, &board); err != nil {
			return "", "", err
		}
		for _, k := range board.Tickets {
			if k.ID == ticket || strings.ToUpper(k.Key) == key {
				found = append(found, [2]string{id, k.ID})
			}
		}
	}
	switch len(found) {
	case 0:
		return "", "", fmt.Errorf("no ticket %q on your projects", ticket)
	case 1:
		return found[0][0], found[0][1], nil
	}
	return "", "", fmt.Errorf("%q is on more than one project; add --project", ticket)
}

// findLocalProject picks the local Werkbord project whose Git remote is the team project's repository.
func findLocalProject(ctx context.Context, hc *http.Client, base, token, repository string) (string, error) {
	want := domain.RepositoryKey(repository)
	if want == "" {
		return "", errors.New("this Team project has no repository address, so there is nothing to match your local projects against; pass --local-project")
	}
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
	if err := doJSON(ctx, hc, http.MethodGet, base+"/api/projects", token, nil, &list); err != nil {
		return "", fmt.Errorf("listing the projects in your local Werkbord: %w", err)
	}
	var match []string
	for _, p := range list.Projects {
		if p.Repository == nil {
			continue
		}
		for _, r := range p.Repository.Remotes {
			if domain.RepositoryKey(r.URL) == want {
				match = append(match, p.ID)
				break
			}
		}
	}
	switch len(match) {
	case 0:
		return "", fmt.Errorf("none of your local Werkbord projects has %s as a remote; add the repository to your Werkbord first, or pass --local-project", repository)
	case 1:
		return match[0], nil
	}
	return "", fmt.Errorf("%d of your local projects use %s; pass --local-project", len(match), repository)
}
