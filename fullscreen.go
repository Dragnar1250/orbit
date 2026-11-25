//go:build windows || darwin

package main

// Full screen (Cinema): the view itself, shared by Windows and Mac. The window it lives in is
// in fullscreen_windows.go / fullscreen_darwin.go.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type FullView struct {
	hwnd       uintptr
	W, H       int
	mon        RECT
	winW, winH int     // the screen
	q          float64 // always 1 (kept for the mouse maths)
	frameMs    float64

	lyr *LyricsView // the lyrics stage's own lyrics (any of the lyric styles)
	vin *VinylView  // the turning record, when the cover is shown as one

	last time.Time
	t    float64
	ver  int

	hiKey, hiBusy string
	hiImg         uintptr
	hiVer         int

	mouseAt   time.Time
	mousePt   POINT
	cursorOff bool
	toast     string
	toastAt   time.Time
	nav       fsNav
	cin       cinState
}

func setFullscreen(open bool) {
	if open && fs == nil {
		openFullscreen()
	} else if !open && fs != nil {
		closeFullscreen()
	}
}

func toggleFullscreen() { setFullscreen(fs == nil) }

func offscreenWidget(name string) *Widget {
	return &Widget{name: name, label: name, alpha: 255, offscreen: true}
}

func (f *FullView) free() {
	f.cinemaFree()
	if f.lyr != nil {
		f.lyr.w.canvas.Free()
		f.lyr.w.canvas = nil
	}
	f.freeVinyl()
	if f.hiImg != 0 {
		freeImage(f.hiImg)
		f.hiImg = 0
	}
}

func (f *FullView) say(s string) { f.toast, f.toastAt = tJoin(s), time.Now() }

func cycle(v, n, d int) int { return ((v+d)%n + n) % n }

func (f *FullView) set(change func(), msg string) {
	withCfg(change)
	saveConfigSoon()
	styleDirty = true
	f.say(msg)
}

func (f *FullView) textSize(d int) {
	f.set(func() { cfg.FsTextSize = clampI(cfg.FsTextSize+d, 20, 200) }, fmt.Sprintf("Lyrics size · %d", cfg.FsTextSize+d))
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (f *FullView) Update(now time.Time, snap MediaSnap, bands [nBands]float32, bass float32) {
	dt := math.Max(0, math.Min(0.1, now.Sub(f.last).Seconds()))
	f.last = now
	f.t += dt
	if snap.Version != f.ver {
		f.ver = snap.Version
		if snap.Track != nil {
			pushHistory(snap.Track) // the desktop turntable is asleep: keep its list up to date
		}
		f.followSong(snap)
	}
	for _, n := range spotify.takeNotes() { // what Spotify answered to a change asked from here
		f.say(n)
	}
	updateCover(snap.Track)
	f.fetchHiRes(snap.Track)
	if !f.cursorOff && now.Sub(f.mouseAt) > 2500*time.Millisecond {
		pt := cursorPos()
		if pt.X >= f.mon.Left && pt.X < f.mon.Right && pt.Y >= f.mon.Top && pt.Y < f.mon.Bottom && fsInFront(f) {
			f.cursorOff = true // only our own screen, and only while we are in front
			showCursor(false)
		} else {
			f.mouseAt = now
		}
	}
	f.cinemaFrame(now, snap, dt, bands, bass)
}

func (f *FullView) fetchHiRes(t *Track) {
	if !cfg.FsHiRes || t == nil || f.hiKey == t.Key || f.hiBusy == t.Key {
		return
	}
	key, title, artist := t.Key, t.Title, t.Artist
	// already on disk (fetched earlier, or in the background while the song played): no wait
	if b, err := os.ReadFile(hiResPath(key)); err == nil && len(b) > 0 {
		f.useHiRes(key, b)
		return
	}
	f.hiBusy = key
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logf("hi-res cover panic: %v", r)
			}
		}()
		b := fetchHiResCached(key, title, artist)
		runOnUI(func() {
			if fs == nil || fs.hiBusy != key {
				return
			}
			fs.hiBusy = ""
			fs.useHiRes(key, b)
		})
	}()
}

// useHiRes shows the big cover if it really is this song's (iTunes may find another one).
func (f *FullView) useHiRes(key string, b []byte) {
	img := imageFromBytes(b)
	if img != 0 && cover.key == key && cover.img != 0 && imageDiff(img, cover.img) > 0.16 {
		logf("hi-res cover does not match the player's — keeping the original")
		freeImage(img)
		img = 0
	}
	freeImage(f.hiImg)
	f.hiImg, f.hiKey = img, key
	if img != 0 {
		f.hiVer++
	}
}

func hiResPath(key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(appDir(), "cache", "hires2k-"+hex.EncodeToString(h[:8])+".jpg")
}

// fetchHiResCached downloads the big cover once and keeps it (an empty file = none found).
func fetchHiResCached(key, title, artist string) []byte {
	p := hiResPath(key)
	if b, err := os.ReadFile(p); err == nil {
		return b
	}
	b := fetchCoverHiRes(title, artist)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if os.WriteFile(p+".tmp", b, 0o644) == nil {
		_ = os.Rename(p+".tmp", p)
	}
	pruneHiRes(300)
	return b
}

// pruneHiRes keeps the cache to the newest n covers (about 200 KB each).
func pruneHiRes(n int) {
	old, _ := filepath.Glob(filepath.Join(appDir(), "cache", "hires-*.jpg")) // 1.13's smaller ones
	for _, p := range old {
		_ = os.Remove(p)
	}
	pruneGlob("sq-*.jpg", 200) // Spotify queue pictures
	pruneGlob("hires2k-*.jpg", n)
}

func pruneGlob(pattern string, n int) {
	files, _ := filepath.Glob(filepath.Join(appDir(), "cache", pattern))
	if len(files) <= n {
		return
	}
	type fi struct {
		p string
		t time.Time
	}
	var all []fi
	for _, p := range files {
		if st, err := os.Stat(p); err == nil {
			all = append(all, fi{p, st.ModTime()})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
	for _, x := range all[min(n, len(all)):] {
		_ = os.Remove(x.p)
	}
}

var hiPrefetchKey string

func prefetchHiRes(t *Track) {
	if t == nil || !cfg.FsHiRes || t.Key == hiPrefetchKey || looksLikeID(t.Title) {
		return
	}
	hiPrefetchKey = t.Key
	key, title, artist := t.Key, t.Title, t.Artist
	go func() {
		defer func() { recover() }()
		time.Sleep(3 * time.Second) // the lyrics and the normal cover go first
		fetchHiResCached(key, title, artist)
	}()
}

// imageDiff: 0 = the same picture, 1 = opposite (compared at 8×8).
func imageDiff(a, b uintptr) float64 {
	A, B := newBitmap(8, 8), newBitmap(8, 8)
	defer A.Free()
	defer B.Free()
	if A == nil || B == nil {
		return 0
	}
	drawCoverSquare(A.g, a, 0, 0, 8)
	drawCoverSquare(B.g, b, 0, 0, 8)
	var d float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			p, q := pixel(A.bmp, x, y), pixel(B.bmp, x, y)
			for s := 0; s < 24; s += 8 {
				d += math.Abs(float64(p>>s&255) - float64(q>>s&255))
			}
		}
	}
	return d / (64 * 3 * 255)
}

func fmtTime(s float64) string {
	if s < 0 {
		s = 0
	}
	n := int(s)
	return fmt.Sprintf("%d:%02d", n/60, n%60)
}

// fs: the full-screen view while it is open (nil otherwise)
var fs *FullView

// wake: the mouse moved — show the cursor.
func (f *FullView) wake() {
	f.mouseAt = time.Now()
	if f.cursorOff {
		f.cursorOff = false
		showCursor(true)
	}
}
