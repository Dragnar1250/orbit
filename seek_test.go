package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSeekHold(t *testing.T) {
	m := &Media{}
	m.SetTrack("Song", "Artist", "", "Spotify")
	m.SetDuration(keyOf("Song", "Artist"), 200)
	m.SetPosition(30, true)
	m.Seek(120)
	if p := m.Snapshot().Pos; p < 119.9 || p > 121 {
		t.Fatalf("seek not shown at once: %v", p)
	}
	// the player still reports the old place for a moment: ignored
	m.SetPosition(31, true)
	if p := m.Snapshot().Pos; p < 119.9 {
		t.Fatalf("old position pulled it back: %v", p)
	}
	// the player arrived: followed again, and the hold ends
	m.SetPosition(120.4, true)
	if p := m.Snapshot().Pos; p < 120.3 || p > 121 {
		t.Fatalf("arrived %v", p)
	}
	m.SetPosition(10, true) // a real jump afterwards (e.g. in Spotify itself) is followed
	if p := m.Snapshot().Pos; p > 11 {
		t.Fatalf("after the hold %v", p)
	}
	// limits
	m.Seek(-5)
	if p := m.Snapshot().Pos; p > 0.5 {
		t.Fatalf("negative %v", p)
	}
	m.Seek(999)
	if p := m.Snapshot().Pos; p > 200 {
		t.Fatalf("past the end %v", p)
	}
	// the hold runs out even when the player never moves
	m.Seek(50)
	m.seekUntil = time.Now().Add(-time.Millisecond)
	m.SetPosition(12, false)
	if p := m.Snapshot().Pos; p > 13 {
		t.Fatalf("hold never ends %v", p)
	}
	// play state still follows during the hold
	m.SetPosition(12, true)
	m.Seek(100)
	m.SetPosition(12, false)
	if m.Snapshot().Playing {
		t.Fatal("pause during the hold lost")
	}
	// a seek never counts as a new song
	v := m.Snapshot().Version
	m.Seek(30)
	if m.Snapshot().Version != v {
		t.Fatal("seek changed the version")
	}
}

func TestSpotifySettingsSafety(t *testing.T) {
	c := defaultConfig()
	c.SpotifyClientID, c.SpotifyRefresh = "abcdef0123456789abcdef0123456789", "SECRET"
	if c.setField("SpotifyRefresh", json.RawMessage(`"evil"`)) == nil || c.SpotifyRefresh != "SECRET" {
		t.Fatal("the page must not set the sign-in")
	}
	if err := c.setField("FsTextSize", json.RawMessage(`70`)); err != nil || c.SpotifyRefresh != "SECRET" {
		t.Fatal("other settings must keep the sign-in")
	}
	if err := c.setField("SpotifyClientID", json.RawMessage(`"abcdef0123456789abcdef0123456789"`)); err != nil || c.SpotifyRefresh != "SECRET" {
		t.Fatal("same client id kept the sign-in")
	}
	if err := c.setField("SpotifyClientID", json.RawMessage(`"ffffff0123456789abcdef0123456789"`)); err != nil || c.SpotifyRefresh != "" {
		t.Fatal("another app must drop the old sign-in")
	}
	for _, bad := range []string{`"a b"`, `"x/y"`, `"<script>"`, `"` + strings.Repeat("a", 65) + `"`} {
		c.setField("SpotifyClientID", json.RawMessage(bad))
		if c.SpotifyClientID != "" {
			t.Fatalf("kept %s", bad)
		}
	}

	// the state the settings page reads never carries the refresh token
	cfg = defaultConfig()
	cfg.SpotifyClientID, cfg.SpotifyRefresh = "abcdef0123456789abcdef0123456789", "SECRET-REFRESH"
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.Get("http://127.0.0.1:" + fmt.Sprint(srv.port) + "/api/state?t=" + srv.token)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	if strings.Contains(string(b), "SECRET-REFRESH") {
		t.Fatal("refresh token sent to the page")
	}
	var st struct {
		Spotify map[string]any `json:"spotify"`
	}
	json.Unmarshal(b, &st)
	if st.Spotify["connected"] != true || !strings.HasSuffix(st.Spotify["redirect"].(string), "/spotify/callback") {
		t.Fatalf("spotify state %v", st.Spotify)
	}
	// disconnect
	body := strings.NewReader(`{"name":"spotifyDisconnect","value":true}`)
	if r, _ := http.Post("http://127.0.0.1:"+fmt.Sprint(srv.port)+"/api/action?t="+srv.token, "application/json", body); r.StatusCode != 200 {
		t.Fatal("disconnect")
	}
	if cfg.SpotifyRefresh != "" || cfg.SpotifyClientID == "" {
		t.Fatal("disconnect must drop only the sign-in")
	}
}
