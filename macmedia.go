package main

// Mac media: Spotify and Apple Music answer AppleScript (macOS 15.4+ no longer tells other apps
// what plays through the system's "Now Playing"). The scripts print one tab-separated line;
// parsing it lives here so it is tested on every platform.

import (
	"strconv"
	"strings"
)

type macNowPlaying struct {
	App, State, Title, Artist, Album, Art string
	Pos, Dur                              float64
}

// scripts per app (only run when the app is already running: a "tell" never launches it)
const (
	macSpotifyScript = `tell application "Spotify"
	if player state is stopped then return "spotify" & tab & "stopped"
	set t to current track
	return "spotify" & tab & (player state as text) & tab & (name of t) & tab & (artist of t) & tab & (album of t) & tab & (player position as text) & tab & ((duration of t) as text) & tab & (artwork url of t)
end tell`
	macMusicScript = `tell application "Music"
	if player state is stopped then return "music" & tab & "stopped"
	set t to current track
	return "music" & tab & (player state as text) & tab & (name of t) & tab & (artist of t) & tab & (album of t) & tab & (player position as text) & tab & ((duration of t) as text) & tab & ""
end tell`
)

// macNumber reads AppleScript numbers in any locale ("12,5" in Turkish, "1.234,5", "3.0E+2").
func macNumber(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.Contains(s, ",") {
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") { // comma is the decimal mark
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v != v || v < 0 || v > 1e7 {
		return 0
	}
	return v
}

// parseMacNowPlaying: one script's answer. ok=false when nothing (useful) plays there.
func parseMacNowPlaying(out string) (macNowPlaying, bool) {
	f := strings.Split(strings.TrimRight(out, "\r\n"), "\t")
	var n macNowPlaying
	if len(f) < 2 {
		return n, false
	}
	n.App, n.State = strings.TrimSpace(f[0]), strings.TrimSpace(f[1])
	if n.State != "playing" && n.State != "paused" || len(f) < 7 {
		return n, false
	}
	n.Title, n.Artist, n.Album = strings.TrimSpace(f[2]), strings.TrimSpace(f[3]), strings.TrimSpace(f[4])
	n.Pos, n.Dur = macNumber(f[5]), macNumber(f[6])
	if n.App == "spotify" && n.Dur > 10000 { // Spotify says milliseconds
		n.Dur /= 1000
	}
	if len(f) > 7 && strings.HasPrefix(strings.TrimSpace(f[7]), "https://") {
		n.Art = strings.TrimSpace(f[7])
	}
	if n.Title == "" || looksLikeID(n.Title) || strings.HasPrefix(n.Title, "spotify:") || (n.App == "spotify" && (n.Title == "Advertisement" || n.Title == "Spotify")) { // ads
		return n, false
	}
	if n.Dur > 0 && n.Pos > n.Dur {
		n.Pos = n.Dur
	}
	return n, true
}

// pickMacPlayer: with both apps open, the one playing wins (then Spotify).
func pickMacPlayer(sp, mu macNowPlaying, spOK, muOK bool) (macNowPlaying, bool) {
	switch {
	case spOK && sp.State == "playing":
		return sp, true
	case muOK && mu.State == "playing":
		return mu, true
	case spOK:
		return sp, true
	case muOK:
		return mu, true
	}
	return macNowPlaying{}, false
}
