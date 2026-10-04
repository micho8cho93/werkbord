package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAreLoopbackWithTheirOwnDirectoryAndPort(t *testing.T) {
	c := Default()
	if !c.IsLoopback() || c.Addr == "0.0.0.0:7430" {
		t.Fatalf("Team must listen on this computer only until told otherwise: %s", c.Addr)
	}
	if !strings.HasSuffix(c.DataDir, "werkbord-team") && !strings.HasSuffix(c.DataDir, ".werkbord-team") {
		t.Fatalf("Team's data directory must not be the individual product's: %s", c.DataDir)
	}
	if c.Addr == "127.0.0.1:7420" {
		t.Fatal("Team must not take the individual product's port")
	}
	if filepath.Base(c.DBPath()) != "team.db" {
		t.Fatalf("db path %s", c.DBPath())
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentOverridesDefaultsAndIgnoresTheIndividualProductsVariables(t *testing.T) {
	t.Setenv("DEVBOARD_ADDR", "127.0.0.1:1111")
	t.Setenv("DEVBOARD_DATA_DIR", "/somewhere/else")
	t.Setenv("WERKBORD_TEAM_ADDR", "0.0.0.0:9000")
	t.Setenv("WERKBORD_TEAM_DATA_DIR", "/srv/team")
	c := Load()
	if c.Addr != "0.0.0.0:9000" || c.DataDir != "/srv/team" {
		t.Fatalf("%+v", c)
	}
	if c.IsLoopback() {
		t.Fatal("0.0.0.0 is not loopback")
	}
	if c.ClientAddr() != "127.0.0.1:9000" {
		t.Fatalf("client addr %s", c.ClientAddr())
	}
}

func TestValidate(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"no port":     func(c *Config) { c.Addr = "localhost" },
		"no data dir": func(c *Config) { c.DataDir = "" },
		"log format":  func(c *Config) { c.LogFormat = "xml" },
		"timeout":     func(c *Config) { c.ShutdownTimeout = 0 },
	} {
		c := Default()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for addr, loop := range map[string]bool{"127.0.0.1:1": true, "[::1]:1": true, "localhost:1": true, "10.0.0.5:1": false, ":1": false} {
		c := Default()
		c.Addr = addr
		if c.IsLoopback() != loop {
			t.Errorf("IsLoopback(%s) = %v", addr, !loop)
		}
	}
}
