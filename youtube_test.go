package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// a trimmed copy of the shape of YouTube's results page
func fakeResults(items ...[3]string) string {
	var b strings.Builder
	b.WriteString(`<html><script>var ytInitialData = {"contents":{"x":[`)
	for _, it := range items {
		fmt.Fprintf(&b, `{"videoRenderer":{"videoId":"%s","thumbnail":{"thumbnails":[]},"title":{"runs":[{"text":"%s"}],"accessibility":{}},"lengthText":{"accessibility":{"accessibilityData":{"label":"x minutes"}},"simpleText":"%s"},"viewCountText":{}}},`, it[0], it[1], it[2])
	}
	b.WriteString(`]}};</script></html>`)
	return b.String()
}

func TestParseYouTube(t *testing.T) {
	page := fakeResults(
		[3]string{"AAAAAAAAAA1", `Juice WRLD - Lucid Dreams (Official Music Video)`, "4:05"},
		[3]string{"AAAAAAAAAA2", `Lucid Dreams (Lyrics)`, "3:59"},
		[3]string{"AAAAAAAAAA1", `dup`, "1:00"},
		[3]string{"AAAAAAAAAA3", `Lucid Dreams \"cover\" by someone`, "3:50"},
	)
	c := parseYouTubeResults(page)
	if len(c) != 3 || c[0].ID != "AAAAAAAAAA1" || c[0].Dur != 245 || !strings.Contains(c[2].Title, `"cover"`) {
		t.Fatalf("%+v", c)
	}
	if clockSeconds("1:02:03") != 3723 || clockSeconds("x") != 0 {
		t.Fatal("clock")
	}
	if len(parseYouTubeResults("<html>nothing</html>")) != 0 {
		t.Fatal("empty page")
	}
}

func TestRankVideos(t *testing.T) {
	cands := []videoCand{
		{ID: "lyrics00001", Title: "Juice WRLD - Lucid Dreams (Lyrics)", Dur: 240},
		{ID: "cover000001", Title: "Lucid Dreams - Juice WRLD (piano cover)", Dur: 238},
		{ID: "official001", Title: "Juice WRLD - Lucid Dreams (Official Music Video)", Dur: 245},
		{ID: "live0000001", Title: "Juice WRLD - Lucid Dreams LIVE at Rolling Loud", Dur: 260},
		{ID: "hour0000001", Title: "Lucid Dreams 1 hour loop", Dur: 3600},
		{ID: "other000001", Title: "Some other song", Dur: 200},
	}
	r := rankVideos(cands, "Lucid Dreams", "Juice WRLD", 239)
	if len(r) == 0 || r[0].ID != "official001" {
		t.Fatalf("best %+v", r)
	}
	for _, c := range r {
		if c.ID == "cover000001" || c.ID == "live0000001" || c.ID == "hour0000001" || c.ID == "other000001" {
			t.Fatalf("%s should be dropped", c.Title)
		}
	}
	if !hasWord("song (live at x)", "live") || hasWord("alive (official video)", "live") || !hasWord("x 8d audio", "8d") {
		t.Fatal("whole words")
	}
	r = rankVideos([]videoCand{{ID: "alive000001", Title: "Alive (Official Video) - Band", Dur: 200}}, "Alive", "Band", 200)
	if len(r) != 1 {
		t.Fatal("'Alive' is not a live version")
	}
	// a song that is itself a live version / remix keeps those
	r = rankVideos([]videoCand{{ID: "remix000001", Title: "Song (Remix) Official Video", Dur: 200}}, "Song (Remix)", "X", 200)
	if len(r) != 1 {
		t.Fatal("remix song lost its remix video")
	}
}

func TestVideoSearchAndState(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("search_query")
		fmt.Fprint(w, fakeResults(
			[3]string{"official001", "Chase Atlantic - SWIM (Official Music Video)", "4:40"},
			[3]string{"lyrics00001", "Chase Atlantic - Swim (Lyrics)", "4:05"},
		))
	}))
	defer srv.Close()
	youtubeBase = srv.URL
	v := &videoState{offsets: map[string]float64{}}
	v.forSong(&Track{Key: "k1", Title: "Swim", Artist: "Chase Atlantic", Duration: 240})
	deadline := time.Now().Add(3 * time.Second)
	for {
		v.Lock()
		busy := v.Busy
		v.Unlock()
		if !busy || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	v.Lock()
	defer v.Unlock()
	if !strings.Contains(gotQ, "Swim") || !strings.Contains(gotQ, "official video") {
		t.Fatalf("query %q", gotQ)
	}
	if v.current() != "official001" {
		t.Fatalf("picked %q", v.current())
	}
	// a 40 s intro: the video starts into it
	if v.Offset < 20 || v.Offset > 40 {
		t.Fatalf("intro offset %.1f", v.Offset)
	}
	v.nudge(1.5)
	v.skip()
	if v.current() != "lyrics00001" && v.current() != "" {
		t.Fatalf("skip → %q", v.current())
	}
	v.skip()
	v.skip()
	if v.current() != "" {
		t.Fatal("past the end must be none")
	}
}

func TestVideoEndpoints(t *testing.T) {
	cfg = defaultConfig()
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.port)
	tok := "?t=" + srv.token
	// the page (only with the token)
	if r, _ := http.Get(base + "/video"); r.StatusCode != 403 {
		t.Fatal("video page without token")
	}
	r, err := http.Get(base + "/video" + tok)
	if err != nil || r.StatusCode != 200 {
		t.Fatal("video page")
	}
	// nothing wanted: no id
	media.SetTrack("Swim", "Chase Atlantic", "", "Spotify.exe")
	snap := media.Snapshot()
	video.Lock()
	video.Key, video.Cands, video.Pick, video.Want = snap.Track.Key, []videoCand{{ID: "aaaaaaaaaaa"}, {ID: "bbbbbbbbbbb"}}, 0, false
	video.Unlock()
	get := func() map[string]any {
		r, err := http.Get(base + "/api/video" + tok)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		return m
	}
	if get()["id"] != "" {
		t.Fatal("id while not wanted")
	}
	video.Lock()
	video.Want = true
	video.Unlock()
	if get()["id"] != "aaaaaaaaaaa" {
		t.Fatal("wanted video not handed out")
	}
	post := func(body string) {
		r, err := http.Post(base+"/api/video"+tok, "application/json", strings.NewReader(body))
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("post %s", body)
		}
	}
	post(`{"playing":true,"id":"aaaaaaaaaaa"}`)
	video.Lock()
	seen := !video.Seen.IsZero()
	video.Unlock()
	if !seen {
		t.Fatal("playing report ignored")
	}
	post(`{"error":150,"id":"zzzzzzzzzzz"}`) // an old id: ignored
	if get()["id"] != "aaaaaaaaaaa" {
		t.Fatal("stale error report skipped the video")
	}
	post(`{"error":150,"id":"aaaaaaaaaaa"}`)
	if get()["id"] != "bbbbbbbbbbb" {
		t.Fatal("error did not move to the next video")
	}
	if r, _ := http.Post(base+"/api/video"+tok, "application/json", strings.NewReader("{bad")); r.StatusCode != 400 {
		t.Fatal("bad body")
	}
}
