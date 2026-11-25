package main

// Finding a big, sharp copy of the cover for the full-screen background.
//
// The player itself only hands over a small cover (Spotify: 300 px), which is mush when it is
// spread over a 1440p screen. So a big one is looked up: iTunes (2000 px) by song, then by album,
// then Deezer (1000–1800 px). Every candidate is compared with the player's own cover at 8×8 and
// only the same picture is used, so a wrong match never shows.

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// coverSig is a cover's middle square at 8×8 (r, g, b), to tell whether two pictures are the same cover.
type coverSig [64][3]float64

func sigDiff(a, b *coverSig) float64 {
	var d float64
	for i := range a {
		for k := 0; k < 3; k++ {
			d += math.Abs(a[i][k] - b[i][k])
		}
	}
	return d / (64 * 3 * 255)
}

// sigFromImage: the middle square, averaged into 8×8 boxes (decoded in Go, off the UI thread).
func sigFromImage(im image.Image) *coverSig {
	r := im.Bounds()
	w, h := r.Dx(), r.Dy()
	if w < 8 || h < 8 {
		return nil
	}
	s := min(w, h)
	x0, y0 := r.Min.X+(w-s)/2, r.Min.Y+(h-s)/2
	step := max(1, s/64) // about 64 samples a side are plenty for an average
	var sig coverSig
	var cnt [64]float64
	for y := 0; y < s; y += step {
		by := y * 8 / s
		for x := 0; x < s; x += step {
			bx := x * 8 / s
			cr, cg, cb, _ := im.At(x0+x, y0+y).RGBA()
			i := by*8 + bx
			sig[i][0] += float64(cr >> 8)
			sig[i][1] += float64(cg >> 8)
			sig[i][2] += float64(cb >> 8)
			cnt[i]++
		}
	}
	for i := range sig {
		if cnt[i] > 0 {
			for k := 0; k < 3; k++ {
				sig[i][k] /= cnt[i]
			}
		}
	}
	return &sig
}

func decodeSize(b []byte) (int, int) {
	c, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0
	}
	return c.Width, c.Height
}

// alnum: lower-case letters and digits only, accents folded ("Ünlü Ş." → "unlus").
func alnum(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if f, ok := foldTable[r]; ok {
			r = f
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func sameArtist(a, want string) bool {
	a, want = alnum(a), alnum(firstArtist(want))
	return want != "" && a != "" && (strings.Contains(a, want) || strings.Contains(want, a))
}

// coverCandidates: the big covers to try, best first (the artist's own ones before the others).
var deezerBase = "https://api.deezer.com"

func coverCandidates(title, artist, album string) (good, other []string, answered bool) {
	seen := map[string]bool{}
	add := func(u, by string) {
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		if sameArtist(by, artist) {
			good = append(good, u)
		} else {
			other = append(other, u)
		}
	}
	type itRes struct {
		Results []struct {
			ArtistName    string `json:"artistName"`
			ArtworkUrl100 string `json:"artworkUrl100"`
		} `json:"results"`
	}
	itunes := func(entity, term string) {
		var res itRes
		u := itunesBase + "/search?entity=" + entity + "&limit=8&term=" + url.QueryEscape(term)
		if code, err := httpJSON(u, &res); err != nil || code != 200 {
			return
		}
		answered = true
		for _, r := range res.Results {
			if strings.Contains(r.ArtworkUrl100, "100x100bb") {
				add(strings.Replace(r.ArtworkUrl100, "100x100bb", "2000x2000bb", 1), r.ArtistName)
			}
		}
	}
	type dzRes struct {
		Data []struct {
			CoverXL string `json:"cover_xl"`
			Artist  struct {
				Name string `json:"name"`
			} `json:"artist"`
			Album struct {
				CoverXL string `json:"cover_xl"`
			} `json:"album"`
		} `json:"data"`
	}
	deezer := func(path, q string) {
		var res dzRes
		u := deezerBase + path + "?limit=8&q=" + url.QueryEscape(q)
		if code, err := httpJSON(u, &res); err != nil || code != 200 {
			return
		}
		answered = true
		for _, d := range res.Data {
			c := d.Album.CoverXL
			if c == "" {
				c = d.CoverXL
			}
			add(c, d.Artist.Name)
		}
	}
	t, a := cleanTitle(title), firstArtist(artist)
	itunes("song", t+" "+a)
	if len(good) == 0 && album != "" {
		itunes("album", cleanTitle(album)+" "+a)
	}
	if len(good) == 0 {
		deezer("/search", `artist:"`+a+`" track:"`+t+`"`)
	}
	if len(good) == 0 && album != "" {
		deezer("/search/album", `artist:"`+a+`" album:"`+cleanTitle(album)+`"`)
	}
	if len(good) == 0 {
		deezer("/search", t+" "+a)
	}
	return good, other, answered
}

func download(u string, limit int) []byte {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "OrbitLyrics/1.0 (desktop app)")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	b := make([]byte, 0, 400000)
	buf := make([]byte, 32768)
	for {
		n, e := resp.Body.Read(buf)
		b = append(b, buf[:n]...)
		if len(b) > limit {
			return nil
		}
		if e != nil {
			break
		}
	}
	return b
}

// findBigCover returns a big copy of the cover (nil = none found; answered = false when no
// service could be reached, so nothing should be remembered). ref is the player's own cover
// at 8×8: only the same picture is accepted. Without ref, only the artist's own cover is taken.
func findBigCover(title, artist, album string, ref *coverSig) (_ []byte, answered bool) {
	good, other, answered := coverCandidates(title, artist, album)
	cands := good
	if ref != nil {
		cands = append(good, other...)
	}
	tries := 0
	for _, u := range cands {
		if tries >= 5 {
			break
		}
		tries++
		urls := []string{u}
		if strings.Contains(u, "/1000x1000-") { // Deezer: ask for a bigger one first
			urls = []string{strings.Replace(u, "/1000x1000-", "/1800x1800-", 1), u}
		}
		for _, v := range urls {
			b := download(v, 8<<20)
			if len(b) == 0 {
				continue
			}
			im, _, err := image.Decode(bytes.NewReader(b))
			if err != nil {
				continue
			}
			if w, h := im.Bounds().Dx(), im.Bounds().Dy(); w < 600 || h < 600 {
				continue // no bigger than what the player gives
			}
			if ref != nil {
				s := sigFromImage(im)
				if s == nil {
					continue
				}
				if d := sigDiff(s, ref); d > 0.16 {
					logf("big cover: %s is another picture (%.2f)", shortURL(v), d)
					break // the smaller copy of it would be too
				}
			}
			logf("big cover: %dx%d from %s", im.Bounds().Dx(), im.Bounds().Dy(), shortURL(v))
			return b, true
		}
	}
	return nil, answered
}

func shortURL(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return "?"
}
