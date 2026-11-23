package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeQueue = `{"currently_playing":{"id":"cur","name":"A Little Death","artists":[{"name":"The Neighbourhood"}],"album":{"images":[]}},
"queue":[{"id":"t1","name":"Sweater Weather","duration_ms":240000,"artists":[{"name":"The Neighbourhood"}],"album":{"images":[{"url":"https://i.scdn.co/640","width":640},{"url":"https://i.scdn.co/300","width":300},{"url":"https://i.scdn.co/64","width":64}]}},
{"id":"t2","name":"Swim","duration_ms":200000,"artists":[{"name":"Chase Atlantic"},{"name":"X"}],"album":{"images":[{"url":"https://i.scdn.co/a","width":300}]}},
{"id":"","name":"broken"},
{"id":"e1","name":"Episode 4","show":{"name":"A Podcast","images":[{"url":"https://i.scdn.co/p","width":300}]}}]}`

func TestParseQueue(t *testing.T) {
	playing, items := parseQueue([]byte(fakeQueue))
	if playing != "A Little Death" || len(items) != 3 {
		t.Fatalf("%q %+v", playing, items)
	}
	if items[0].Image != "https://i.scdn.co/300" || items[0].Dur != 240 || items[1].Artist != "Chase Atlantic, X" {
		t.Fatalf("%+v", items)
	}
	if items[2].Artist != "A Podcast" || items[2].Image != "https://i.scdn.co/p" {
		t.Fatalf("episode %+v", items[2])
	}
	if _, it := parseQueue([]byte("nonsense")); it != nil {
		t.Fatal("bad json")
	}
}

func TestSpotifyFlow(t *testing.T) {
	var verifier string
	var refreshes, queues int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token":
			r.ParseForm()
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				verifier = r.Form.Get("code_verifier")
				if r.Form.Get("code") != "good" || r.Form.Get("client_id") != "cid" || !strings.HasSuffix(r.Form.Get("redirect_uri"), "/spotify/callback") {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid_grant","error_description":"bad code"}`)
					return
				}
				fmt.Fprint(w, `{"access_token":"A1","refresh_token":"R1","expires_in":3600}`)
			case "refresh_token":
				atomic.AddInt32(&refreshes, 1)
				if r.Form.Get("refresh_token") != "R1" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid_grant"}`)
					return
				}
				fmt.Fprint(w, `{"access_token":"A2","refresh_token":"R2","expires_in":3600}`)
			}
		case "/v1/me/player/queue":
			atomic.AddInt32(&queues, 1)
			if a := r.Header.Get("Authorization"); a != "Bearer A1" && a != "Bearer A2" {
				w.WriteHeader(401)
				return
			}
			io.WriteString(w, fakeQueue)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	spotifyAccounts, spotifyAPI = fake.URL, fake.URL
	s := &spotifyState{}

	// the sign-in link: PKCE challenge of the verifier, a state, our redirect, read-only scopes
	u, _ := url.Parse(s.loginURL("cid", 47330))
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("redirect_uri") != "http://127.0.0.1:47330/spotify/callback" || q.Get("code_challenge_method") != "S256" ||
		q.Get("state") == "" || strings.ContainsAny(q.Get("scope"), ",") || strings.Contains(q.Get("scope"), "library") || strings.Contains(q.Get("scope"), "playlist") || strings.Contains(q.Get("scope"), "email") || strings.Contains(q.Get("scope"), "private") {
		t.Fatalf("login url %v", u)
	}
	state, challenge := q.Get("state"), q.Get("code_challenge")
	// a link from elsewhere (wrong state) is refused and doesn't use up the real one
	if _, _, err := s.finishLogin("cid", "good", "evil", 47330); err == nil {
		t.Fatal("wrong state accepted")
	}
	u, _ = url.Parse(s.loginURL("cid", 47330))
	state, challenge = u.Query().Get("state"), u.Query().Get("code_challenge")
	ref, _, err := s.finishLogin("cid", "good", state, 47330)
	if err != nil || ref != "R1" {
		t.Fatalf("login: %v %q", err, ref)
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Fatal("PKCE verifier doesn't match the challenge")
	}
	if _, _, err := s.finishLogin("cid", "good", state, 47330); err == nil {
		t.Fatal("a sign-in link must work only once")
	}
	// the queue with the fresh token
	items, playing, nr, err := s.fetchQueue("cid", "R1")
	if err != nil || len(items) != 3 || playing != "A Little Death" || nr != "" {
		t.Fatalf("queue %v %d %q", err, len(items), nr)
	}
	// token expired → refreshed (and Spotify's new refresh token handed back)
	s.access = ""
	items, _, nr, err = s.fetchQueue("cid", "R1")
	if err != nil || nr != "R2" || atomic.LoadInt32(&refreshes) != 1 || len(items) != 3 {
		t.Fatalf("refresh %v %q", err, nr)
	}
	// rejected token → error, and the next call refreshes again
	s.access = "stale"
	if _, _, _, err = s.fetchQueue("cid", "R1"); err == nil {
		t.Fatal("401 must be an error")
	}
	if s.access != "" {
		t.Fatal("a refused token must be dropped")
	}
	// Spotify's Premium rule and "nothing playing"
	status := 403
	fake2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer fake2.Close()
	spotifyAPI = fake2.URL
	s.access, s.expires = "A1", time.Now().Add(time.Hour)
	if _, _, _, err := s.fetchQueue("cid", "R1"); err == nil || !strings.Contains(err.Error(), "Premium") {
		t.Fatalf("403: %v", err)
	}
	status = 204
	if items, _, _, err := s.fetchQueue("cid", "R1"); err != nil || items != nil {
		t.Fatalf("204: %v %v", items, err)
	}
	// not connected
	s2 := &spotifyState{}
	if _, _, _, err := s2.fetchQueue("", ""); err == nil {
		t.Fatal("no client id must fail")
	}
}

func TestSpotifyEndpoints(t *testing.T) {
	cfg = defaultConfig()
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.port)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// starting a sign-in needs the settings token and a Client ID
	if r, _ := noRedirect.Get(base + "/spotify/login"); r.StatusCode != 403 {
		t.Fatal("login without token")
	}
	if r, _ := noRedirect.Get(base + "/spotify/login?t=" + srv.token); r.StatusCode != 400 {
		t.Fatal("login without client id")
	}
	cfg.SpotifyClientID = "cid"
	r, _ := noRedirect.Get(base + "/spotify/login?t=" + srv.token)
	if r.StatusCode != 302 || !strings.Contains(r.Header.Get("Location"), "/authorize?") {
		t.Fatalf("login redirect %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	// Spotify's return needs no token, but a forged one is refused politely
	r, _ = http.Get(base + "/spotify/callback?code=x&state=forged")
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(b), "Couldn't connect") || cfg.SpotifyRefresh != "" {
		t.Fatalf("forged callback: %d %s", r.StatusCode, b)
	}
	r, _ = http.Get(base + "/spotify/callback?error=access_denied")
	b, _ = io.ReadAll(r.Body)
	if !strings.Contains(string(b), "cancelled") {
		t.Fatal("cancel page")
	}
	// HTML in what Spotify sends back is shown as text
	r, _ = http.Get(base + "/spotify/callback?error=" + url.QueryEscape("<script>x</script>"))
	b, _ = io.ReadAll(r.Body)
	if strings.Contains(string(b), "<script>x") {
		t.Fatal("not escaped")
	}
	// a bad client id never reaches the link
	c := defaultConfig()
	c.SpotifyClientID = "abc&redirect_uri=evil"
	c.validateFields()
	if c.SpotifyClientID != "" {
		t.Fatal("client id with link characters kept")
	}
}

func TestQueueAfter(t *testing.T) {
	_, q := parseQueue([]byte(fakeQueue)) // Sweater Weather, Swim, Episode 4
	if got := queueAfter(q, "A Little Death", "A Little Death", keyOf("A Little Death", "The Neighbourhood")); len(got) != 3 {
		t.Fatal("same song: whole queue")
	}
	got := queueAfter(q, "A Little Death", "Sweater Weather", keyOf("Sweater Weather", "The Neighbourhood"))
	if len(got) != 2 || got[0].Title != "Swim" {
		t.Fatalf("moved on one: %+v", got)
	}
	if got := queueAfter(q, "A Little Death", "Something Else", keyOf("Something Else", "X")); got != nil {
		t.Fatal("unknown song: the queue no longer fits")
	}
	if got := queueAfter(q, "A Little Death", "Episode 4", keyOf("Episode 4", "A Podcast")); len(got) != 0 {
		t.Fatal("last one: nothing after")
	}
	if queueAfter(nil, "", "x", "x") != nil {
		t.Fatal("empty")
	}
}
