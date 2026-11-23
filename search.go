package main

// In-app search: suggestions while typing. Songs you played (instant), then Spotify's own search
// when connected (those can be played right here), else Apple's free song search.

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type searchHit struct {
	Title, Artist, Album string
	Image                string // picture address (https)
	File                 string // the picture, once downloaded
	URI                  string // spotify:track:… (playable from here)
	From                 string // "played", "spotify", "apple"
	Key                  string
}

// localMatches: songs Orbit already knows whose title or artist contain every word typed.
func localMatches(q string, items []HistItem, limit int) []searchHit {
	words := strings.Fields(normStr(q))
	if len(words) == 0 {
		return nil
	}
	var out []searchHit
	seen := map[string]bool{}
	for _, h := range items {
		hay := normStr(h.Title + " " + h.Artist)
		all := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				all = false
				break
			}
		}
		if !all || seen[h.Key] || h.Title == "" {
			continue
		}
		seen[h.Key] = true
		out = append(out, searchHit{Title: h.Title, Artist: h.Artist, File: h.CoverFile, From: "played", Key: h.Key})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// mergeHits: online results first when they can be played here, the played ones folded in —
// each song once, at most limit.
func mergeHits(local, online []searchHit, limit int) []searchHit {
	var out []searchHit
	seen := map[string]bool{}
	add := func(h searchHit) {
		if h.Key == "" {
			h.Key = keyOf(h.Title, h.Artist)
		}
		if seen[h.Key] || len(out) >= limit {
			return
		}
		seen[h.Key] = true
		out = append(out, h)
	}
	// a played song that Spotify also found: keep Spotify's (it can be played)
	byKey := map[string]searchHit{}
	for _, h := range online {
		byKey[keyOf(h.Title, h.Artist)] = h
	}
	for _, h := range local {
		if o, ok := byKey[h.Key]; ok && o.URI != "" {
			o.From = "played"
			if o.File == "" {
				o.File = h.File
			}
			add(o)
		} else if len(out) < 2 { // at most two old ones ahead of the new results
			add(h)
		}
	}
	for _, h := range online {
		add(h)
	}
	for _, h := range local {
		add(h)
	}
	return out
}

func parseSpotifySearch(b []byte) []searchHit {
	var r struct {
		Tracks struct {
			Items []struct {
				Name    string `json:"name"`
				URI     string `json:"uri"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					Images []struct {
						URL   string `json:"url"`
						Width int    `json:"width"`
					} `json:"images"`
				} `json:"album"`
			} `json:"items"`
		} `json:"tracks"`
	}
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	var out []searchHit
	for _, t := range r.Tracks.Items {
		if t.Name == "" || !strings.HasPrefix(t.URI, "spotify:") {
			continue
		}
		var names []string
		for _, a := range t.Artists {
			names = append(names, a.Name)
		}
		h := searchHit{Title: t.Name, Artist: strings.Join(names, ", "), Album: t.Album.Name, URI: t.URI, From: "spotify"}
		bw := 1 << 30
		for _, im := range t.Album.Images { // the smallest one of at least 64 px
			if im.Width >= 64 && im.Width < bw {
				h.Image, bw = im.URL, im.Width
			}
		}
		h.Key = keyOf(h.Title, h.Artist)
		out = append(out, h)
	}
	return out
}

func parseAppleSearch(b []byte) []searchHit {
	var r struct {
		Results []struct {
			Track  string `json:"trackName"`
			Artist string `json:"artistName"`
			Album  string `json:"collectionName"`
			Art    string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	var out []searchHit
	for _, t := range r.Results {
		if t.Track == "" {
			continue
		}
		h := searchHit{Title: t.Track, Artist: t.Artist, Album: t.Album, From: "apple"}
		if strings.HasPrefix(t.Art, "https://") {
			h.Image = t.Art
		}
		h.Key = keyOf(h.Title, h.Artist)
		out = append(out, h)
	}
	return out
}

// onlineSearch: Spotify when connected, else Apple. limit results.
func onlineSearch(q string, limit int) []searchHit {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	if spotifyConnected() {
		code, b, err := spotify.call("GET", "/v1/search?type=track&limit="+itoa(limit)+"&q="+url.QueryEscape(q), nil)
		if err == nil && code == 200 {
			if h := parseSpotifySearch(b); len(h) > 0 {
				return h
			}
		}
	}
	var raw json.RawMessage
	if code, err := httpJSON(itunesBase+"/search?entity=song&limit="+itoa(limit)+"&term="+url.QueryEscape(q), &raw); err == nil && code == 200 {
		return parseAppleSearch(raw)
	}
	return nil
}

// searcher: asks once typing pauses, keeps only the answer to the newest text.
type searcher struct {
	mu    sync.Mutex
	want  string // the newest text
	q     string // the text the results belong to
	hits  []searchHit
	ver   int
	delay time.Duration
	find  func(string, int) []searchHit // (tests swap it)
}

var finder = &searcher{delay: 280 * time.Millisecond, find: onlineSearch}

func (s *searcher) ask(q string) {
	q = strings.TrimSpace(q)
	s.mu.Lock()
	if q == s.want {
		s.mu.Unlock()
		return
	}
	s.want = q
	s.mu.Unlock()
	if len([]rune(q)) < 2 {
		return
	}
	go func() {
		defer func() { recover() }()
		time.Sleep(s.delay)
		s.mu.Lock()
		still := s.want == q
		s.mu.Unlock()
		if !still { // more was typed meanwhile
			return
		}
		hits := s.find(q, 6)
		for i := range hits {
			if hits[i].Image != "" && i < 6 {
				hits[i].File = queueCover(queueItem{Image: hits[i].Image})
			}
		}
		s.mu.Lock()
		if s.want == q {
			s.q, s.hits = q, hits
			s.ver++
		}
		s.mu.Unlock()
	}()
}

// results: the newest answer and the text it is for.
func (s *searcher) results() (string, []searchHit, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.q, s.hits, s.ver
}

func (s *searcher) reset() {
	s.mu.Lock()
	s.want, s.q, s.hits = "", "", nil
	s.ver++
	s.mu.Unlock()
}

func itoa(n int) string { return strconv.Itoa(n) }
