package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestFolderBrowserListsOnlyVisibleDirectoriesAndRequiresSignIn(t *testing.T) {
	ts := newTestServer(t, nil)
	root := t.TempDir()
	for _, name := range []string{"plain", "repo/.git", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "private-file.txt"), []byte("not exposed"), 0600); err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Path    string `json:"path"`
		Folders []struct {
			Name, Path string
			Repository bool
		} `json:"folders"`
	}
	if code := do(t, "GET", ts.URL+"/api/folders?path="+url.QueryEscape(root), "", &listing); code != 200 {
		t.Fatalf("list: %d", code)
	}
	if listing.Path != root || len(listing.Folders) != 2 || listing.Folders[0].Name != "repo" || !listing.Folders[0].Repository || listing.Folders[1].Name != "plain" {
		t.Fatalf("folders = %+v", listing)
	}
	if code := do(t, "GET", ts.URL+"/api/folders?path=relative", "", nil); code != 400 {
		t.Fatalf("relative: %d", code)
	}
	locked := newTestServer(t, func(o *Options) { o.AuthRequired = true; o.Token = "test-secret" })
	if code := do(t, "GET", locked.URL+"/api/folders?path="+url.QueryEscape(root), "", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: %d", code)
	}
}
