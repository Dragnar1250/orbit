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

// lyricFrame (the Mac's lyrics window): the line at position p, the next one, its visibility
// (dimmed while paused), and whether the line changed since *last.
func lyricFrame(ls []Line, p float64, playing, showNext bool, last *int) (string, string, float64, bool) {
	i := lineAt(ls, p)
	changed := i != *last
	*last = i
	if i < 0 || i >= len(ls) || strings.TrimSpace(ls[i].Text) == "" {
		return "", "", 0, false
	}
	next := ""
	if showNext && i+1 < len(ls) {
		next = ls[i+1].Text
	}
	a := 1.0
	if !playing {
		a = 0.55
	}
	return ls[i].Text, next, a, changed
}

// macPlayersScript: one JavaScript-for-Automation run asks both players — one process instead of
// several, and a player that isn't open is skipped without starting it. One line per player.
const macPlayersScript = `function run() {
  var out = [], c = function (s) { return String(s === undefined || s === null ? "" : s).replace(/[\t\r\n]+/g, " "); };
  [["Spotify", "spotify"], ["Music", "music"]].forEach(function (p) {
    try {
      var a = Application(p[0]);
      if (!a.running()) return;
      var st = c(a.playerState());
      if (st === "stopped") { out.push(p[1] + "\tstopped"); return; }
      var t = a.currentTrack, art = "";
      if (p[1] === "spotify") { try { art = c(t.artworkUrl()); } catch (e) {} }
      out.push([p[1], st, c(t.name()), c(t.artist()), c(t.album()), c(a.playerPosition()), c(t.duration()), art].join("\t"));
    } catch (e) {}
  });
  return out.join("\n");
}`

// parseMacPlayers: the script's answer → the player to follow.
func parseMacPlayers(out string) (macNowPlaying, bool) {
	var sp, mu macNowPlaying
	var spOK, muOK bool
	for _, ln := range strings.Split(out, "\n") {
		n, ok := parseMacNowPlaying(ln)
		switch n.App {
		case "spotify":
			sp, spOK = n, ok
		case "music":
			mu, muOK = n, ok
		}
	}
	return pickMacPlayer(sp, mu, spOK, muOK)
}
