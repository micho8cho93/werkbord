package github

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
)

// LocalClone is a clone of a GitHub repository found on this computer.
type LocalClone struct {
	Path string
	Repo Repo
}

// ScanOptions bounds a search for local clones.
type ScanOptions struct {
	// Roots are the directories to look in.
	Roots []string
	// MaxDepth is how many directories below a root to look: 3 finds ~/code/org/app.
	MaxDepth int
	// MaxDirs bounds the work, whatever the disk looks like. Default 20000.
	MaxDirs int
}

// DefaultScanRoots are where people keep code, under their home directory. Only
// those that exist are searched. The home directory itself is searched one level
// deep, which finds a clone dropped straight into it.
func DefaultScanRoots(home string) []string {
	var roots []string
	for _, d := range []string{"code", "Code", "dev", "Dev", "src", "projects", "Projects", "Developer", "repos", "Repos", "workspace", "work", "git", "GitHub", "github",
		"Documents/GitHub", "Documents/Code", "Documents/Projects", "Desktop"} {
		roots = append(roots, filepath.Join(home, filepath.FromSlash(d)))
	}
	return roots
}

// skipDirs are never entered: they are enormous, and nothing the user would
// choose to work on lives in them.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, "venv": true, ".venv": true,
	"Library": true, "AppData": true, "Applications": true, "Pictures": true, "Music": true, "Movies": true,
}

// ScanLocalClones finds git repositories under the roots and reads which GitHub
// repository each one's remotes point at. It reads each repository's .git/config
// and nothing else: it runs no git, touches no network, and changes nothing.
// A repository is not searched inside, and a linked worktree (whose .git is a
// file) is not a clone of its own.
func ScanLocalClones(ctx context.Context, opt ScanOptions) []LocalClone {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 3
	}
	if opt.MaxDirs <= 0 {
		opt.MaxDirs = 20000
	}
	var out []LocalClone
	visited := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if ctx.Err() != nil || visited >= opt.MaxDirs {
			return
		}
		visited++
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if fi.IsDir() {
				for _, r := range remotesOf(filepath.Join(dir, ".git", "config")) {
					out = append(out, LocalClone{Path: dir, Repo: r})
				}
			}
			return // a repository, or a linked worktree of one: never look inside
		}
		if depth >= opt.MaxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}
	// Two roots can be one directory (~/code and ~/Code on a case-insensitive disk),
	// and must not be searched twice. Symlinked directories are never entered, so
	// there are no loops to guard against below a root.
	var roots []os.FileInfo
rootLoop:
	for _, root := range opt.Roots {
		fi, err := os.Stat(root)
		if err != nil || !fi.IsDir() {
			continue
		}
		for _, r := range roots {
			if os.SameFile(r, fi) {
				continue rootLoop
			}
		}
		roots = append(roots, fi)
		walk(root, 0)
	}
	return out
}

// remotesOf reads the GitHub repositories a .git/config's remotes point at.
func remotesOf(configPath string) []Repo {
	f, err := os.Open(configPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var repos []Repo
	inRemote := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "["):
			inRemote = strings.HasPrefix(line, "[remote ")
		case inRemote && strings.HasPrefix(line, "url"):
			_, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if r, ok := ParseRemote(strings.TrimSpace(v)); ok && (r.Host == "github.com") {
				repos = append(repos, r)
			}
		}
	}
	return repos
}

// ClonesAt reads which GitHub repositories the repository whose working tree is
// at dir has remotes for. It is empty if dir is not the top of a clone (a
// subdirectory, a linked worktree) or has no GitHub remote.
func ClonesAt(dir string) []Repo {
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !fi.IsDir() {
		return nil
	}
	return remotesOf(filepath.Join(dir, ".git", "config"))
}
