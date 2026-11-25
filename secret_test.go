package main

import (
	"os"
	"strings"
	"testing"
)

func TestSecrets(t *testing.T) {
	if sealSecret("") != "" || openSecret("") != "" {
		t.Fatal("empty")
	}
	sealed := sealSecret("REFRESH-123")
	if !strings.HasPrefix(sealed, sealedPrefix) || strings.Contains(sealed, "REFRESH") || openSecret(sealed) != "REFRESH-123" {
		t.Fatalf("round trip %q", sealed)
	}
	if openSecret("plain-from-1.17") != "plain-from-1.17" {
		t.Fatal("1.17's plain value must still work")
	}
	if openSecret(sealedPrefix+"!!!notbase64") != "" || openSecret(sealedPrefix+"aGVsbG8=") != "" {
		t.Fatal("a value this PC can't open must give nothing")
	}
	// config.json never holds it in the clear, and loads back
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfg = defaultConfig()
	cfg.SpotifyClientID, cfg.SpotifyRefresh, cfg.SpotifyScopes = "abcdef0123456789abcdef0123456789", "TOP-SECRET", "user-read-playback-state"
	saveConfigSoon()
	saveConfigIfDue(true)
	b, err := os.ReadFile(configPath())
	if err != nil || strings.Contains(string(b), "TOP-SECRET") || !strings.Contains(string(b), sealedPrefix) {
		t.Fatalf("written in the clear? %v", err)
	}
	cfg = defaultConfig()
	loadConfig()
	if cfg.SpotifyRefresh != "TOP-SECRET" || cfg.SpotifyScopes == "" {
		t.Fatalf("loaded %q", cfg.SpotifyRefresh)
	}
	// a config.json from another PC: disconnected, nothing else lost
	os.WriteFile(configPath(), []byte(strings.Replace(string(b), sealedPrefix, sealedPrefix+"AAAA", 1)), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.SpotifyRefresh != "" || cfg.SpotifyScopes != "" || cfg.SpotifyClientID == "" {
		t.Fatalf("foreign sign-in: %q %q %q", cfg.SpotifyRefresh, cfg.SpotifyScopes, cfg.SpotifyClientID)
	}
}
