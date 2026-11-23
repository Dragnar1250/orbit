package main

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestServiceFor(t *testing.T) {
	cases := map[string]int{
		"Spotify.exe": svcSpotify, "SpotifyAB.SpotifyMusic_zpdnekdrzrea0!Spotify": svcSpotify,
		"AppleInc.AppleMusicWin_nzyj5cx40ttqa!App": svcApple, "Chrome": svcYTMusic, "MSEdge": svcYTMusic,
		"308046B0AF4A39CB": svcSpotifyWeb, "": svcSpotifyWeb, "Firefox": svcYTMusic, "SoundCloud.exe": svcSoundCloud,
	}
	for src, want := range cases {
		if got := serviceFor(svcAuto, src); got != want {
			t.Errorf("auto %q = %s, want %s", src, serviceNames[got], serviceNames[want])
		}
	}
	for i := 1; i < len(serviceNames); i++ {
		if serviceFor(i, "Spotify.exe") != i {
			t.Errorf("explicit choice %d ignored", i)
		}
	}
	for _, bad := range []int{-5, 99} {
		if s := serviceFor(bad, "spotify"); s != svcSpotify {
			t.Errorf("out of range choice %d → %d", bad, s)
		}
	}
	if len(serviceColors) != len(serviceNames) {
		t.Fatal("a colour per service")
	}
}

func TestSearchURL(t *testing.T) {
	q := "Lil Tecca — Love Me & more?/#"
	for s := 0; s < len(serviceNames); s++ {
		u := searchURL(s, q)
		if strings.ContainsAny(u, " #&?") && !strings.Contains(u, "?q=") && !strings.Contains(u, "?term=") && !strings.Contains(u, "?search_query=") {
			t.Errorf("%s: unescaped %q", serviceNames[s], u)
		}
		if strings.Count(u, "?") > 1 || strings.Contains(u, "#") || strings.Contains(u, " ") {
			t.Errorf("%s: query leaks into the link: %q", serviceNames[s], u)
		}
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "spotify:search:") {
			t.Errorf("%s: odd link %q", serviceNames[s], u)
		}
	}
	if u := searchURL(svcSpotify, "a b"); u != "spotify:search:a%20b" {
		t.Errorf("spotify app link %q", u)
	}
	// anything typed stays inside our address
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 3000; i++ {
		b := make([]rune, r.Intn(300))
		for k := range b {
			b[k] = rune(r.Intn(0x2FFF))
		}
		for s := 0; s < len(serviceNames); s++ {
			u := searchURL(s, string(b))
			if !utf8.ValidString(u) || strings.ContainsAny(u, " \t\r\n\"<>\\^`{|}#") {
				t.Fatalf("%s: bad link for %q: %q", serviceNames[s], string(b), u)
			}
			if len(u) > 2000 {
				t.Fatalf("link too long: %d", len(u))
			}
		}
	}
}

func TestEditText(t *testing.T) {
	s := ""
	for _, r := range "Juice WRLD" {
		s = editText(s, r, 80)
	}
	if s != "Juice WRLD" {
		t.Fatal(s)
	}
	if s = editText(s, 8, 80); s != "Juice WRL" {
		t.Fatal(s)
	}
	if s = editText(s, 0x7F, 80); s != "Juice " {
		t.Fatalf("ctrl+backspace %q", s)
	}
	if editText("", 8, 80) != "" || editText("abc", 13, 80) != "abc" || editText("ab", 'c', 2) != "ab" {
		t.Fatal("edge cases")
	}
	if editText("çğ", 8, 80) != "ç" {
		t.Fatal("backspace must remove a whole letter")
	}
}

func TestCoverFlow(t *testing.T) {
	prevX := -9.0
	for d := -3; d <= 3; d++ {
		dx, sc, li := coverFlow(d)
		if dx <= prevX || sc <= 0 || sc > 1 || li <= 0 || li > 1 {
			t.Fatalf("slot %d: %v %v %v", d, dx, sc, li)
		}
		prevX = dx
		if d != 0 {
			ndx, nsc, _ := coverFlow(-d)
			if ndx != -dx || nsc != sc {
				t.Fatal("cover flow must be symmetric")
			}
		}
	}
}

func TestLooksLikeID(t *testing.T) {
	for _, id := range []string{"79ead9f36e7f4fc835a1b2c3d4e5f607", "79EAD9F3-6E7F-4FC8-35A1-B2C3D4E5F607", "0123456789abcdef.mp3"} {
		if !looksLikeID(id) {
			t.Errorf("%q should be an id", id)
		}
	}
	for _, title := range []string{"The Walls", "R U Mine?", "Swim", "Snap Out Of It", "Deadbeef Cafe Babe", "abcdef", "Bad Habits", "Faded", "DECAFFEINATED", "acdc"} {
		if looksLikeID(title) {
			t.Errorf("%q is a real title", title)
		}
	}
	c := defaultConfig()
	c.History = []HistItem{{Key: "a", Title: "The Walls"}, {Key: "b", Title: "79ead9f36e7f4fc835a1b2c3d4e5f607"}}
	c.validateWidgets()
	if len(c.History) != 1 || c.History[0].Title != "The Walls" {
		t.Fatalf("hash titles stay in the list: %+v", c.History)
	}
}
