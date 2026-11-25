//go:build windows || darwin

package main

// The full screen's player: previous / next (keeping the song list in order), shuffle and
// repeat, and what plays next. (Kept from the older full-screen views for Cinema.)

import (
	"strings"
	"time"
)

const (
	vkMediaNext = 0xB0
	vkMediaPrev = 0xB1
)

// fsNav: Orbit stepping through songs itself — what it knows plays next, and how long to hold
// the song list while the player catches up.
type fsNav struct {
	upNext   []HistItem
	navUntil time.Time
}

// backCount: how many songs back key is from the playing one (0 = not in the list).
func (f *FullView) backCount(key string, snap MediaSnap) int {
	hist, cur := f.pastSongs(snap)
	n := 0
	for _, h := range hist {
		if h.Key == cur {
			continue
		}
		n++
		if h.Key == key {
			return n
		}
	}
	return 0
}

func (f *FullView) pressKeys(vk uintptr, n int) {
	if n <= 0 {
		return
	}
	f.nav.navUntil = time.Now().Add(time.Duration(n)*260*time.Millisecond + 2500*time.Millisecond)
	historyHold = f.nav.navUntil
	go func() {
		for i := 0; i < n; i++ {
			mediaKey(vk)
			time.Sleep(220 * time.Millisecond)
		}
	}()
}

// goBack n songs: the list becomes "that song first", the songs passed over become "up next".
func (f *FullView) goBack(n int, snap MediaSnap) {
	hist, cur := f.pastSongs(snap)
	var past []HistItem
	var curItem *HistItem
	for i := range hist {
		if hist[i].Key == cur {
			c := hist[i]
			curItem = &c
			continue
		}
		past = append(past, hist[i])
	}
	if n < 1 || n > len(past) {
		return
	}
	if curItem == nil && snap.Track != nil {
		curItem = &HistItem{Key: snap.Track.Key, Title: snap.Track.Title, Artist: snap.Track.Artist}
	}
	ahead := []HistItem{}
	for i := n - 2; i >= 0; i-- {
		ahead = append(ahead, past[i])
	}
	if curItem != nil {
		ahead = append(ahead, *curItem)
	}
	f.nav.upNext = append(ahead, f.nav.upNext...)
	if len(f.nav.upNext) > 12 {
		f.nav.upNext = f.nav.upNext[:12]
	}
	rest := append([]HistItem(nil), past[n-1:]...)
	withCfg(func() { cfg.History = rest })
	saveConfigSoon()
	presses := n
	if snap.Pos > 3 { // Previous first rewinds the song itself when it has played a while
		presses++
	}
	f.pressKeys(vkMediaPrev, presses)
	f.say(strings.Replace(T("Back to “{x}”"), "{x}", past[n-1].Title, 1))
}

// goNext j songs of the known "up next" ones.
func (f *FullView) goNext(j int, snap MediaSnap) {
	next := f.nextSongs(snap)
	if j < 1 || j > len(next) {
		return
	}
	f.nav.upNext = next
	spotify.stale() // the queue moves on: ask again once the song has changed
	hist, cur := f.pastSongs(snap)
	var front []HistItem
	for i := j - 1; i >= 0; i-- {
		front = append(front, f.nav.upNext[i])
	}
	if snap.Track != nil && (len(hist) == 0 || hist[0].Key != cur) {
		front = append(front, HistItem{Key: snap.Track.Key, Title: snap.Track.Title, Artist: snap.Track.Artist})
	}
	target := f.nav.upNext[j-1]
	f.nav.upNext = f.nav.upNext[j:]
	newHist := append(front, hist...)
	seen := map[string]bool{}
	var dedup []HistItem
	for _, h := range newHist {
		if !seen[h.Key] {
			seen[h.Key] = true
			dedup = append(dedup, h)
		}
	}
	if len(dedup) > 8 {
		dedup = dedup[:8]
	}
	withCfg(func() { cfg.History = dedup })
	saveConfigSoon()
	f.pressKeys(vkMediaNext, j)
	f.say(strings.Replace(T("On to “{x}”"), "{x}", target.Title, 1))
}

// stepPrev / stepNext: the bar's buttons and the arrow keys, keeping the list in order.
func (f *FullView) stepPrev(snap MediaSnap) {
	if snap.Pos > 3 {
		mediaKey(vkMediaPrev) // rewinds the song (or nothing known before it)
		return
	}
	hist, cur := f.pastSongs(snap)
	for _, h := range hist {
		if h.Key != cur {
			f.goBack(1, snap)
			return
		}
	}
	mediaKey(vkMediaPrev)
}

func (f *FullView) stepNext(snap MediaSnap) {
	if len(f.nextSongs(snap)) > 0 {
		f.goNext(1, snap)
		return
	}
	mediaKey(vkMediaNext)
}

// followSong keeps "up next" right when songs change on their own.
func (f *FullView) followSong(snap MediaSnap) {
	if f.spotifyOn() {
		spotify.refreshQueue() // (the old queue stays up meanwhile — nextSongs finds the new song in it)
	}
	if snap.Track == nil || time.Now().Before(f.nav.navUntil) || len(f.nav.upNext) == 0 {
		return
	}
	for i, u := range f.nav.upNext {
		if u.Key == snap.Track.Key { // it played on into the songs we knew
			f.nav.upNext = f.nav.upNext[i+1:]
			return
		}
	}
	f.nav.upNext = nil // something else is playing: the old "next" is no longer known
}

// spotifyOn: the user connected their Spotify account in the settings.
func (f *FullView) spotifyOn() bool { return cfg.SpotifyClientID != "" && cfg.SpotifyRefresh != "" }

// nextSongs: what plays after the playing song — Spotify's real queue when connected, else the
// songs Orbit knows from going back. Empty when unknown.
func (f *FullView) nextSongs(snap MediaSnap) []HistItem {
	if f.spotifyOn() && snap.Track != nil && time.Now().After(f.nav.navUntil) {
		q, playing := spotify.snapshot()
		q = queueAfter(q, playing, snap.Track.Title, snap.Track.Key)
		if len(q) > 0 {
			out := make([]HistItem, 0, len(q))
			for _, it := range q {
				h := HistItem{Key: keyOf(it.Title, it.Artist), Title: it.Title, Artist: it.Artist}
				h.CoverFile = it.File
				if h.Key == snap.Track.Key && len(out) == 0 { // Spotify repeats the playing song first sometimes
					continue
				}
				out = append(out, h)
			}
			return out
		}
	}
	return f.nav.upNext
}

// pastSongs: the playing song first, then the ones before it (as Orbit remembers them), and the
// playing song's key.
func (f *FullView) pastSongs(snap MediaSnap) ([]HistItem, string) {
	cur := ""
	if snap.Track != nil {
		cur = snap.Track.Key
	}
	return cfg.History, cur
}

// modes: shuffle and repeat to show — Windows' answer, else Spotify's — and whether they can be
// changed from here. (Reads the settings directly: called while drawing.)
func (f *FullView) modes(snap MediaSnap) (shuf, rep int, canShuf, canRep bool) {
	shuf, rep = snap.Modes.Shuffle, snap.Modes.Repeat
	sc := f.spotifyCan()
	if shuf < 0 || rep < 0 {
		s2, r2 := spotify.modes()
		if shuf < 0 {
			shuf = s2
		}
		if rep < 0 {
			rep = r2
		}
	}
	return shuf, rep, snap.Modes.CanShuffle || sc, snap.Modes.CanRepeat || sc
}

// spotifyCan: connected with permission to change the playback (lock-free: used while drawing).
func (f *FullView) spotifyCan() bool {
	if !f.spotifyOn() {
		return false
	}
	spotify.Lock()
	sc := spotify.scope
	spotify.Unlock()
	return strings.Contains(cfg.SpotifyScopes+" "+sc, "user-modify-playback-state")
}

// toggleMode: shuffle on/off; repeat off → all → this song → off.
func (f *FullView) toggleMode(id string, snap MediaSnap) {
	shuf, rep, canShuf, canRep := f.modes(snap)
	if id == "shuffle" {
		on := shuf != 1
		if !canShuf || !requestShuffle(on) {
			f.say(f.cantMsg("Shuffle"))
			return
		}
		f.say("Shuffle · " + onOff(on))
		return
	}
	next := map[int]int{0: 2, 2: 1, 1: 0}[max(rep, 0)]
	if !canRep || !requestRepeat(next) {
		f.say(f.cantMsg("Repeat"))
		return
	}
	f.say("Repeat · " + []string{"Off", "This song", "All"}[next])
}

func (f *FullView) cantMsg(what string) string {
	if f.spotifyOn() && !f.spotifyCan() {
		return T(what) + ": " + T("connect Spotify again in Settings (Orbit needs one more permission)")
	}
	return T(what) + ": " + T("your player doesn't allow it from outside — connect Spotify in Settings")
}
