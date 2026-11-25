package main

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Track is what is playing right now.
type Track struct {
	Key      string
	Title    string
	Artist   string
	Album    string
	Duration float64 // seconds, 0 = unknown
	Cover    []byte  // encoded image (png/jpg), may be nil
	CoverVer int     // bumps when the cover changes
}

// Media is shared between the media poller goroutine and the UI thread.
type Media struct {
	mu                sync.Mutex
	track             *Track
	playing           bool
	pos               float64
	posAt             time.Time
	seekTo            float64 // a jump the user asked for, while the player catches up
	seekAt, seekUntil time.Time
	exact             bool   // position comes from the player (true) or is estimated (false)
	source            string // "smtc" or "spotify-title"
	version           int    // bumps on every track change
	offset            float64
	modes             Modes
	modesHold         time.Time // the user just flipped one: the player's old answer is ignored briefly
	// 1.19.1: small differences between our clock and the player's are eased in over slewDur
	// instead of jumping (the lyrics used to twitch back and forth every poll)
	slewAmt float64
	slewAt  time.Time
	slewLen float64 // seconds this correction is spread over
}

const (
	slewDur  = 0.6 // seconds a small correction is spread over
	slewJump = 0.9 // a difference bigger than this is a real jump: followed at once
)

// cur: the position now, without the lyrics offset (m.mu held).
func (m *Media) cur(now time.Time) float64 {
	p := m.pos
	if m.playing && !m.posAt.IsZero() {
		p += now.Sub(m.posAt).Seconds()
	}
	if m.slewAmt != 0 {
		k := now.Sub(m.slewAt).Seconds() / math.Max(slewDur, m.slewLen)
		if k > 1 {
			k = 1
		}
		p += m.slewAmt * k
	}
	return p
}

// fold puts the eased-in part of the correction into pos (m.mu held).
func (m *Media) fold(now time.Time) {
	m.pos, m.posAt, m.slewAmt = m.cur(now), now, 0
}

// Modes: shuffle and repeat as the player reports them. -1 = not known.
// Repeat: 0 off, 1 this song, 2 the list.
type Modes struct {
	Shuffle, Repeat       int
	CanShuffle, CanRepeat bool // the player takes these from outside (Windows media controls)
}

type MediaSnap struct {
	Track   *Track
	Playing bool
	Pos     float64
	Exact   bool
	Source  string
	Version int
	Modes   Modes
}

var media = &Media{}

func (m *Media) Snapshot() MediaSnap {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.cur(time.Now())
	if m.track != nil && m.track.Duration > 0 && p > m.track.Duration {
		p = m.track.Duration
	}
	return MediaSnap{Track: m.track, Playing: m.playing, Pos: p + m.offset, Exact: m.exact, Source: m.source, Version: m.version, Modes: m.modes}
}

// SetTrack updates the current track; returns true when it is a different song.
func (m *Media) SetTrack(title, artist, album string, source string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if title == "" {
		if m.track != nil {
			m.track = nil
			m.version++
		}
		return false
	}
	key := keyOf(title, artist)
	if m.track != nil && m.track.Key == key {
		return false
	}
	m.track = &Track{Key: key, Title: title, Artist: artist, Album: album}
	m.pos, m.posAt, m.slewAmt = 0, time.Now(), 0
	m.exact = false
	m.source = source
	m.version++
	return true
}

func (m *Media) SetCover(key string, img []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track != nil && m.track.Key == key && len(img) > 0 {
		t := *m.track
		t.Cover = img
		t.CoverVer++
		m.track = &t
	}
}

func (m *Media) SetDuration(key string, d float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track != nil && m.track.Key == key && d > 0 && m.track.Duration != d {
		t := *m.track
		t.Duration = d
		m.track = &t
	}
}

// SetPosition sets an exact position reported by the player.
func (m *Media) SetPosition(pos float64, playing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Now().Before(m.seekUntil) { // just jumped: the player reports the old place for a moment
		exp := m.seekTo
		if m.playing {
			exp += time.Since(m.seekAt).Seconds()
		}
		if math.Abs(pos-exp) > 2.5 {
			m.playing = playing
			return
		}
		m.seekUntil = time.Time{}
		m.pos, m.posAt, m.playing, m.exact, m.slewAmt = pos, time.Now(), playing, true, 0
		return
	}
	now := time.Now()
	if m.exact && playing && m.playing && m.track != nil {
		shown := m.cur(now)
		if d := pos - shown; math.Abs(d) <= slewJump {
			// the player's clock and ours drift by a few tens of ms each poll: ease it in
			m.pos, m.posAt = shown, now
			m.slewAmt, m.slewAt = 0, now
			if math.Abs(d) > 0.015 {
				// never backwards: time only slows down (to 30 % at least) while it catches up
				m.slewAmt, m.slewLen = d, math.Max(slewDur, -d/0.7)
			}
			return
		}
	}
	m.pos, m.posAt, m.playing, m.exact, m.slewAmt = pos, now, playing, true, 0
}

// Seek: the place the user jumped to, shown at once (the player catches up a moment later).
func (m *Media) Seek(pos float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pos < 0 {
		pos = 0
	}
	if m.track != nil && m.track.Duration > 0 && pos > m.track.Duration-0.5 {
		pos = math.Max(0, m.track.Duration-0.5)
	}
	now := time.Now()
	m.pos, m.posAt, m.seekTo, m.seekAt, m.seekUntil = pos, now, pos, now, now.Add(2500*time.Millisecond)
	m.slewAmt = 0
}

// SetPlaying changes play state while keeping the estimated position continuous.
func (m *Media) SetPlaying(playing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if playing == m.playing {
		return
	}
	m.fold(time.Now())
	m.playing = playing
}

func (m *Media) Nudge(sec float64) {
	m.mu.Lock()
	m.offset += sec
	m.mu.Unlock()
}

func (m *Media) SetOffset(sec float64) {
	m.mu.Lock()
	m.offset = sec
	m.mu.Unlock()
}

func (m *Media) Offset() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offset
}

/* ---------- Normalising titles ---------- */

var (
	reParen  = regexp.MustCompile(`\(.*?\)|\[.*?\]`)
	reDash   = regexp.MustCompile(`\s+-\s+.*$`)
	reNonAln = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	reArtSep = regexp.MustCompile(`(?i),|&| feat\.| ft\.| x `)
)

func normStr(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(foldRune, s)
	s = reParen.ReplaceAllString(s, "")
	s = reDash.ReplaceAllString(s, "")
	s = reNonAln.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func firstArtist(a string) string {
	return strings.TrimSpace(reArtSep.Split(a, 2)[0])
}

func keyOf(title, artist string) string { return normStr(title) + "|" + normStr(firstArtist(artist)) }

func cleanTitle(t string) string {
	t = reParen.ReplaceAllString(t, " ")
	t = reDash.ReplaceAllString(t, "")
	return strings.Join(strings.Fields(t), " ")
}

/* ---------- App folder ---------- */

func appDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	if base == "" {
		base = "."
	}
	d := filepath.Join(base, "OrbitLyrics")
	_ = os.MkdirAll(filepath.Join(d, "cache"), 0o755)
	return d
}

// foldRune strips accents from common Latin letters (é→e, ş→s, ı→i …) so titles match lyric databases.
var foldTable = map[rune]rune{}

func init() {
	groups := map[rune]string{
		'a': "àáâãäåāăą", 'c': "çćĉċč", 'd': "ďđ", 'e': "èéêëēĕėęě", 'g': "ĝğġģ", 'h': "ĥħ",
		'i': "ìíîïĩīĭįıİ", 'j': "ĵ", 'k': "ķ", 'l': "ĺļľŀł", 'n': "ñńņňŉ", 'o': "òóôõöøōŏő",
		'r': "ŕŗř", 's': "śŝşšș", 't': "ţťŧț", 'u': "ùúûüũūŭůűų", 'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	}
	for base, list := range groups {
		for _, r := range list {
			foldTable[r] = base
		}
	}
}

func foldRune(r rune) rune {
	if f, ok := foldTable[r]; ok {
		return f
	}
	return r
}

// SetModes: what the player reports (ignored for a moment after the user changed one).
func (m *Media) SetModes(md Modes) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Now().Before(m.modesHold) {
		md.Shuffle, md.Repeat = m.modes.Shuffle, m.modes.Repeat
	}
	m.modes = md
}

// WantModes: the user flipped shuffle or repeat — shown at once.
func (m *Media) WantModes(shuffle, repeat int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if shuffle >= 0 {
		m.modes.Shuffle = shuffle
	}
	if repeat >= 0 {
		m.modes.Repeat = repeat
	}
	m.modesHold = time.Now().Add(2500 * time.Millisecond)
}

// looksLikeID: a "title" that is really a hash or id (some players send the file's name, e.g.
// 79ead9f36e7f4fc835…): 16+ hex digits / letters with no spaces.
func looksLikeID(s string) bool {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "."); i > 0 && len(s)-i <= 5 { // "….mp3"
		s = s[:i]
	}
	if len(s) < 16 || strings.ContainsAny(s, " ") {
		return false
	}
	hex := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			hex++
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return hex >= 16
}
