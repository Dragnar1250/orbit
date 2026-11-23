package main

// Music videos for full screen: find the song's official video on YouTube (from the public
// search page — no account, no key), pick the best match, and keep its id per song.

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var youtubeBase = "https://www.youtube.com" // tests point this at a fake server

type videoCand struct {
	ID    string
	Title string
	Dur   float64 // seconds (0 = unknown)
	Score float64
}

var (
	reVideoRenderer = regexp.MustCompile(`"videoRenderer":\{"videoId":"([A-Za-z0-9_-]{11})"`)
	reTitleRuns     = regexp.MustCompile(`"title":\{"runs":\[\{"text":"((?:[^"\\]|\\.)*)"`)
	reLength        = regexp.MustCompile(`"lengthText":\{"accessibility":\{"accessibilityData":\{"label":"[^"]*"\}\},"simpleText":"([0-9:]+)"`)
	reLength2       = regexp.MustCompile(`"lengthText":\{[^{}]*"simpleText":"([0-9:]+)"`)
)

// parseYouTubeResults pulls video ids, titles and lengths out of a search results page.
func parseYouTubeResults(page string) []videoCand {
	locs := reVideoRenderer.FindAllStringSubmatchIndex(page, -1)
	var out []videoCand
	seen := map[string]bool{}
	for i, l := range locs {
		id := page[l[2]:l[3]]
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if end-l[0] > 20000 {
			end = l[0] + 20000
		}
		block := page[l[0]:end]
		if seen[id] {
			continue
		}
		seen[id] = true
		c := videoCand{ID: id}
		if m := reTitleRuns.FindStringSubmatch(block); m != nil {
			c.Title = unescapeJSON(m[1])
		}
		if m := reLength.FindStringSubmatch(block); m != nil {
			c.Dur = clockSeconds(m[1])
		} else if m := reLength2.FindStringSubmatch(block); m != nil {
			c.Dur = clockSeconds(m[1])
		}
		out = append(out, c)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func unescapeJSON(s string) string {
	if v, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return v
	}
	return s
}

// clockSeconds: "3:45" → 225, "1:02:03" → 3723.
func clockSeconds(s string) float64 {
	t := 0.0
	for _, p := range strings.Split(s, ":") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0
		}
		t = t*60 + float64(n)
	}
	return t
}

// rankVideos orders candidates: the official video of this song first; covers, live versions,
// lyric / audio-only uploads, reactions and very different lengths last.
func rankVideos(cands []videoCand, title, artist string, dur float64) []videoCand {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	tt := norm(cleanTitle(title))
	ar := norm(firstArtist(artist))
	for i := range cands {
		c := &cands[i]
		v := norm(c.Title)
		s := 0.0
		if tt != "" && strings.Contains(v, tt) {
			s += 4
		}
		if ar != "" && strings.Contains(v, ar) {
			s += 3
		}
		for _, w := range []string{"official video", "official music video", "(official", "music video"} {
			if strings.Contains(v, w) {
				s += 3
				break
			}
		}
		for _, w := range []string{"cover", "live", "reaction", "karaoke", "instrumental", "8d", "slowed", "sped up", "nightcore", "remix", "tutorial", "lesson", "reverb", "1 hour", "loop"} {
			if hasWord(v, w) && !hasWord(tt, w) {
				s -= 9 // not this recording
			}
		}
		for _, w := range []string{"lyric", "lyrics", "audio", "visualizer", "visualiser"} {
			if hasWord(v, w) && !hasWord(tt, w) {
				s -= 2.5 // still a picture, not the clip
			}
		}
		if dur > 0 && c.Dur > 0 {
			d := c.Dur - dur
			switch {
			case d < -20:
				s -= 4 // shorter than the song: cut / preview
			case d > 240:
				s -= 4 // a long film or a compilation
			case d > 60:
				s -= 1 // a long intro / outro (the offset keys fix it)
			default:
				s += 1
			}
		}
		s -= float64(i) * 0.15 // YouTube's own order breaks ties
		c.Score = s
	}
	out := append([]videoCand(nil), cands...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	var good []videoCand
	for _, c := range out {
		if c.Score > 0 {
			good = append(good, c)
		}
	}
	return good
}

// findVideos searches YouTube for the song's video.
func findVideos(title, artist string, dur float64) []videoCand {
	q := cleanTitle(title) + " " + firstArtist(artist) + " official video"
	req, _ := http.NewRequest("GET", youtubeBase+"/results?search_query="+url.QueryEscape(q)+"&hl=en", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+1"})
	cl := &http.Client{Timeout: 12 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		logf("video search: %v", err)
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	return rankVideos(parseYouTubeResults(string(b)), title, artist, dur)
}

// videoState: what the full-screen video player should show (shared with the settings server,
// which hands it to the player page).
type videoState struct {
	sync.Mutex
	Key     string      // the song
	Cands   []videoCand // found videos, best first
	Pick    int         // which one plays
	Offset  float64     // video time = song time + Offset (keys , and . nudge it)
	Want    bool        // full screen wants a video now
	Seen    time.Time   // last time the player page reported playing
	Err     string
	Busy    bool
	offsets map[string]float64 // remembered nudges per song
}

var video = &videoState{offsets: map[string]float64{}}

// current: the id to play ("" = none).
func (v *videoState) current() string {
	if v.Pick >= 0 && v.Pick < len(v.Cands) {
		return v.Cands[v.Pick].ID
	}
	return ""
}

// skip moves to the next candidate (the page reports one it can't play).
func (v *videoState) skip() {
	v.Pick++
	v.Offset = v.offsets[v.Key+"|"+v.current()]
}

// nudge shifts the video against the song and remembers it for this song and video.
func (v *videoState) nudge(d float64) {
	v.Offset += d
	v.offsets[v.Key+"|"+v.current()] = v.Offset
}

// forSong starts the search when the song changes (search runs in the background).
func (v *videoState) forSong(t *Track) {
	if t == nil {
		v.Lock()
		v.Key, v.Cands, v.Pick = "", nil, 0
		v.Unlock()
		return
	}
	v.Lock()
	if v.Key == t.Key {
		v.Unlock()
		return
	}
	v.Key, v.Cands, v.Pick, v.Offset, v.Err, v.Busy = t.Key, nil, 0, 0, "", true
	key, title, artist, dur := t.Key, t.Title, t.Artist, t.Duration
	v.Unlock()
	go func() {
		defer func() { recover() }()
		c := findVideos(title, artist, dur)
		v.Lock()
		defer v.Unlock()
		if v.Key != key {
			return
		}
		v.Busy = false
		v.Cands = c
		if len(c) == 0 {
			v.Err = "no video found"
		}
		v.Offset = v.offsets[key+"|"+v.current()]
		// a long intro before the song: start that much into the video
		if len(c) > 0 && v.Offset == 0 && dur > 0 && c[0].Dur-dur > 15 && c[0].Dur-dur < 120 {
			v.Offset = (c[0].Dur - dur) * 0.8
		}
		logf("video for %q: %d found, playing %q", title, len(c), v.current())
	}()
}

// hasWord: w appears in s as a whole word ("live" in "Live at…", not in "Alive").
func hasWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		a, b := i+j, i+j+len(w)
		okA := a == 0 || !isWordByte(s[a-1])
		okB := b == len(s) || !isWordByte(s[b])
		if okA && okB {
			return true
		}
		i = a + 1
	}
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c >= 0x80
}
