package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoopbackHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "127.1.2.3", "::1", "::ffff:127.0.0.1", "localhost", "LOCALHOST"} {
		if !isLoopbackHost(host) {
			t.Errorf("rejected %q", host)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "::", "192.168.0.1", "example.com", "localhost.evil.test", "127.1", "localhost:80", "[::1]"} {
		if isLoopbackHost(host) {
			t.Errorf("accepted %q", host)
		}
	}
}

func TestRequestHost(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST:20203", "127.0.0.1:80", "127.1.2.3", "[::1]", "[::1]:20203", "[::ffff:127.0.0.1]:80"} {
		if !validRequestHost(host) {
			t.Errorf("rejected %q", host)
		}
	}
	for _, host := range []string{"", "evil.test", "localhost.evil.test:80", "127.0.0.1.evil.test", "localhost@evil.test", "0.0.0.0", "[::]", "::1", "localhost:", "localhost:bad", "localhost:0", "localhost:65536", "localhost:+80", "127.0.0.1/path"} {
		if validRequestHost(host) {
			t.Errorf("accepted %q", host)
		}
	}
}

func TestConfiguration(t *testing.T) {
	executable, _ := os.Executable()
	for _, platform := range []string{"darwin", "windows"} {
		cfg := defaultConfig(platform)
		cfg.executable = executable
		if err := cfg.validate(platform); err != nil {
			t.Fatal(err)
		}
		if cfg.listenAddress() != "127.0.0.1:20203" {
			t.Fatal(cfg.listenAddress())
		}
	}
	for _, tc := range []struct {
		name   string
		modify func(*serverConfig)
	}{
		{"host", func(c *serverConfig) { c.host = "0.0.0.0" }},
		{"port", func(c *serverConfig) { c.port = 65536 }},
		{"timeout", func(c *serverConfig) { c.timeout = 0 }},
		{"chars", func(c *serverConfig) { c.maxInputChars = 0 }},
		{"executable", func(c *serverConfig) { c.executable = "" }},
		{"directory", func(c *serverConfig) { c.executable = t.TempDir() }},
		{"origin", func(c *serverConfig) { c.allowedOrigins = []string{"*"} }},
		{"preset", func(c *serverConfig) { c.defaultPreset = "\x00" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig("windows")
			cfg.executable = executable
			tc.modify(&cfg)
			if err := cfg.validate("windows"); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	cfg := defaultConfig("linux")
	if err := cfg.validate("linux"); err == nil || !strings.Contains(err.Error(), "unsupported OS") {
		t.Fatalf("got %v", err)
	}
	cfg.host = "localhost"
	if cfg.listenAddress() != "127.0.0.1:20203" {
		t.Fatal("localhost must bind to a literal address")
	}
	cfg.host = "::1"
	if cfg.listenAddress() != "[::1]:20203" {
		t.Fatal("incorrect IPv6 address")
	}
}

func TestFlags(t *testing.T) {
	cfg, version, err := parseFlags([]string{"--host", "::1", "--port", "20204", "--aquestalkplayer-path", "player", "--default-preset", "slow", "--timeout", "2m", "--max-input-chars", "2000", "--allowed-origin", "https://one.test https://two.test", "--allowed-origin", "https://three.test"}, "windows", io.Discard)
	if err != nil || version || cfg.host != "::1" || cfg.port != 20204 || cfg.executable != "player" || cfg.defaultPreset != "slow" || cfg.timeout != 2*time.Minute || cfg.maxInputChars != 2000 || len(cfg.allowedOrigins) != 3 {
		t.Fatalf("%+v version=%v err=%v", cfg, version, err)
	}
	_, version, err = parseFlags([]string{"--version"}, "linux", io.Discard)
	if err != nil || !version {
		t.Fatal("version must work without a supported OS or executable")
	}
	for _, args := range [][]string{{"--timeout", "x"}, {"unexpected"}, {"--unknown"}} {
		if _, _, err := parseFlags(args, "darwin", io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
