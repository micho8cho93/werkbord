package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"werkbord/internal/assistant/provider"
	"werkbord/internal/productmap"
)

func TestProductMapAPIIsOwnerOnlyAndCannotResolveArbitraryPaths(t *testing.T) {
	sc := newScoped(t)
	server := newRunsServer(t, func(o *Options) {
		var err error
		o.ProductMaps, err = productmap.New(context.Background(), productmap.Config{Store: o.Store, Providers: provider.NewRegistry(), WorkDir: filepath.Join(t.TempDir(), "analysis")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(o.ProductMaps.Close)
		o.AuthRequired = true
		o.Token = sc.owner
		o.LocalAccess = sc.store
	})
	route := "/api/projects/" + server.project.ID + "/product-map"
	narrow := sc.mint(t, "Unrelated program")
	for _, r := range []struct{ method, path, body string }{{"GET", route, ""}, {"GET", route + "/snapshot", ""}, {"POST", route + "/jobs", `{"mode":"generate","provider":"codex"}`}, {"POST", route + "/jobs/mapjob_fake/cancel", `{}`}} {
		if code, body := call(t, r.method, server.url+r.path, narrow, r.body); code != http.StatusForbidden {
			t.Fatalf("narrow grant reached map API: %d %s", code, body)
		}
		if code, _ := call(t, r.method, server.url+r.path, "", r.body); code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated map API: %d", code)
		}
	}
	if code, body := call(t, "GET", server.url+route, sc.owner, ""); code != http.StatusOK {
		t.Fatalf("owner view: %d %s", code, body)
	}
	if code, _ := call(t, "GET", server.url+"/api/projects/missing/product-map", sc.owner, ""); code != http.StatusNotFound {
		t.Fatal("missing project not scoped", code)
	}
	if code, _ := call(t, "POST", server.url+route+"/jobs", sc.owner, `{"mode":"generate","provider":"codex","path":"/etc"}`); code != http.StatusBadRequest {
		t.Fatal("arbitrary path accepted", code)
	}
}
