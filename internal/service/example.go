package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/domain"
)

// The example project of the guided setup: a one-page site that says the person
// is getting started with Werkbord. The assistant writes it; these are the
// pieces for when there is no GitHub (a local repository to work in) and for when
// the assistant cannot (a template, committed on the person's say-so).

// ExampleRepoName is the default name of the example repository.
const ExampleRepoName = "werkbord-hello"

const exampleReadme = "# Getting started with Werkbord\n\nA small example project made by Werkbord's guided setup.\n"

const exampleIndex = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Getting started with Werkbord</title>
<style>
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; font: 18px/1.5 system-ui, sans-serif; background: #f6f6f2; color: #1c1c1a; }
  main { padding: 2rem; text-align: center; }
  h1 { font-size: clamp(2rem, 6vw, 3.5rem); margin: 0 0 .5rem; letter-spacing: -0.02em; }
  p { margin: 0; color: #55554f; }
</style>
</head>
<body>
<main>
  <h1>I'm getting started with Werkbord</h1>
  <p>Published from my own repository with GitHub Pages.</p>
</main>
</body>
</html>
`

const examplePagesWorkflow = `name: Deploy page

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/configure-pages@v5
      - uses: actions/upload-pages-artifact@v3
        with:
          path: .
      - id: deployment
        uses: actions/deploy-pages@v4
`

// exampleGit runs git in dir. When the person has not told Git who they are, the
// commit is made as Werkbord rather than failing: the identity is only for this
// command and nothing is written to their configuration.
func exampleGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, _ := exec.CommandContext(ctx, "git", "-C", dir, "config", "user.email").Output(); strings.TrimSpace(string(out)) == "" {
		env = append(env, "GIT_AUTHOR_NAME=Werkbord", "GIT_AUTHOR_EMAIL=setup@werkbord.invalid", "GIT_COMMITTER_NAME=Werkbord", "GIT_COMMITTER_EMAIL=setup@werkbord.invalid")
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], truncate(strings.TrimSpace(string(out)), 300))
	}
	return strings.TrimSpace(string(out)), nil
}

// CreateLocalExample makes a new repository on this computer, in the clone
// directory, with one commit, and registers it as a project. It is the example
// for a person who is not using GitHub. It never touches a directory that exists.
func (g *GitHubSetup) CreateLocalExample(ctx context.Context) (*ProjectDetail, error) {
	root := g.cloneRoot()
	dest := filepath.Join(root, ExampleRepoName)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dest); err != nil {
			break
		}
		if i > 20 {
			return nil, fmt.Errorf("%w: too many %s folders already exist in %s", domain.ErrConflict, ExampleRepoName, root)
		}
		dest = filepath.Join(root, fmt.Sprintf("%s-%d", ExampleRepoName, i))
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dest, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dest) // only ever the directory this call just made
		}
	}()
	if _, err := exampleGit(ctx, dest, "init", "-q", "-b", "main"); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrGit, err)
	}
	if err := os.WriteFile(filepath.Join(dest, "README.md"), []byte(exampleReadme), 0o644); err != nil {
		return nil, err
	}
	if _, err := exampleGit(ctx, dest, "add", "."); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrGit, err)
	}
	if _, err := exampleGit(ctx, dest, "commit", "-q", "-m", "Start the example project"); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrGit, err)
	}
	p, err := g.Projects.Register(ctx, dest, "")
	if err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

// ScaffoldResult says where the template was committed.
type ScaffoldResult struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

// ScaffoldExample commits the template page and its Pages workflow to the
// example project's current branch. It is the fallback for when the assistant
// cannot do the task. It refuses a project with uncommitted changes and never
// overwrites a file.
func (g *GitHubSetup) ScaffoldExample(ctx context.Context, projectID string) (*ScaffoldResult, error) {
	p, err := g.Projects.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	root := p.RepoPath
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("%w: this project has no repository", domain.ErrInvalid)
	}
	if st, err := exampleGit(ctx, root, "status", "--porcelain"); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrGit, err)
	} else if st != "" {
		return nil, fmt.Errorf("%w: the project has uncommitted changes; commit or discard them first", domain.ErrConflict)
	}
	files := map[string]string{
		"index.html":                  exampleIndex,
		".github/workflows/pages.yml": examplePagesWorkflow,
	}
	for name := range files {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			return nil, fmt.Errorf("%w: %s already exists, so the template would overwrite it", domain.ErrConflict, name)
		}
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := exampleGit(cctx, root, "add", "index.html", ".github/workflows/pages.yml"); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrGit, err)
	}
	if _, err := exampleGit(cctx, root, "commit", "-q", "-m", "Add a starter page and its Pages workflow"); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrGit, err)
	}
	branch, _ := exampleGit(cctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	sha, _ := exampleGit(cctx, root, "rev-parse", "HEAD")
	return &ScaffoldResult{Branch: branch, Commit: sha}, nil
}
