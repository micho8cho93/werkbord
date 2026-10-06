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

func TestTheNetworkSettingsAreTheCustomersOwnAddressesAndFiles(t *testing.T) {
	t.Setenv("WERKBORD_TEAM_DATA_DIR", "/srv/team")
	t.Setenv("WERKBORD_TEAM_ENDPOINTS", "team.example.org, 203.0.113.5 ,")
	t.Setenv("WERKBORD_TEAM_BOOTSTRAP_ADDR", "0.0.0.0:9440")
	t.Setenv("WERKBORD_TEAM_NETWORK_PORT", "4343")
	t.Setenv("WERKBORD_TEAM_NETWORK_NODE", "off")
	t.Setenv("WERKBORD_TEAM_NEBULA_DIR", "/opt/a,/opt/b")
	c := Load()
	if len(c.Endpoints) != 2 || c.BootstrapPort() != 9440 || c.NetworkPort != 4343 || c.RunNode || len(c.NebulaDirs) != 2 {
		t.Fatalf("%+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.PKIDir() != "/srv/team/pki" || c.SealingKeyPath() != "/srv/team/secrets/sealing.key" || c.NodeDir() != "/srv/team/network" {
		t.Errorf("%s %s %s", c.PKIDir(), c.SealingKeyPath(), c.NodeDir())
	}
	for name, mutate := range map[string]func(*Config){
		"a URL as an endpoint":             func(c *Config) { c.Endpoints = []string{"https://relay.example.com"} },
		"an endpoint with a port":          func(c *Config) { c.Endpoints = []string{"team.example.org:7440"} },
		"an endpoint with a path":          func(c *Config) { c.Endpoints = []string{"team.example.org/x"} },
		"a port that is not a port":        func(c *Config) { c.NetworkPort = 70000 },
		"a bootstrap address with no port": func(c *Config) { c.BootstrapAddr = "localhost" },
	} {
		c := Default()
		mutate(&c)
		if c.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	d := Default()
	d.Endpoints = []string{"2001:db8::1", "[2001:db8::2]"}
	if err := d.Validate(); err != nil {
		t.Errorf("IPv6 endpoints: %v", err)
	}
	t.Setenv("WERKBORD_TEAM_NETWORK_PORT", "nope")
	if Load().Validate() == nil {
		t.Error("an unreadable port was accepted")
	}
}
