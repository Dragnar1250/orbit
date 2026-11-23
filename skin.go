package main

// Desktop skin: the second kind of full screen — a big clock, "Continue playing", a now playing
// card or cover flow, a glass player bar with search, lyrics and a light round the edges.
// This file holds the parts that don't need Windows (names, search links, layout maths).

import (
	"net/url"
	"strings"
)

var uiThemeNames = []string{"Like Windows", "Dark", "Light"}
var fsKindNames = []string{"Music display", "Desktop skin"}
var skinBgNames = []string{"Cover, sharp", "Cover, blurred", "Black hole", "Aurora", "Nebula", "Colour flow", "Starfield", "Plain", "Music video"}
var skinClockFontNames = []string{"Tall", "Thin", "Heavy", "Serif"}
var skinClockColorNames = []string{"Album glow", "White", "Sunset", "Ice", "Neon"}
var skinCentreNames = []string{"Now playing card", "Cover flow", "Nothing"}
var serviceNames = []string{"Automatic", "Spotify", "Spotify Web Player", "Apple Music", "YouTube Music", "SoundCloud", "Amazon Music", "YouTube"}

const (
	svcAuto = iota
	svcSpotify
	svcSpotifyWeb
	svcApple
	svcYTMusic
	svcSoundCloud
	svcAmazon
	svcYouTube
)

// serviceColors: the dot in the player bar and the menu.
var serviceColors = []uint32{0xFFB0B0BC, 0xFF1ED760, 0xFF1ED760, 0xFFFA2D48, 0xFFFF0033, 0xFFFF5500, 0xFF25D1DA, 0xFFFF0000}

// serviceFor: "Automatic" follows the app that is playing (its Windows media id).
func serviceFor(choice int, source string) int {
	if choice > svcAuto && choice < len(serviceNames) {
		return choice
	}
	s := strings.ToLower(source)
	switch {
	case strings.Contains(s, "spotify"):
		return svcSpotify
	case strings.Contains(s, "applemusic") || strings.Contains(s, "apple.music") || strings.Contains(s, "itunes"):
		return svcApple
	case strings.Contains(s, "soundcloud"):
		return svcSoundCloud
	case strings.Contains(s, "amazon"):
		return svcAmazon
	case strings.Contains(s, "chrome") || strings.Contains(s, "msedge") || strings.Contains(s, "firefox") ||
		strings.Contains(s, "opera") || strings.Contains(s, "brave") || strings.Contains(s, "vivaldi") || strings.Contains(s, "youtube"):
		return svcYTMusic
	}
	return svcSpotifyWeb
}

// searchURL: where a search (or a click on a song) takes you. Only our own fixed addresses,
// with the words escaped — nothing typed can turn into a different link or a command.
func searchURL(service int, q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len([]rune(q)) > 200 {
		q = string([]rune(q)[:200])
	}
	switch service {
	case svcSpotify:
		return "spotify:search:" + pathEsc(q)
	case svcApple:
		return "https://music.apple.com/search?term=" + url.QueryEscape(q)
	case svcYTMusic:
		return "https://music.youtube.com/search?q=" + url.QueryEscape(q)
	case svcSoundCloud:
		return "https://soundcloud.com/search?q=" + url.QueryEscape(q)
	case svcAmazon:
		return "https://music.amazon.com/search/" + pathEsc(q)
	case svcYouTube:
		return "https://www.youtube.com/results?search_query=" + url.QueryEscape(q)
	}
	return "https://open.spotify.com/search/" + pathEsc(q)
}

// serviceLabel: what the source dot says when you point at it.
func serviceLabel(choice int, source string) string {
	s := serviceFor(choice, source)
	if choice == svcAuto {
		return "Automatic · " + serviceNames[s]
	}
	return serviceNames[s]
}

// coverFlow: positions of the covers around the current one (index 0 = the current song).
// Returns x offset (in cover widths), scale and brightness for slot d = -3..3.
func coverFlow(d int) (dx, scale, light float64) {
	a := d
	if a < 0 {
		a = -a
	}
	switch a {
	case 0:
		return 0, 1, 1
	case 1:
		dx, scale, light = 0.78, 0.74, 0.72
	case 2:
		dx, scale, light = 1.36, 0.56, 0.5
	default:
		dx, scale, light = 1.8, 0.42, 0.32
	}
	if d < 0 {
		dx = -dx
	}
	return
}

// editText applies one typed character / key to a search box (rune 8 = backspace).
func editText(s string, r rune, max int) string {
	switch {
	case r == 8:
		rs := []rune(s)
		if len(rs) > 0 {
			return string(rs[:len(rs)-1])
		}
		return s
	case r == 0x7F: // Ctrl+Backspace: the last word
		s = strings.TrimRight(s, " ")
		if i := strings.LastIndex(s, " "); i >= 0 {
			return s[:i+1]
		}
		return ""
	case r < 32:
		return s
	}
	if len([]rune(s)) >= max {
		return s
	}
	return s + string(r)
}

// pathEsc escapes everything but letters and digits (PathEscape keeps & = + : @ which some
// players read as separators).
func pathEsc(q string) string { return strings.ReplaceAll(url.QueryEscape(q), "+", "%20") }

// looksLikeID: a "title" that is really a hash or id (some players send the file's name, e.g.
// 79ead9f36e7f4fc835…): 16+ hex digits / letters with no spaces.
func looksLikeID(s string) bool {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "."); i > 0 && len(s)-i <= 5 { // "….mp3"
		s = s[:i]
	}
	if len(s) < 16 || strings.ContainsAny(s, " ") {
		return false
	}
	hex := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			hex++
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return hex >= 16
}
