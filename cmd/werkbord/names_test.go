package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBothCommandNamesWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows shim is a .cmd file; the links are Unix")
	}
	// A new install: werkbord, and devboard beside it.
	real := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }
	dir := real(t.TempDir())
	exe := filepath.Join(dir, "werkbord")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	made, err := ensureCommandNames(exe)
	if err != nil || made != filepath.Join(dir, "devboard") {
		t.Fatalf("made %q, %v", made, err)
	}
	if target, _ := os.Readlink(made); target != "werkbord" {
		t.Fatalf("devboard points at %q", target)
	}
	// Run again (or through the alias): nothing to do, nothing touched.
	if made, err := ensureCommandNames(filepath.Join(dir, "devboard")); err != nil || made != "" {
		t.Fatalf("second run made %q, %v", made, err)
	}

	// An install from before the rename: devboard, and werkbord beside it.
	old := real(t.TempDir())
	legacy := filepath.Join(old, "devboard")
	if err := os.WriteFile(legacy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if made, err := ensureCommandNames(legacy); err != nil || made != filepath.Join(old, "werkbord") {
		t.Fatalf("legacy made %q, %v", made, err)
	}

	// A name already taken by something else is left alone; other executables are not renamed.
	taken := t.TempDir()
	if err := os.WriteFile(filepath.Join(taken, "werkbord"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taken, "devboard"), []byte("someone else's"), 0o755); err != nil {
		t.Fatal(err)
	}
	if made, err := ensureCommandNames(filepath.Join(taken, "werkbord")); err != nil || made != "" {
		t.Fatalf("taken: made %q, %v", made, err)
	}
	if b, _ := os.ReadFile(filepath.Join(taken, "devboard")); string(b) != "someone else's" {
		t.Fatal("an existing file was replaced")
	}
	other := filepath.Join(t.TempDir(), "werkbord.test")
	_ = os.WriteFile(other, nil, 0o755)
	if made, err := ensureCommandNames(other); err != nil || made != "" {
		t.Fatalf("other name: made %q, %v", made, err)
	}
}
