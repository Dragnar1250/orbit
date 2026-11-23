package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalMatches(t *testing.T) {
	hist := []HistItem{
		{Key: keyOf("Swim", "Chase Atlantic"), Title: "Swim", Artist: "Chase Atlantic"},
		{Key: keyOf("Sweater Weather", "The Neighbourhood"), Title: "Sweater Weather", Artist: "The Neighbourhood"},
		{Key: keyOf("Swim", "Chase Atlantic"), Title: "Swim", Artist: "Chase Atlantic"}, // twice
		{Key: keyOf("Çıkmaz Sokak", "Şebnem"), Title: "Çıkmaz Sokak", Artist: "Şebnem"},
		{Key: "x", Title: "", Artist: "Nobody"},
	}
	if got := localMatches("sw", hist, 5); len(got) != 2 || got[0].From != "played" {
		t.Fatalf("sw: %+v", got)
	}
	if got := localMatches("chase swim", hist, 5); len(got) != 1 {
		t.Fatalf("every word, any order: %+v", got)
	}
	if got := localMatches("SWEATER neighbour", hist, 5); len(got) != 1 {
		t.Fatal("case")
	}
	if got := localMatches("çıkmaz", hist, 5); len(got) != 1 {
		t.Fatalf("Turkish letters: %+v", got)
	}
	if localMatches("   ", hist, 5) != nil || len(localMatches("s", hist, 1)) != 1 || localMatches("nobody", hist, 5) != nil {
		t.Fatal("empty / limit / untitled")
	}
}

func TestMergeHits(t *testing.T) {
	local := []searchHit{{Title: "Swim", Artist: "Chase Atlantic", Key: keyOf("Swim", "Chase Atlantic"), From: "played", File: "c.jpg"},
		{Title: "Old", Artist: "A", Key: keyOf("Old", "A"), From: "played"},
		{Title: "Older", Artist: "B", Key: keyOf("Older", "B"), From: "played"},
		{Title: "Oldest", Artist: "C", Key: keyOf("Oldest", "C"), From: "played"}}
	online := []searchHit{{Title: "Swimming", Artist: "X", URI: "spotify:track:1", From: "spotify"},
		{Title: "Swim", Artist: "Chase Atlantic", URI: "spotify:track:2", From: "spotify"}}
	got := mergeHits(local, online, 6)
	if len(got) != 5 { // 4 played + "Swimming" (Swim is both)
		t.Fatalf("%d", len(got))
	}
	if got[0].Title != "Swim" || got[0].URI != "spotify:track:2" || got[0].From != "played" || got[0].File != "c.jpg" {
		t.Fatalf("a played song Spotify found keeps its uri and cover: %+v", got[0])
	}
	keys := map[string]bool{}
	for _, h := range got {
		if keys[h.Key] {
			t.Fatal("a song twice")
		}
		keys[h.Key] = true
	}
	if mergeHits(nil, nil, 6) != nil || len(mergeHits(local, online, 2)) != 2 {
		t.Fatal("empty / limit")
	}
}

func TestParseSearches(t *testing.T) {
	sp := parseSpotifySearch([]byte(`{"tracks":{"items":[{"name":"Swim","uri":"spotify:track:1","artists":[{"name":"Chase Atlantic"}],"album":{"name":"CA","images":[{"url":"https://i/640","width":640},{"url":"https://i/64","width":64},{"url":"https://i/300","width":300}]}},{"name":"","uri":"spotify:track:2"},{"name":"bad","uri":"javascript:x"}]}}`))
	if len(sp) != 1 || sp[0].Image != "https://i/64" || sp[0].URI != "spotify:track:1" || sp[0].Key == "" {
		t.Fatalf("spotify %+v", sp)
	}
	ap := parseAppleSearch([]byte(`{"results":[{"trackName":"Swim","artistName":"Chase Atlantic","collectionName":"CA","artworkUrl100":"https://a/100.jpg"},{"trackName":"Y","artworkUrl100":"http://insecure"},{"artistName":"no title"}]}`))
	if len(ap) != 2 || ap[0].Image == "" || ap[1].Image != "" || ap[0].URI != "" {
		t.Fatalf("apple %+v", ap)
	}
	if parseSpotifySearch([]byte("{")) != nil || parseAppleSearch([]byte("x")) != nil {
		t.Fatal("bad json")
	}
	if s, r, ok := parsePlayer([]byte(`{"shuffle_state":true,"repeat_state":"track"}`)); !ok || s != 1 || r != 1 {
		t.Fatal("player")
	}
	if _, _, ok := parsePlayer([]byte(`{}`)); ok {
		t.Fatal("empty player")
	}
}

// typing fast: only the newest text is asked, an old answer never replaces a newer one
func TestSearcherDebounce(t *testing.T) {
	var mu sync.Mutex
	asked := []string{}
	s := &searcher{delay: 40 * time.Millisecond, find: func(q string, n int) []searchHit {
		mu.Lock()
		asked = append(asked, q)
		mu.Unlock()
		if q == "sw" {
			time.Sleep(80 * time.Millisecond) // slow answer
		}
		return []searchHit{{Title: q}}
	}}
	for _, q := range []string{"s", "sw", "swi", "swim"} {
		s.ask(q)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	q, hits, _ := s.results()
	mu.Lock()
	n := len(asked)
	mu.Unlock()
	if q != "swim" || len(hits) != 1 || hits[0].Title != "swim" || n != 1 {
		t.Fatalf("q=%q hits=%v asked=%d", q, hits, n)
	}
	// a slow old answer arriving after newer typing is dropped
	s.ask("sw")
	time.Sleep(60 * time.Millisecond) // asked, answer pending
	s.ask("swimming")
	time.Sleep(200 * time.Millisecond)
	if q, _, _ := s.results(); q != "swimming" {
		t.Fatalf("old answer won: %q", q)
	}
	s.reset()
	if q, h, _ := s.results(); q != "" || h != nil {
		t.Fatal("reset")
	}
	s.ask("x") // one letter: not asked
	time.Sleep(80 * time.Millisecond)
	if q, _, _ := s.results(); q != "" {
		t.Fatal("one letter asked")
	}
}

func TestSpotifyControl(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var status int32 = 204
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer T" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/v1/search":
			fmt.Fprint(w, `{"tracks":{"items":[{"name":"Swim","uri":"spotify:track:1","artists":[{"name":"Chase Atlantic"}],"album":{"images":[]}}]}}`)
		case r.URL.Path == "/v1/me/player" && r.Method == "GET":
			fmt.Fprint(w, `{"shuffle_state":true,"repeat_state":"context"}`)
		case r.URL.Path == "/v1/me/player/queue" && r.Method == "GET":
			fmt.Fprint(w, fakeQueue)
		default:
			s := atomic.LoadInt32(&status)
			w.WriteHeader(int(s))
			if s == 404 {
				fmt.Fprint(w, `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`)
			}
			if s == 403 {
				fmt.Fprint(w, `{"error":{"status":403,"message":"Player command failed: Premium required","reason":"PREMIUM_REQUIRED"}}`)
			}
		}
	}))
	defer fake.Close()
	spotifyAPI = fake.URL
	cfg = defaultConfig()
	cfg.SpotifyClientID, cfg.SpotifyRefresh, cfg.SpotifyScopes = "cid", "R", "user-read-playback-state"
	s := &spotifyState{shuffle: -1, repeat: -1, access: "T", expires: time.Now().Add(time.Hour)}
	if s.canControl() {
		t.Fatal("a read-only sign-in can't control")
	}
	cfg.SpotifyScopes += " user-modify-playback-state"
	if !s.canControl() {
		t.Fatal("control scope")
	}
	wait := func(n int) []string {
		for i := 0; i < 100; i++ {
			mu.Lock()
			c := append([]string(nil), calls...)
			mu.Unlock()
			if len(c) >= n {
				time.Sleep(20 * time.Millisecond)
				mu.Lock()
				c = append([]string(nil), calls...)
				calls = nil
				mu.Unlock()
				return c
			}
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
	// play = add to the queue, then skip to it (the album/playlist goes on afterwards)
	s.do("play", "spotify:track:1")
	c := wait(2)
	if len(c) < 2 || !strings.HasPrefix(c[0], "POST /v1/me/player/queue?uri=spotify%3Atrack%3A1") || c[1] != "POST /v1/me/player/next?" {
		t.Fatalf("play: %v", c)
	}
	s.do("queue", "spotify:track:9")
	c = wait(1)
	if len(c) != 1 || !strings.Contains(c[0], "track%3A9") {
		t.Fatalf("queue: %v", c)
	}
	time.Sleep(50 * time.Millisecond)
	if n := s.takeNotes(); len(n) != 1 || n[0] != "Added to the queue" {
		t.Fatalf("notes %v", n)
	}
	// shuffle / repeat: shown at once, sent to Spotify
	s.do("shuffle", "true")
	if sh, _ := s.modes(); sh != 1 {
		t.Fatal("shuffle not shown at once")
	}
	c = wait(1)
	if len(c) != 1 || c[0] != "PUT /v1/me/player/shuffle?state=true" {
		t.Fatalf("shuffle %v", c)
	}
	s.do("repeat", "track")
	if _, rp := s.modes(); rp != 1 {
		t.Fatal("repeat")
	}
	if c = wait(1); len(c) != 1 || c[0] != "PUT /v1/me/player/repeat?state=track" {
		t.Fatalf("repeat %v", c)
	}
	// Spotify's answer right after a change doesn't undo it; later it is followed
	s.readPlayer()
	wait(1)
	if _, rp := s.modes(); rp != 1 {
		t.Fatal("old answer undid the change")
	}
	s.modesAt = time.Now().Add(-5 * time.Second)
	s.readPlayer()
	wait(1)
	if sh, rp := s.modes(); sh != 1 || rp != 2 {
		t.Fatalf("player state %d %d", sh, rp)
	}
	// errors become plain words
	for code, want := range map[int32]string{404: "isn't playing", 403: "Premium", 429: "too many"} {
		atomic.StoreInt32(&status, code)
		s.do("queue", "spotify:track:1")
		wait(1)
		time.Sleep(50 * time.Millisecond)
		n := s.takeNotes()
		if len(n) != 1 || !strings.Contains(n[0], want) {
			t.Fatalf("%d: %v", code, n)
		}
	}
	atomic.StoreInt32(&status, 204)
	s.do("nonsense", "x")
	time.Sleep(50 * time.Millisecond)
	if c := wait(0); len(c) != 0 {
		t.Fatalf("unknown action sent %v", c)
	}
	// search goes to Spotify when connected
	old := spotify
	spotify = s
	defer func() { spotify = old }()
	hits := onlineSearch("swim", 6)
	if len(hits) != 1 || hits[0].URI != "spotify:track:1" {
		t.Fatalf("search %+v", hits)
	}
	if c := wait(1); !strings.Contains(c[0], "/v1/search?") || !strings.Contains(c[0], "type=track") || !strings.Contains(c[0], "q=swim") {
		t.Fatalf("search call %v", c)
	}
	if onlineSearch("   ", 6) != nil {
		t.Fatal("blank search")
	}
}

// not connected: Apple's free song search
func TestAppleSearch(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("term") != "swim chase" || r.URL.Query().Get("entity") != "song" {
			w.WriteHeader(400)
			return
		}
		fmt.Fprint(w, `{"results":[{"trackName":"Swim","artistName":"Chase Atlantic","artworkUrl100":"https://a/100.jpg"}]}`)
	}))
	defer fake.Close()
	old := itunesBase
	itunesBase = fake.URL
	defer func() { itunesBase = old }()
	cfg = defaultConfig()
	hits := onlineSearch("swim chase", 6)
	if len(hits) != 1 || hits[0].From != "apple" || hits[0].URI != "" {
		t.Fatalf("%+v", hits)
	}
}

func TestModesHold(t *testing.T) {
	m := &Media{}
	m.SetModes(Modes{Shuffle: 0, Repeat: 0, CanShuffle: true, CanRepeat: true})
	m.WantModes(1, -1)
	m.SetModes(Modes{Shuffle: 0, Repeat: 0, CanShuffle: true, CanRepeat: true}) // the player's old answer
	if md := m.Snapshot().Modes; md.Shuffle != 1 || md.Repeat != 0 || !md.CanShuffle {
		t.Fatalf("%+v", md)
	}
	m.modesHold = time.Now().Add(-time.Millisecond)
	m.SetModes(Modes{Shuffle: 0, Repeat: 2})
	if md := m.Snapshot().Modes; md.Shuffle != 0 || md.Repeat != 2 {
		t.Fatalf("after the hold %+v", md)
	}
}
