package main

import (
	"crypto/ed25519"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// Whatever Spotify, Apple or GitHub send back (or someone pretending to be them): never a crash,
// never an unsafe result.

func FuzzParseQueue(f *testing.F) {
	f.Add([]byte(fakeQueue))
	f.Add([]byte(`{"queue":[{"id":"x","name":"y","album":{"images":[{"url":"https://a","width":-5}]}}]}`))
	f.Add([]byte(`{"currently_playing":null,"queue":null}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, items := parseQueue(b)
		if len(items) > 10 {
			t.Fatal("more than 10")
		}
		for _, it := range items {
			if it.ID == "" || it.Title == "" {
				t.Fatal("empty item kept")
			}
		}
		_ = queueAfter(items, "a", "b", "c")
	})
}

func FuzzParseSearch(f *testing.F) {
	f.Add([]byte(`{"tracks":{"items":[{"name":"Swim","uri":"spotify:track:1","artists":[{"name":"A"}],"album":{"images":[{"url":"https://i/64","width":64}]}}]}}`))
	f.Add([]byte(`{"results":[{"trackName":"Swim","artistName":"A","artworkUrl100":"https://a/1.jpg"}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, h := range parseSpotifySearch(b) {
			if !strings.HasPrefix(h.URI, "spotify:") || h.Title == "" {
				t.Fatalf("unsafe spotify hit %+v", h)
			}
		}
		for _, h := range parseAppleSearch(b) {
			if h.Image != "" && !strings.HasPrefix(h.Image, "https://") || h.Title == "" || h.URI != "" {
				t.Fatalf("unsafe apple hit %+v", h)
			}
		}
		sh, rp, ok := parsePlayer(b)
		if ok && (sh < 0 || sh > 1 || rp < 0 || rp > 2) {
			t.Fatal("player out of range")
		}
		_ = spotifyErr(403, b)
	})
}

func FuzzLocalAndMerge(f *testing.F) {
	f.Add("sw", "Swim", "Chase Atlantic", "Çıkmaz", "Şebnem")
	f.Add("", "", "", "\xff", "\x00")
	f.Fuzz(func(t *testing.T, q, t1, a1, t2, a2 string) {
		hist := []HistItem{{Key: keyOf(t1, a1), Title: t1, Artist: a1}, {Key: keyOf(t2, a2), Title: t2, Artist: a2}}
		loc := localMatches(q, hist, 4)
		on := []searchHit{{Title: t2, Artist: a2, URI: "spotify:track:1"}, {Title: q, Artist: t1}}
		got := mergeHits(loc, on, 6)
		if len(got) > 6 {
			t.Fatal("limit")
		}
		seen := map[string]bool{}
		for _, h := range got {
			if seen[h.Key] {
				t.Fatal("duplicate")
			}
			seen[h.Key] = true
		}
	})
}

func FuzzManifest(f *testing.F) {
	f.Add([]byte(`{"version":"1.19.0","url":"https://github.com/Dragnar1250/orbit/releases/download/v1.19.0/Orbit.exe","sha256":"` + strings.Repeat("a", 64) + `","size":100,"sig":"AAAA"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var m updateManifest
		if json.Unmarshal(b, &m) != nil {
			return
		}
		if m.verify("1.0.0", updateKey) == nil {
			t.Fatalf("an unsigned/forged manifest passed: %s", b)
		}
		// with any random key too
		pub := ed25519.PublicKey(make([]byte, ed25519.PublicKeySize))
		if m.verify("1.0.0", pub) == nil {
			t.Fatal("zero key accepted")
		}
	})
}

func FuzzVersions(f *testing.F) {
	f.Add("1.18.0", "1.19.0")
	f.Add("v1", "x.y.z.w.q")
	f.Fuzz(func(t *testing.T, a, b string) {
		if versionLess(a, b) && versionLess(b, a) {
			t.Fatalf("both less: %q %q", a, b)
		}
		if versionLess(a, a) {
			t.Fatal("less than itself")
		}
		_ = validVersion(a)
	})
}

func FuzzSecrets(f *testing.F) {
	f.Add("refresh-token")
	f.Add("dpapi:AAAA")
	f.Add("dpapi:")
	f.Fuzz(func(t *testing.T, s string) {
		_ = openSecret(s) // whatever is in config.json
		if s == "" {
			return
		}
		sealed := sealSecret(s)
		if openSecret(sealed) != s {
			t.Fatalf("round trip lost %q", s)
		}
		if runtime.GOOS != "linux" && len(s) > 6 && strings.Contains(sealed, s) { // (Linux: a reversible stand-in, not encryption)
			t.Fatal("sealed holds it in the clear")
		}
	})
}

func FuzzSearchTyping(f *testing.F) {
	f.Add("swim", []byte{8, 8, 'a', 0x7f, 13})
	f.Fuzz(func(t *testing.T, start string, keys []byte) {
		s := start
		if len(s) > 120 {
			s = s[:120]
		}
		for _, k := range keys {
			s = editText(s, rune(k), 120)
			if utf8.RuneCountInString(s) > 120 || !utf8.ValidString(s) && utf8.ValidString(start) {
				t.Fatalf("bad text %q", s)
			}
		}
	})
}
