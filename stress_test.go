package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Everything the full screen, the media thread, the settings page and Spotify's answers do at
// once, hammered from many goroutines (run with -race).
func TestStressEverythingAtOnce(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
		switch r.URL.Path {
		case "/v1/me/player/queue":
			fmt.Fprint(w, fakeQueue)
		case "/v1/me/player":
			fmt.Fprint(w, `{"shuffle_state":false,"repeat_state":"off"}`)
		case "/v1/search":
			fmt.Fprint(w, `{"tracks":{"items":[{"name":"Swim","uri":"spotify:track:1","artists":[{"name":"A"}],"album":{"images":[]}}]}}`)
		default:
			w.WriteHeader([]int{204, 204, 404, 403, 429, 500}[rand.Intn(6)])
		}
	}))
	defer fake.Close()
	spotifyAPI = fake.URL // (left pointing here: background calls may still be finishing)
	withCfg(func() {
		cfg = defaultConfig()
		cfg.SpotifyClientID, cfg.SpotifyRefresh, cfg.SpotifyScopes = "cid", "R", "user-modify-playback-state"
	})
	s := &spotifyState{shuffle: -1, repeat: -1, access: "T", expires: time.Now().Add(time.Hour)}
	spotify = s
	m := &Media{}
	se := &searcher{delay: time.Millisecond, find: onlineSearch}

	var wg sync.WaitGroup
	stop := time.Now().Add(3 * time.Second)
	run := func(f func(r *rand.Rand)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			for time.Now().Before(stop) {
				f(r)
			}
		}()
	}
	run(func(r *rand.Rand) { // the media thread
		m.SetTrack([]string{"Swim", "Sweater Weather", "A Little Death"}[r.Intn(3)], "X", "", "smtc")
		m.SetDuration(keyOf("Swim", "X"), 200)
		m.SetPosition(r.Float64()*200, r.Intn(2) == 0)
		m.SetModes(Modes{Shuffle: r.Intn(3) - 1, Repeat: r.Intn(4) - 1, CanShuffle: r.Intn(2) == 0})
	})
	run(func(r *rand.Rand) { // the user clicking the bar
		switch r.Intn(4) {
		case 0:
			m.Seek(r.Float64() * 300)
		case 1:
			m.WantModes(r.Intn(2), -1)
		case 2:
			s.do([]string{"shuffle", "repeat", "play", "queue", "bogus"}[r.Intn(5)], "spotify:track:1")
		case 3:
			se.ask(string(rune('a'+r.Intn(26))) + string(rune('a'+r.Intn(26))))
		}
		time.Sleep(time.Millisecond)
	})
	run(func(r *rand.Rand) { // drawing frames
		snap := m.Snapshot()
		_, _ = s.modes()
		q, playing := s.snapshot()
		_ = queueAfter(q, playing, "Swim", keyOf("Swim", "X"))
		_, hits, _ := se.results()
		_ = mergeHits(localMatches("sw", []HistItem{{Key: "k", Title: "Swim"}}, 4), hits, 6)
		_ = s.takeNotes()
		_ = snap
	})
	run(func(r *rand.Rand) { // background refreshes
		s.refreshQueue()
		s.readPlayer()
		time.Sleep(2 * time.Millisecond)
	})
	run(func(r *rand.Rand) { // the settings page
		withCfg(func() {
			_ = cfg.setField("SkinGlass", []byte(fmt.Sprint(r.Float64())))
		})
		_ = s.canControl()
		_ = updater.status()
		if r.Intn(50) == 0 {
			se.reset()
		}
	})
	wg.Wait()
	time.Sleep(1500 * time.Millisecond) // let the last background calls finish
	// Only "Swim" gets a duration (200 s). The others can be left as the last track with no
	// duration, where a seek of up to 300 s is kept as is and still runs on for a few seconds.
	snap := m.Snapshot()
	limit := 300 + 5.0
	if snap.Track != nil && snap.Track.Duration > 0 {
		limit = snap.Track.Duration + 0.5
	}
	if p := snap.Pos; p < 0 || p > limit {
		t.Fatalf("position out of range %v (limit %v)", p, limit)
	}
}
