package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "")
	got, err := parseInterspersed(fs, []string{"./repo", "--name", "Demo app"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"./repo"}) || *name != "Demo app" {
		t.Fatalf("positional=%v name=%q", got, *name)
	}
}
