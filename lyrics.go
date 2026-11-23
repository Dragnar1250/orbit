package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Word struct {
	W string
	T float64
}

type Line struct {
	T     float64
	Text  string
	Words []Word
	End   float64 // start of the next line
	Sing  float64 // estimated time it takes to sing the line
}

const (
	lyrNone = iota
	lyrLoading
	lyrSynced
	lyrPlain
	lyrNotFound
	lyrInstrumental
)

type Lyrics struct {
	mu     sync.Mutex
	key    string
	status int
	lines  []Line
	reqID  int
}

var lyrics = &Lyrics{}

func (l *Lyrics) Get() (string, int, []Line) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.key, l.status, l.lines
}

/* ---------- LRC parsing ---------- */

var (
	reTag    = regexp.MustCompile(`\[(\d+):(\d+(?:[.:]\d+)?)\]`)
	reOffset = regexp.MustCompile(`(?i)^\[offset:\s*([+-]?\d+)\]`)
	reWordT  = regexp.MustCompile(`<(\d+):(\d+(?:\.\d+)?)>\s*([^<]*)`)
)

func parseLRC(txt string) []Line {
	var out []Line
	offset := 0.0
	for _, raw := range strings.Split(strings.ReplaceAll(txt, "\r", ""), "\n") {
		if m := reOffset.FindStringSubmatch(raw); m != nil {
			v, _ := strconv.Atoi(m[1])
			offset = float64(v) / 1000
			continue
		}
		tags := reTag.FindAllStringSubmatch(raw, -1)
		if len(tags) == 0 {
			continue
		}
		text := strings.TrimSpace(reTag.ReplaceAllString(raw, ""))
		var words []Word
		if wt := reWordT.FindAllStringSubmatch(text, -1); len(wt) > 0 {
			var parts []string
			for _, m := range wt {
				w := strings.TrimSpace(m[3])
				if w == "" {
					continue
				}
				mm, _ := strconv.ParseFloat(m[1], 64)
				ss, _ := strconv.ParseFloat(m[2], 64)
				words = append(words, Word{w, mm*60 + ss})
				parts = append(parts, w)
			}
			text = strings.Join(parts, " ")
		}
		for _, m := range tags {
			mm, _ := strconv.ParseFloat(m[1], 64)
			ss, _ := strconv.ParseFloat(strings.Replace(m[2], ":", ".", 1), 64)
			ln := Line{T: mm*60 + ss - offset, Text: text}
			if words != nil {
				ln.Words = append([]Word(nil), words...)
			}
			out = append(out, ln)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

func plainToLines(txt string, dur float64) []Line {
	var ls []string
	for _, s := range strings.Split(strings.ReplaceAll(txt, "\r", ""), "\n") {
		if s = strings.TrimSpace(s); s != "" {
			ls = append(ls, s)
		}
	}
	total := dur
	if total <= 0 {
		total = float64(len(ls))*3.6 + 10
	}
	step := (total - 10) / math.Max(1, float64(len(ls)))
	out := make([]Line, len(ls))
	for i, s := range ls {
		out[i] = Line{T: 8 + float64(i)*step, Text: s}
	}
	return out
}

// prepare estimates when each word is sung (LRC only gives line start times).
func prepare(ls []Line) []Line {
	for i := range ls {
		L := &ls[i]
		next := L.T + 6
		if i+1 < len(ls) {
			next = ls[i+1].T
		}
		L.End = next
		if len(L.Words) == 0 {
			ws := strings.Fields(L.Text)
			// how long the line takes: from its letters, stretched toward the time until the next
			// line (slow songs) but not across a long instrumental gap, never past the next line
			chars := 0
			for _, w := range ws {
				chars += len([]rune(w)) + 1
			}
			gap := next - L.T
			base := math.Max(1.0, float64(chars)*0.075+0.35)
			sing := math.Min(gap*0.9, math.Max(base, math.Min(gap*0.62, base*1.6)))
			sing = math.Max(0.25, sing)
			// each word takes about as long as its syllables, a comma adds a breath
			weights := make([]float64, len(ws))
			total := 0.0
			for k, w := range ws {
				weights[k] = wordWeight(w)
				total += weights[k]
			}
			if total == 0 {
				total = 1
			}
			acc := 0.0
			L.Words = make([]Word, len(ws))
			for k, w := range ws {
				L.Words[k] = Word{w, L.T + sing*acc/total}
				acc += weights[k]
			}
			L.Sing = sing
		} else {
			last := L.Words[len(L.Words)-1].T + 0.4
			L.Sing = math.Max(0.3, last-L.T)
		}
	}
	return ls
}

// wordWeight: roughly how long a word is sung — its syllables (vowel groups), a little for its
// letters, and a short breath after a comma or other punctuation.
func wordWeight(w string) float64 {
	groups, prevVowel, letters := 0, false, 0
	for _, r := range strings.ToLower(w) {
		v := strings.ContainsRune("aeiouyáéíóúàèìòùâêîôûäëïöüıåæøœ", r)
		if v && !prevVowel {
			groups++
		}
		prevVowel = v
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters++
		}
	}
	if groups == 0 {
		groups = 1
	}
	wt := float64(groups) + 0.12*float64(letters) + 0.25
	if strings.ContainsAny(w[len(w)-1:], ",;:.!?…—") {
		wt += 0.6
	}
	return wt
}

func lineAt(ls []Line, p float64) int {
	k := -1
	for i := range ls {
		if ls[i].T <= p {
			k = i
		} else {
			break
		}
	}
	return k
}

func wordAt(L *Line, p float64) int {
	k := -1
	for i, w := range L.Words {
		if w.T <= p {
			k = i
		}
	}
	return k
}

/* ---------- lrclib.net ---------- */

type lrcRec struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  *string `json:"plainLyrics"`
	SyncedLyrics *string `json:"syncedLyrics"`
}

type cached struct {
	Synced       string `json:"synced"`
	Plain        string `json:"plain"`
	Instrumental bool   `json:"instrumental"`
	NotFound     bool   `json:"notFound"`
	At           int64  `json:"at"`
}

var httpc = &http.Client{Timeout: 12 * time.Second}

var lrclibBase = "https://lrclib.net"

func httpJSON(u string, v any) (int, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "OrbitLyrics/1.0 (desktop app; https://github.com/)")
	req.Header.Set("Lrclib-Client", "OrbitLyrics/1.0")
	resp, err := httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	return 200, json.NewDecoder(resp.Body).Decode(v)
}

func cachePath(key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(appDir(), "cache", hex.EncodeToString(h[:8])+".json")
}

func lookupLyrics(t Track) (*cached, error) {
	if b, err := os.ReadFile(cachePath(t.Key)); err == nil {
		var c cached
		if json.Unmarshal(b, &c) == nil {
			// "not found" answers are re-checked after 3 days (songs get added)
			if !c.NotFound || time.Since(time.Unix(c.At, 0)) < 72*time.Hour {
				return &c, nil
			}
		}
	}
	q := url.QueryEscape
	var rec *lrcRec
	netErr := false
	if t.Duration > 0 {
		var r lrcRec
		code, err := httpJSON(fmt.Sprintf(lrclibBase+"/api/get?track_name=%s&artist_name=%s&album_name=%s&duration=%d",
			q(t.Title), q(t.Artist), q(t.Album), int(math.Round(t.Duration))), &r)
		if err != nil {
			netErr = true
		} else if code == 200 {
			rec = &r
		}
	}
	if rec == nil || (rec.SyncedLyrics == nil && rec.PlainLyrics == nil && !rec.Instrumental) {
		title := cleanTitle(t.Title)
		fa := firstArtist(t.Artist)
		tries := []string{
			"track_name=" + q(t.Title) + "&artist_name=" + q(t.Artist),
			"track_name=" + q(title) + "&artist_name=" + q(fa),
			"q=" + q(title+" "+fa),
		}
		for _, tq := range tries {
			var arr []lrcRec
			_, err := httpJSON(lrclibBase+"/api/search?"+tq, &arr)
			if err != nil {
				netErr = true
				continue
			}
			if best := pickBest(arr, title, fa, t.Duration); best != nil {
				rec = best
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if rec == nil {
		if netErr {
			return nil, fmt.Errorf("network error")
		}
		c := &cached{NotFound: true, At: time.Now().Unix()}
		saveCache(t.Key, c)
		return c, nil
	}
	c := &cached{Instrumental: rec.Instrumental, At: time.Now().Unix()}
	if rec.SyncedLyrics != nil {
		c.Synced = *rec.SyncedLyrics
	}
	if rec.PlainLyrics != nil {
		c.Plain = *rec.PlainLyrics
	}
	saveCache(t.Key, c)
	return c, nil
}

func pickBest(arr []lrcRec, title, artist string, dur float64) *lrcRec {
	want, wa := normStr(title), normStr(artist)
	score := func(x lrcRec) int {
		s := 0
		if x.SyncedLyrics != nil && *x.SyncedLyrics != "" {
			s += 4
		} else if x.PlainLyrics != nil && *x.PlainLyrics != "" {
			s++
		}
		if normStr(x.TrackName) == want {
			s += 3
		}
		if wa != "" && strings.Contains(normStr(x.ArtistName), wa) {
			s += 2
		}
		if dur > 0 && math.Abs(x.Duration-dur) <= 3 {
			s += 3
		}
		return s
	}
	best, bs := -1, -1
	for i, x := range arr {
		if sc := score(x); sc > bs {
			best, bs = i, sc
		}
	}
	if best < 0 || bs < 4 {
		return nil
	}
	return &arr[best]
}

func saveCache(key string, c *cached) {
	b, _ := json.Marshal(c)
	_ = os.WriteFile(cachePath(key), b, 0o644)
}

// Load fetches lyrics for t in the background.
func (l *Lyrics) Load(t *Track) {
	l.mu.Lock()
	l.reqID++
	id := l.reqID
	if t == nil {
		l.key, l.status, l.lines = "", lyrNone, nil
		l.mu.Unlock()
		return
	}
	l.key, l.status, l.lines = t.Key, lyrLoading, nil
	l.mu.Unlock()
	tr := *t
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logf("lyrics panic: %v", r)
			}
		}()
		// give the player a moment to report the duration (needed for an exact match)
		if tr.Duration == 0 {
			time.Sleep(1200 * time.Millisecond)
			if s := media.Snapshot(); s.Track != nil && s.Track.Key == tr.Key {
				tr.Duration = s.Track.Duration
			}
		}
		var c *cached
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			c, err = lookupLyrics(tr)
			if err == nil {
				break
			}
			time.Sleep(time.Duration(3+attempt*5) * time.Second)
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		if id != l.reqID {
			return
		}
		switch {
		case c == nil || c.NotFound:
			l.status = lyrNotFound
			if err != nil {
				logf("lyrics: %v", err)
			}
		case c.Instrumental:
			l.status = lyrInstrumental
		case c.Synced != "":
			l.lines, l.status = prepare(parseLRC(c.Synced)), lyrSynced
		case c.Plain != "":
			l.lines, l.status = prepare(plainToLines(c.Plain, tr.Duration)), lyrPlain
		default:
			l.status = lyrNotFound
		}
		logf("lyrics for %q: status %d, %d lines", tr.Title, l.status, len(l.lines))
	}()
}

/* ---------- Cover fallback (iTunes search, no key needed) ---------- */

var itunesBase = "https://itunes.apple.com" // tests point this at a local server

func fetchCoverOnline(title, artist string) []byte {
	return fetchCoverSized(title, artist, "600x600bb")
}

// fetchCoverHiRes: a big, sharp copy of the cover for full screen (2000 px).
func fetchCoverHiRes(title, artist string) []byte {
	return fetchCoverSized(title, artist, "2000x2000bb") // sharp even across a whole 1440p screen
}

func fetchCoverSized(title, artist, size string) []byte {
	var res struct {
		Results []struct {
			ArtworkUrl100 string `json:"artworkUrl100"`
		} `json:"results"`
	}
	u := itunesBase + "/search?entity=song&limit=1&term=" + url.QueryEscape(cleanTitle(title)+" "+firstArtist(artist))
	if code, err := httpJSON(u, &res); err != nil || code != 200 || len(res.Results) == 0 {
		return nil
	}
	art := strings.Replace(res.Results[0].ArtworkUrl100, "100x100bb", size, 1)
	req, _ := http.NewRequest("GET", art, nil)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	b := make([]byte, 0, 200000)
	buf := make([]byte, 32768)
	for {
		n, e := resp.Body.Read(buf)
		b = append(b, buf[:n]...)
		if e != nil || len(b) > 4<<20 {
			break
		}
	}
	return b
}
