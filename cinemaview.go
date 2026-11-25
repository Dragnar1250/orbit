//go:build windows || darwin

package main

// Cinema (1.20): the full-screen "now playing" view, rebuilt for calm and speed.
//
// Speed: an ordinary window (not a see-through layered one, which made Windows blend 15 MB on
// every frame), everything that stands still drawn ONCE into a composed picture (per song, per
// minute for the clock), and every frame only the parts that change — the lyrics, the time
// line, the controls — restored from that picture, redrawn and copied to the screen.
// Song changes cross-fade the two pictures for half a second on all cores.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

type cinBlock struct {
	b     lyrBlock
	words []string
	img   *Canvas // the line's words, white, drawn once (blended in Go: see blendWhite)
	pad   float64
}

type cinState struct {
	on                  bool
	bg, comp, old, buf  *Canvas
	tiny, mid           *Canvas
	scaleTmp            []uint32
	lay                 cinLayout
	art                 *Bitmap
	artSig              string
	bgSig, compSig      string
	compKey             string
	compBg              string  // the background the composed picture was made on
	fadeT               float64 // 0..1 while two songs cross-fade, -1 = no fade
	hover               int     // -1 none, 0..4 buttons, 5 time line, 6 up next
	drag                bool
	dragFrac            float64
	help                bool
	progSig, ctrlSig    string
	lyrSig              string
	stagePrev           rectF    // where the stage's lyrics were drawn last time
	bloom               []uint16 // the stage's glow, small (see bloomFrom)
	bloomW, bloomH      int
	bloomAt             int      // the lyrics frame the glow was made for
	bloomOn             float64  // strength (0 = the text effect isn't a glow)
	bloomGain           float64  // × the strength this frame (the Glow motion)
	mot                 float64  // the beat-driven motion, smoothed (0–1)
	motSx, motSy        float64  // Pulse / Stretch: the scale the words are laid down at this frame
	box                 [4]int   // where the stage's words are on their canvas (contentBox)
	boxAt               int      // the lyrics frame it was found for
	stageIdx            int      // the line the stage shows
	stageOld            []uint32 // the line before, fading out over the new one (plain memory, reused)
	stageOldW           int
	stageOldH           int
	stageOldAt          rectF
	stageOldT           float64 // 0..1 through its fade, ≥1 = none
	layCover            int     // the cover mode the layout was made for
	vinSig              string
	armT, armV, armLift float64 // the tonearm: 0 on its rest … 1 in the groove; how far it is lifted
	armAt               time.Time
	oldFree             time.Time // when the cross-fade picture can go
	warmed              string    // the next line, already got ready
	frameT0             time.Time // when this frame's work began
	noLyr               string    // "No lyrics for this song" / "Instrumental" (with lyrics on)
	over                *Canvas   // everything still that isn't the background (cover, titles…), see-through
	overSig, palSig     string
	flowAt              float64 // Flowing colours: when the background was last moved
	flowT               float64
	pal                 []uint32 // the cover's colours (Gradient, Flowing colours)
	noFade              bool     // the next new picture replaces the old one without a fade
	// lyrics
	lyrKey    string
	lyrSize   float64
	lyrW      float64
	lyrFont   string
	blocks    []cinBlock
	scroll    float64 // where block 0's top is now (glides to the target)
	scrollSet bool
	moving    bool
	// up next
	qKey string
	qAsk time.Time
	next *HistItem
	// timing
	frameMs float64
	logAt   time.Time
}

const (
	cinHoverNone = -1
	cinHoverProg = 5
	cinHoverNext = 6
)

func (f *FullView) cinemaOn() bool { return f.cin.on }

// cinemaAlloc makes the four screen-size pictures (background, composed, previous, frame).
func (f *FullView) cinemaAlloc() bool {
	c := &f.cin
	W, H := f.winW, f.winH
	c.bg, c.comp, c.buf = newCanvas(W, H), newCanvas(W, H), newCanvas(W, H) // (c.old: only while a cross-fade runs)
	c.tiny, c.mid, c.over = newCanvas(20, 12), newCanvas(160, 90), newCanvas(W, H)
	if c.bg == nil || c.comp == nil || c.buf == nil || c.tiny == nil || c.mid == nil || c.over == nil {
		f.cinemaFree()
		return false
	}
	for _, cv := range []*Canvas{c.comp, c.buf, c.over} {
		setTextHint(cv.g, 4) // smooth anti-aliasing for big text
	}
	f.W, f.H, f.q = W, H, 1
	c.on, c.fadeT, c.hover, c.stageOldT, c.stageIdx = true, -1, cinHoverNone, 1, -9
	return true
}

func (f *FullView) cinemaFree() {
	c := &f.cin
	for _, cv := range []*Canvas{c.bg, c.comp, c.old, c.buf, c.tiny, c.mid, c.over} {
		cv.Free()
	}
	c.bg, c.comp, c.old, c.buf, c.tiny, c.mid, c.over = nil, nil, nil, nil, nil, nil, nil
	c.overSig, c.palSig = "", ""
	c.art.Free()
	c.art = nil
	c.freeBlocks()
	c.stageOld, c.bloom = nil, nil
	flushTextPaths() // (full screen's words are big: give their outlines back)
	flushWordSprites()
	c.on = false
}

func (c *cinState) freeBlocks() {
	for i := range c.blocks {
		c.blocks[i].img.Free()
	}
	c.blocks, c.lyrKey = nil, ""
}

/* ---------- every frame ---------- */

// what each part of a frame costs (smoothed, ms): shown by Q, written by the test mode
var profNames = []string{"record", "stage", "still", "mix", "parts", "present"}
var prof [6]float64

// the last frames, one by one: how long each took, and how far apart they were
var frameRing, gapRing [240]float64
var ringN int
var lastFrameAt time.Time

func ringStats(r *[240]float64) (p50, p95, worst float64) {
	n := min(ringN, len(r))
	if n == 0 {
		return
	}
	v := append([]float64(nil), r[:n]...)
	sort.Float64s(v)
	return v[n/2], v[n*95/100], v[n-1]
}

func frameStats() string {
	a, b, c := ringStats(&frameRing)
	d, e, g := ringStats(&gapRing)
	return fmt.Sprintf("work %.1f/%.1f/%.1f ms · steps %.1f/%.1f/%.1f ms (median/95%%/worst)", a, b, c, d, e, g)
}

func profAdd(i int, since time.Time) time.Time {
	now := time.Now()
	ms := float64(now.Sub(since).Microseconds()) / 1000
	prof[i] += (ms - prof[i]) * 0.1
	return now
}

func profLine() string {
	var b strings.Builder
	for i, n := range profNames {
		fmt.Fprintf(&b, "%s %.1f  ", n, prof[i])
	}
	return b.String()
}

func (f *FullView) cinemaFrame(now time.Time, snap MediaSnap, dt float64, bands [nBands]float32, bass float32) {
	c := &f.cin
	t0 := time.Now()
	c.frameT0 = t0
	if snap.Version != lyricsVer { // the desktop lyrics rest while full screen is open
		lyricsVer = snap.Version
		lyrics.Load(snap.Track)
	}
	f.cinemaQueue(snap)
	key := ""
	if snap.Track != nil {
		key = snap.Track.Key
	}
	lkey, status, lines := lyrics.Get()
	lyrOn := cfg.FsLyrics && snap.Track != nil && lkey == key && (status == lyrSynced || status == lyrPlain) && len(lines) > 0
	// with lyrics on, the cover / record keeps its place on the left whatever the song has
	// (a song without lyrics used to move it to the middle — and it stayed there)
	layLyr := cfg.FsLyrics
	c.noLyr = ""
	if cfg.FsLyrics && snap.Track != nil && lkey == key {
		switch status {
		case lyrNotFound:
			c.noLyr = T("No lyrics for this song")
		case lyrInstrumental:
			c.noLyr = T("Instrumental")
		}
	}
	if c.lay.W != float64(f.winW) || c.lay.Lyrics != layLyr || c.layCover != cfg.FsCover {
		c.lay = cinemaLayout(float64(f.winW), float64(f.winH), layLyr, cfg.FsCover >= 1) // (the record gets the big room too)
		c.layCover = cfg.FsCover
		c.compSig = "" // (laid out again: the cross-fade below makes the move smooth)
	}
	img, hiOK := cover.img, false
	if f.hiImg != 0 && f.hiKey == cover.key && cover.key == key {
		img, hiOK = f.hiImg, true
	}
	if cover.key != key {
		img = 0
	}

	// 1. the background (once per song / cover)
	pt := time.Now()
	bgSig := fmt.Sprint(key, cover.ver, hiOK, f.winW, f.winH, img != 0, cfg.FsBg)
	if bgSig != c.bgSig {
		c.bgSig = bgSig
		f.cinemaBackground(img)
	} else if cfg.FsBg == cbFlow && snap.Playing && cfg.FsFPS > 0 && c.fadeT < 0 && f.t-c.flowAt >= 1.0/12 {
		// Flowing colours: the lights move on, 12 times a second, without a cross-fade
		c.flowT += f.t - c.flowAt
		c.flowAt = f.t
		f.cinemaBackground(img)
		c.compSig, c.noFade = "", true
	}
	// 2. the composed picture: everything that stands still
	nextSig := ""
	if c.next != nil {
		nextSig = c.next.Key + c.next.CoverFile
	}
	toast := ""
	if f.toast != "" && now.Sub(f.toastAt) < 2200*time.Millisecond {
		toast = f.toast
	}
	clock := ""
	if cfg.FsClock {
		clock = now.Format("2006-01-02 15:04")
	}
	title, artist, album := "", "", ""
	if snap.Track != nil {
		title, artist, album = snap.Track.Title, snap.Track.Artist, snap.Track.Album
	}
	compSig := fmt.Sprint(c.bgSig, "|", title, artist, album, "|", nextSig, "|", toast, "|", clock, "|", c.help, c.lay.Lyrics, c.noLyr, albumAccent, cfg.Clock24h, cfg.FsCover, langCode())
	full := false
	if compSig != c.compSig {
		// a new song, a sharper cover, lyrics arriving (the cover moves aside): cross-fade.
		// (The clock, a message or the help card just change.)
		changed := key != c.compKey || c.bgSig != c.compBg || c.compSig == ""
		if c.noFade {
			changed, c.noFade = false, false
		}
		if changed && c.comp != nil && c.compBg != "" && cfg.FsFPS > 0 {
			if c.old == nil { // a screen-sized picture kept only around a cross-fade (33 MB at 4K)
				if c.old = newCanvas(c.comp.w, c.comp.h); c.old != nil {
					setTextHint(c.old.g, 4)
				}
			}
			if c.old != nil {
				c.old, c.comp = c.comp, c.old // keep what is on screen: it fades into the new one
				c.fadeT = 0
			}
		}
		c.compSig, c.compKey, c.compBg = compSig, key, c.bgSig
		f.cinemaCompose(snap, img, toast, now)
		full = true
	}
	pt = profAdd(2, pt)

	// 3. the parts that change
	frac, elapsed, dur := 0.0, 0.0, 0.0
	if snap.Track != nil && snap.Track.Duration > 0 {
		dur = snap.Track.Duration
		elapsed = math.Max(0, math.Min(dur, snap.Pos))
		frac = elapsed / dur
	}
	if c.drag {
		frac = c.dragFrac
		elapsed = frac * dur
	}
	progSig := fmt.Sprint(int(frac*c.lay.Prog.W), int(elapsed), int(dur), c.hover == cinHoverProg, c.drag, c.help)
	shuf, rep, canShuf, canRep := f.modes(snap)
	ctrlSig := fmt.Sprint(c.hover, snap.Playing, shuf, rep, canShuf, canRep, c.help, snap.Track != nil)
	lyrState := f.cinemaLyricsState(lines, lyrOn && cfg.FsStage == 0, snap, dt)
	lyrSig := lyrState.sig
	pt = time.Now()
	vinSig := f.cinemaVinyl(now, snap, bands, bass)
	pt = profAdd(0, pt)
	if cfg.FsStage != 0 {
		lyrSig = "stage " + f.cinemaStage(now, snap, bass, lines, dt) + fmt.Sprint(c.help)
	}
	pt = profAdd(1, pt)

	var dirty []rectF
	if c.old != nil && c.fadeT < 0 && !c.oldFree.IsZero() && now.After(c.oldFree) {
		c.old.Free()
		c.old, c.oldFree = nil, time.Time{}
	}
	if c.fadeT >= 0 {
		c.fadeT += math.Min(dt, 1.0/30) / 0.55 // (a slow frame — making the new picture — doesn't skip ahead)
		t := smooth(c.fadeT)
		if c.fadeT >= 1 {
			c.fadeT, t = -1, 1
			c.oldFree = now.Add(6 * time.Second) // let it go once things are calm
		}
		mixFrames(pix(c.buf), pix(c.old), pix(c.comp), t)
		full = true
	} else if full {
		copy(pix(c.buf), pix(c.comp))
	}
	pt = profAdd(3, pt)
	if full {
		f.cinemaProgress(frac, elapsed, dur)
		f.cinemaControls(snap)
		f.cinemaLyrics(lines, lyrState)
		c.stagePrev = f.stageRect()
		f.cinemaVinylDraw()
		c.progSig, c.ctrlSig, c.lyrSig, c.vinSig = progSig, ctrlSig, lyrSig, vinSig
	} else {
		if progSig != c.progSig {
			c.progSig = progSig
			r := f.cinemaProgRect()
			f.cinemaRestore(r)
			f.cinemaProgress(frac, elapsed, dur)
			dirty = append(dirty, r)
		}
		if ctrlSig != c.ctrlSig {
			c.ctrlSig = ctrlSig
			r := c.lay.Ctrl
			f.cinemaRestore(r)
			f.cinemaControls(snap)
			dirty = append(dirty, r)
		}
		if vinSig != c.vinSig {
			c.vinSig = vinSig
			if r, _, _ := f.vinylRect(); r.W > 0 && r.H > 0 {
				secProf("")
				f.cinemaRestore(r)
				secProf("p.vinRestore")
				f.cinemaVinylDraw()
				secProf("p.vinDraw")
				dirty = append(dirty, r)
			}
		}
		if lyrSig != c.lyrSig && c.lay.Lyrics {
			c.lyrSig = lyrSig
			r := c.lay.Lyr
			if cfg.FsStage != 0 && !c.help { // only where the words were and are now
				cur := f.stageRect()
				r = unionRect(c.stagePrev, cur)
				c.stagePrev = cur
			}
			if r.W > 0 && r.H > 0 {
				secProf("")
				f.cinemaRestore(r)
				secProf("p.lyrRestore")
				f.cinemaLyrics(lines, lyrState)
				secProf("p.lyrDraw")
				dirty = append(dirty, r)
			}
		}
	}
	pt = profAdd(4, pt)
	secProf("")
	f.cinemaPresent(full, dirty)
	secProf("present")
	profAdd(5, pt)

	// frames: 60 while something glides, 30 while the song plays (words fill), few when still
	fps := 30
	switch {
	case c.fadeT >= 0 || c.moving || c.drag || c.stageOldT < 1 || (f.vin != nil && f.vin.swapT >= 0):
		fps = 60
	case !snap.Playing && now.Sub(f.mouseAt) > 3*time.Second && toast == "":
		fps = 6
	}
	if cfg.FsFPS > fps && fps > 6 {
		fps = cfg.FsFPS
	}
	setFPS(fps)
	ms := float64(time.Since(t0).Microseconds()) / 1000
	c.frameMs += (ms - c.frameMs) * 0.1
	frameRing[ringN%len(frameRing)] = ms
	if !lastFrameAt.IsZero() {
		gapRing[ringN%len(gapRing)] = float64(now.Sub(lastFrameAt).Microseconds()) / 1000
	}
	lastFrameAt = now
	ringN++
	f.frameMs = c.frameMs
	if now.Sub(c.logAt) > 10*time.Second {
		c.logAt = now
		logf("cinema %dx%d: %.1f ms a frame, refresh %.2f ms (%s)", f.winW, f.winH, c.frameMs, float64(refreshNs.Load())/1e6, []string{"clock", "screen", "compositor"}[vsyncMode.Load()])
	}
}

// cinemaQueue: what plays next (Spotify), asked when the song changes and every 20 s.
func (f *FullView) cinemaQueue(snap MediaSnap) {
	c := &f.cin
	c.next = nil
	if snap.Track == nil {
		return
	}
	if f.spotifyOn() && (c.qKey != snap.Track.Key || time.Since(c.qAsk) > 20*time.Second) {
		c.qKey, c.qAsk = snap.Track.Key, time.Now()
		spotify.refreshQueue()
	}
	if n := f.nextSongs(snap); len(n) > 0 {
		it := n[0]
		c.next = &it
	}
}

// setFPS asks the frame clock for this many frames a second.
func setFPS(n int) {
	if n < 1 {
		n = 1
	}
	atomic.StoreInt32(&fpsNow, int32(n))
}

/* ---------- drawing the still parts ---------- */

// cinemaBackground paints the background (once per song, or a few times a second for Flowing
// colours). The smooth ones are made at 160×90 — darkening included — and enlarged softly:
// a few milliseconds even on a 4K screen. "Cover" is drawn at full size.
func (f *FullView) cinemaBackground(img uintptr) {
	c := &f.cin
	W, H := float32(f.winW), float32(f.winH)
	t, m := c.tiny, c.mid
	// the cover in a few pixels: its colours
	if c.palSig != c.bgSig || len(c.pal) == 0 { // (once per cover, not on every Flowing step)
		c.palSig = c.bgSig
		gClear(t.g, 0xFF0C0C10)
		if img != 0 {
			setInterp(t.g, interpHighQualityBicubic)
			tw, th := float32(t.w), float32(t.h)
			s := float32(math.Max(float64(tw), float64(th))) * 1.25 // fill, a little zoomed in
			drawCoverSquare(t.g, img, (tw-s)/2, (th-s)/2, s)
			c.pal = paletteFromPixels(pix(t), 4)
		} else {
			c.pal = []uint32{0xFF2A2A36}
		}
	}
	deep := func(col uint32, k float64) uint32 { // more colourful and darker
		r, g, b := float64(col>>16&255), float64(col>>8&255), float64(col&255)
		l := 0.299*r + 0.587*g + 0.114*b
		r, g, b = (l+(r-l)*1.5)*k, (l+(g-l)*1.5)*k, (l+(b-l)*1.5)*k
		return 0xFF000000 | uint32(clampByte(int(r)))<<16 | uint32(clampByte(int(g)))<<8 | uint32(clampByte(int(b)))
	}
	pal := func(i int) uint32 { return c.pal[i%len(c.pal)] | 0xFF000000 }
	mw, mh := float32(m.w), float32(m.h)
	mg := m.g
	shade := func(g uintptr, w, h float32, k float64) { // darker where the text sits, top and bottom fall off
		fillRectGrad(g, 0, 0, w*0.7, h, 0, 0, w*0.7, 0, withAlpha(0xFF000000, 0.55*k), 0x00000000)
		fillRectGrad(g, 0, h*0.55, w, h*0.45+1, 0, h*0.55, 0, h, 0x00000000, withAlpha(0xFF000000, 0.6*k))
		fillRectGrad(g, 0, 0, w, h*0.2, 0, 0, 0, h*0.2, withAlpha(0xFF000000, 0.4*k), 0x00000000)
	}
	if cfg.FsBg == cbCover && img != 0 { // the cover itself, across the screen, darkened
		// Drawn straight at the screen's full size (as in 1.22.4), with high-quality bicubic from
		// the sharp 2000 px cover when there is one: no smaller picture enlarged, no blur.
		g := c.bg.g
		gClear(g, 0xFF000000)
		setInterp(g, interpHighQualityBicubic)
		setPixelOffset(g, pixelOffsetHalf) // half-pixel: no dark seam at the edges
		s := float32(math.Max(float64(W), float64(H)))
		drawCoverSquare(g, img, (W-s)/2, (H-s)/2, s)
		setPixelOffset(g, 0)
		setInterp(g, interpBilinear)
		fillRect(g, 0x8C000000, 0, 0, W, H)
		shade(g, W, H, 1)
		grain(pix(c.bg), 2)
		return
	}
	setCopyMode(mg, true)
	switch cfg.FsBg {
	case cbGradient: // two of the cover's colours, corner to corner, a soft light between
		a, b := deep(pal(0), 0.55), deep(pal(1), 0.32)
		fillRectGrad(mg, 0, 0, mw, mh, 0, 0, mw, mh, a, b)
		setCopyMode(mg, false)
		softGlow(mg, mw*0.72, mh*0.3, mh*0.9, withAlpha(deep(pal(2), 0.8), 0.35))
	case cbDark: // near black, the album's colour glowing faintly at the edges
		fillRect(mg, 0xFF07070A, 0, 0, mw, mh)
		setCopyMode(mg, false)
		softGlow(mg, mw*0.82, mh*0.18, mh*0.95, withAlpha(deep(pal(0), 0.9), 0.22))
		softGlow(mg, mw*0.08, mh*0.95, mh*0.8, withAlpha(deep(pal(1), 0.9), 0.14))
	case cbFlow: // the cover's colours drifting: soft lights moving on slow paths
		fillRect(mg, deep(pal(0), 0.16), 0, 0, mw, mh)
		setCopyMode(mg, false)
		tt := c.flowT
		for i := 0; i < 4; i++ {
			fi := float64(i)
			cx := mw * float32(0.5+0.38*math.Sin(tt*(0.05+0.017*fi)+fi*1.7))
			cy := mh * float32(0.5+0.36*math.Cos(tt*(0.043+0.013*fi)+fi*2.3))
			softGlow(mg, cx, cy, mh*float32(0.75+0.15*math.Sin(tt*0.03+fi)), withAlpha(deep(pal(i), 0.75), 0.8))
		}
	default: // Cover colours: the cover blurred into soft light
		setCopyMode(mg, true)
		setInterp(mg, interpHighQualityBicubic)
		ex, ey := mw/float32(t.w), mh/float32(t.h)
		gImage(mg, t.bmp, -ex, -ey, mw+2*ex, mh+2*ey)
		setCopyMode(mg, false)
		mp := pix(m)
		for i, p := range mp {
			mp[i] = deep(p|0xFF000000, 0.62)
		}
	}
	setCopyMode(mg, false)
	shade(mg, mw, mh, 1)
	mp := pix(m)
	for i, p := range mp { // (opaque everywhere)
		mp[i] = p | 0xFF000000
	}
	scaleSmooth(pix(c.bg), c.bg.w, c.bg.h, mp, m.w, m.h, &c.scaleTmp)
	if cfg.FsBg != cbFlow { // (moving 12 times a second, the scaling's own dither is enough)
		grain(pix(c.bg), 2)
	}
}

// cinemaArt: the cover with rounded corners, made once per cover and size.
func (f *FullView) cinemaArt(img uintptr) *Bitmap {
	c := &f.cin
	A := int(c.lay.Art.W)
	sig := fmt.Sprint(c.bgSig, A)
	if c.art != nil && c.artSig == sig {
		return c.art
	}
	c.art.Free()
	c.art, c.artSig = newBitmap(A, A), sig
	if c.art == nil {
		return nil
	}
	g := c.art.g
	gClear(g, 0)
	setInterp(g, interpHighQualityBicubic)
	a := float32(A)
	path := roundRectPath(0, 0, a, a, a*0.028)
	if img != 0 { // smooth round corners, then a touch of sharpening (it was made smaller or larger)
		fillPathCover(g, img, 0, 0, a, path)
		c.art.edit(func(px []uint32, w, h int) { sharpen(px, w, h, 0.45) })
	} else {
		clipPath(g, path)
		fillRectGrad(g, 0, 0, a, a, 0, 0, a, a, 0xFF2A2A33, 0xFF15151A)
		fn := font("Segoe UI Symbol", float64(a*0.22), false)
		tw, th := measure(fn, "♪")
		drawText(g, fn, "♪", (a-tw)/2, (a-th)/2, tw+4, 0x66FFFFFF)
		clipReset(g)
	}
	strokePath(g, path, 0x1CFFFFFF, 1)
	freePath(path)
	return c.art
}

func cinTint() uint32 {
	t := accent() | 0xFF000000
	if albumAccent != 0 {
		t = albumAccent | 0xFF000000
	}
	return softTint(t)
}

// cinemaCompose: background + cover + song text + up next + clock (+ toast / help card).
func (f *FullView) cinemaCompose(snap MediaSnap, img uintptr, toast string, now time.Time) {
	c := &f.cin
	l := c.lay
	U := float32(l.U)
	// the background, then the still parts over it — drawn into their own layer only when they
	// change (Flowing colours moves the background 12 times a second: then it is just laid over)
	defer func() {
		copy(pix(c.comp), pix(c.bg))
		gFlush(c.over.g)
		overBlit(pix(c.comp), c.comp.w, c.comp.h, pix(c.over), c.over.w, c.over.h, 0, 0, [4]int{0, 0, c.comp.w, c.comp.h}, 255)
	}()
	if c.overSig == c.compSig && c.compSig != "" {
		return
	}
	c.overSig = c.compSig
	g := c.over.g
	gClear(g, 0)
	tint := cinTint()
	A := float32(l.Art.W)
	ax, ay := float32(l.Art.X), float32(l.Art.Y)

	// the cover: its own coloured light, a deep soft shadow, then the picture
	softGlow(g, ax+A/2, ay+A*0.55, A*0.95, withAlpha(tint, 0.2))
	if cfg.FsCover == 2 { // the record: it rests on a soft shadow of its own (it is drawn over this)
		rcx, rcy, rR := f.recGeom()
		softGlow(g, float32(rcx), float32(rcy+rR*0.08), float32(rR*1.2), 0xA6000000)
		softGlow(g, float32(rcx), float32(rcy+rR*0.03), float32(rR*1.04), 0x66000000)
	}
	for i := 0; i < 12 && cfg.FsCover != 2; i++ {
		e := A * 0.012 * float32(i)
		sh := roundRectPath(ax-e, ay+A*0.035-e*0.6, A+2*e, A+2*e, A*0.028+e)
		fillPathColor(g, sh, 0x0D000000)
		freePath(sh)
	}
	if art := f.cinemaArt(img); art != nil && cfg.FsCover != 2 {
		gImage(g, art.bmp, ax, ay, A, A)
	}

	// the song
	title, artist, album := T("Nothing playing"), T("Play something on Spotify"), ""
	if snap.Track != nil {
		title, artist, album = snap.Track.Title, snap.Track.Artist, snap.Track.Album
	}
	fT := font("Segoe UI Semibold", float64(30*U), false)
	fA := font("Segoe UI", float64(21*U), false)
	fB := font("Segoe UI", float64(16*U), false)
	put := func(fn *Font, s string, y float32, col uint32) {
		if s == "" {
			return
		}
		x := float32(l.Text.X)
		if l.Centred {
			w, _ := measure(fn, s)
			w = float32(math.Min(float64(w), float64(A)))
			x = ax + (A-w)/2
		}
		drawText(g, fn, s, x, y, A+U*4, col)
	}
	put(fT, title, float32(l.Title), 0xFFFFFFFF)
	put(fA, artist, float32(l.Artist), 0xB8FFFFFF)
	if album != "" && album != title {
		put(fB, album, float32(l.Album), 0x70FFFFFF)
	}
	if c.noLyr != "" && l.Lyrics { // quietly, in the middle of the stage
		fM := font("Segoe UI", float64(30*U), false)
		w, h := measure(fM, c.noLyr)
		drawText(g, fM, "♪", float32(l.Lyr.X+l.Lyr.W/2)-14*U, float32(l.Lyr.Y+l.Lyr.H*0.42)-h*1.5, 40*U, 0x50FFFFFF)
		drawText(g, fM, c.noLyr, float32(l.Lyr.X+l.Lyr.W/2)-w/2, float32(l.Lyr.Y+l.Lyr.H*0.42), w+4*U, 0x60FFFFFF)
	}

	// up next
	if c.next != nil {
		n := c.next
		y := float32(l.Next.Y)
		cs := 34 * U
		lab := font("Segoe UI Semibold", float64(11*U), false)
		fN := font("Segoe UI Semibold", float64(16*U), false)
		fN2 := font("Segoe UI", float64(14*U), false)
		x := float32(l.Next.X)
		textW := A - cs - 14*U
		if l.Centred {
			w1, _ := measure(fN, n.Title)
			w2, _ := measure(fN2, n.Artist)
			tw := float32(math.Min(float64(textW), math.Max(float64(w1), float64(w2))))
			x = ax + (A-(cs+14*U+tw))/2
		}
		path := roundRectPath(x, y+4*U, cs, cs, cs*0.14)
		fillPathColor(g, path, 0xFF26262B)
		if im := historyImage(*n); im != 0 {
			fillPathCover(g, im, x, y+4*U, cs, path)
		}
		freePath(path)
		tx := x + cs + 14*U
		drawText(g, lab, T("UP NEXT"), tx, y-1*U, textW, withAlpha(tint, 0.95))
		drawText(g, fN, n.Title, tx, y+11*U, textW, 0xE6FFFFFF)
		tw, _ := measure(fN, n.Title)
		if tw+12*U < textW {
			drawText(g, fN2, "· "+n.Artist, tx+tw+8*U, y+13*U, textW-tw-8*U, 0x80FFFFFF)
		}
	}

	// the clock, top right
	if cfg.FsClock {
		r := l.Clock
		fC := font("Segoe UI Semibold", float64(30*U), false)
		fD := font("Segoe UI", float64(19*U), false)
		tm := now.Format("15:04")
		if !cfg.Clock24h {
			tm = now.Format("3:04")
		}
		tw, _ := measure(fC, tm)
		x := float32(r.right()) - tw
		drawText(g, fC, tm, x, float32(r.Y), tw+8*U, 0xE6FFFFFF)
		d := formatDate(now, false)
		dw, _ := measure(fD, d)
		drawText(g, fD, d, x-dw-16*U, float32(r.Y)+9*U, dw+8*U, 0x8CFFFFFF)
	}

	if toast != "" {
		f.cinemaPill(g, toast)
	}
	if c.help {
		f.cinemaHelp(g)
	}
}

// cinemaPill: a short message, top centre, on dark glass.
func (f *FullView) cinemaPill(g uintptr, s string) {
	l := f.cin.lay
	U := float32(l.U)
	fn := font("Segoe UI", float64(19*U), false)
	tw, th := measure(fn, s)
	w, h := tw+44*U, th+18*U
	x, y := float32(l.W)/2-w/2, float32(l.Toast.Y)
	p := roundRectPath(x, y, w, h, h/2)
	fillPathColor(g, p, 0xB3141418)
	strokePath(g, p, 0x26FFFFFF, 1)
	freePath(p)
	drawText(g, fn, s, x+22*U, y+9*U, tw+8*U, 0xF0FFFFFF)
}

// cinemaHelp: the keys, on a glass card in the middle (H).
func (f *FullView) cinemaHelp(g uintptr) {
	l := f.cin.lay
	U := float32(l.U)
	W, H := float32(l.W), float32(l.H)
	fillRect(g, 0x73000000, 0, 0, W, H)
	rows := [][2]string{{"Space", "Play / pause"}, {"← →", "Lyrics effect"}, {"Ctrl ← →", "Previous / next song"}, {"↑ ↓", "Lyrics bigger / smaller"},
		{"E · T", "Text effect · colours"}, {"L", "Lyrics on / off"}, {"B", "Background"}, {"V", "Cover or record"},
		{"R", "The record's look"}, {"A", "Tonearm colour"}, {"C", "Clock on / off"}, {"S", "Settings"}, {"Esc", "Leave full screen"}, {"Q", "Frame time"}}
	fk := font("Segoe UI Semibold", float64(19*U), false)
	fv := font("Segoe UI", float64(19*U), false)
	// each column as wide as its longest row (the words differ by language)
	var colW [2]float32
	kws := make([]float32, len(rows))
	for i := range rows {
		rows[i][0], rows[i][1] = T(rows[i][0]), T(rows[i][1])
		kw, _ := measure(fk, rows[i][0])
		vw, _ := measure(fv, rows[i][1])
		kws[i] = kw
		colW[i/7] = max(colW[i/7], kw+44*U+vw)
	}
	gap := 48 * U
	cw := max(820*U, 96*U+colW[0]+gap+colW[1])
	if cw > W-40*U { // too wide for the screen: smaller words
		k := (W - 40*U) / cw
		fk = font("Segoe UI Semibold", float64(19*U*k), false)
		fv = font("Segoe UI", float64(19*U*k), false)
		for i := range kws {
			kws[i] *= k
		}
		colW[0] *= k
		gap *= k
		cw = W - 40*U
	}
	ch := 600 * U
	x, y := (W-cw)/2, (H-ch)/2
	p := roundRectPath(x, y, cw, ch, 28*U)
	fillPathColor(g, p, 0xE6151519)
	strokePath(g, p, 0x22FFFFFF, 1)
	freePath(p)
	drawText(g, font("Segoe UI Semibold", float64(30*U), false), T("Shortcuts"), x+48*U, y+38*U, cw, 0xFFFFFFFF)
	for i, r := range rows {
		col, row := i/7, i%7 // two columns of seven
		rx := x + 48*U + float32(col)*(colW[0]+gap)
		ry := y + 108*U + float32(row)*58*U
		kw := kws[i]
		kp := roundRectPath(rx, ry, kw+26*U, 38*U, 9*U)
		fillPathColor(g, kp, 0x1FFFFFFF)
		freePath(kp)
		drawText(g, fk, r[0], rx+13*U, ry+6*U, kw+8*U, 0xFFFFFFFF)
		drawText(g, fv, r[1], rx+kw+44*U, ry+6*U, cw, 0xB3FFFFFF)
	}
	drawText(g, fv, T("Press H again to close"), x+48*U, y+ch-58*U, cw, 0x66FFFFFF)
}

/* ---------- drawing the changing parts ---------- */

func (f *FullView) cinemaProgRect() rectF {
	l := f.cin.lay
	return rectF{l.Prog.X - 14*l.U, l.Prog.Y - 16*l.U, l.Prog.W + 28*l.U, (l.Times + 30*l.U) - (l.Prog.Y - 16*l.U)}
}

// cinemaRestore puts the still picture back under a region before it is drawn again.
func (f *FullView) cinemaRestore(r rectF) {
	c := &f.cin
	x0, y0 := int(math.Max(0, math.Floor(r.X))), int(math.Max(0, math.Floor(r.Y)))
	x1, y1 := int(math.Min(float64(c.buf.w), math.Ceil(r.right()))), int(math.Min(float64(c.buf.h), math.Ceil(r.bottom())))
	if x1 <= x0 || y1 <= y0 {
		return
	}
	src, dst := pix(c.comp), pix(c.buf)
	w := c.buf.w
	for y := y0; y < y1; y++ {
		copy(dst[y*w+x0:y*w+x1], src[y*w+x0:y*w+x1])
	}
}

func (f *FullView) cinemaProgress(frac, elapsed, dur float64) {
	c := &f.cin
	l := c.lay
	if c.help {
		return
	}
	g := c.buf.g
	U := float32(l.U)
	p := l.Prog
	hot := c.hover == cinHoverProg || c.drag
	h := float32(math.Max(3, float64(5*U)))
	y := float32(p.Y+p.H/2) - h/2
	if hot {
		y -= h * 0.2
		h *= 1.4
	}
	x, w := float32(p.X), float32(p.W)
	// the track: a soft groove
	track := roundRectPath(x, y, w, h, h/2)
	fillPathColor(g, track, 0x26FFFFFF)
	freePath(track)
	kx := x + w*float32(frac)
	if fw := kx - x; fw > 0.5 {
		fill := roundRectPath(x, y, float32(math.Max(float64(fw), float64(h))), h, h/2)
		fillPathColor(g, fill, 0xEBFFFFFF)
		freePath(fill)
	}
	// the knob: small, a little bigger under the mouse
	kr := h * 1.05
	if hot {
		kr = h * 1.4
	}
	fillCircle(g, 0x40000000, kx, y+h/2+kr*0.15, kr*1.05)
	fillCircle(g, 0xFFFFFFFF, kx, y+h/2, kr)
	ft := font("Segoe UI Semibold", float64(14*U), false)
	if dur > 0 {
		ty := float32(l.Times) + 4*U
		drawText(g, ft, fmtTime(elapsed), x, ty, 120*U, 0xB8FFFFFF)
		rem := "-" + fmtTime(dur-elapsed)
		tw, _ := measure(ft, rem)
		drawText(g, ft, rem, x+w-tw, ty, tw+6*U, 0x80FFFFFF)
	}
}

// cinemaControls: shuffle · previous · play / pause · next · repeat — simple: drawn icons of
// one weight, the play button a white disc.
func (f *FullView) cinemaControls(snap MediaSnap) {
	c := &f.cin
	if c.help {
		return
	}
	g := c.buf.g
	tint := cinTint()
	shuf, rep, canShuf, canRep := f.modes(snap)
	bs := c.lay.Buttons
	cr := c.lay.Ctrl
	clipRect(g, float32(cr.X), float32(cr.Y), float32(cr.W), float32(cr.H))
	defer clipReset(g)
	white := uint32(0xF2FFFFFF)
	for i, b := range bs {
		cx, cy := float32(b.X+b.W/2), float32(b.Y+b.H/2)
		s := float32(b.W)
		hot := c.hover == i
		if i == 2 { // play / pause: a plain white disc, the icon in the album's darkest colour
			r := s / 2 * 0.9
			if hot {
				r = s / 2 * 0.96
			}
			fillCircle(g, 0x40000000, cx, cy+r*0.07, r*1.02)
			fillCircle(g, 0xFFFFFFFF, cx, cy, r)
			ink := mixColor(tint, 0xFF000000, 0.8) | 0xFF000000
			if snap.Playing {
				bw, bh := s*0.09, s*0.32
				for _, dx := range []float32{-s * 0.09, s * 0.09} {
					p := roundRectPath(cx+dx-bw/2, cy-bh/2, bw, bh, bw*0.4)
					fillPathColor(g, p, ink)
					freePath(p)
				}
			} else {
				t := s * 0.17
				tri := polylinePath([]PointF{{cx - t*0.62, cy - t}, {cx + t*1.05, cy}, {cx - t*0.62, cy + t}})
				closeFigure(tri)
				fillPathColor(g, tri, ink)
				strokePath(g, tri, ink, t*0.28) // (rounded corners)
				freePath(tri)
			}
			continue
		}
		if hot {
			fillCircle(g, 0x1AFFFFFF, cx, cy, s/2*0.92)
		}
		switch i {
		case 1, 3: // previous / next: two rounded triangles and a bar
			d := float32(1)
			if i == 1 {
				d = -1
			}
			t := s * 0.16
			for _, o := range []float32{-t * 0.52, t * 0.52} {
				x0 := cx + o*d
				tri := polylinePath([]PointF{{x0 - t*0.5*d, cy - t*0.66}, {x0 + t*0.5*d, cy}, {x0 - t*0.5*d, cy + t*0.66}})
				closeFigure(tri)
				fillPathColor(g, tri, white)
				strokePath(g, tri, white, t*0.22)
				freePath(tri)
			}
			line(g, white, s*0.05, cx+t*1.12*d, cy-t*0.62, cx+t*1.12*d, cy+t*0.62)
		case 0, 4: // shuffle / repeat, drawn: the same weight as everything else
			on, can := shuf == 1, canShuf
			if i == 4 {
				on, can = rep > 0, canRep
			}
			col := uint32(0xA6FFFFFF)
			if on {
				col = mixColor(tint, 0xFFFFFFFF, 0.3) | 0xFF000000
			}
			if !can {
				col = 0x33FFFFFF
			}
			sw := s * 0.05
			P := func(x, y float32) PointF { return PointF{cx + x*s, cy + y*s} }
			arrow := func(tip PointF, dir float32) { // a small head pointing left (−1) or right (+1)
				a := s * 0.075
				fillPoly(g, col, []PointF{{tip.X, tip.Y}, {tip.X - dir*a, tip.Y - a*0.85}, {tip.X - dir*a, tip.Y + a*0.85}})
			}
			if i == 0 {
				for _, sy := range []float32{1, -1} {
					pp := polylinePath([]PointF{P(-0.27, -0.12*sy), P(-0.12, -0.12*sy), P(0.09, 0.12*sy), P(0.2, 0.12*sy)})
					strokePath(g, pp, col, sw)
					freePath(pp)
					arrow(P(0.28, 0.12*sy), 1)
				}
			} else {
				loop := roundRectPath(cx-0.25*s, cy-0.13*s, 0.5*s, 0.26*s, 0.13*s)
				strokePath(g, loop, col, sw)
				freePath(loop)
				arrow(P(0.12, -0.13), 1)
				arrow(P(-0.12, 0.13), -1)
				if rep == 1 { // repeat this song: a little 1 inside
					f1 := font("Segoe UI Semibold", float64(s*0.2), false)
					tw, th := measure(f1, "1")
					drawText(g, f1, "1", cx-tw/2, cy-th/2, tw+4, col)
				}
			}
			if on {
				fillCircle(g, col, cx, cy+s*0.34, s*0.035)
			}
		}
	}
}

/* ---------- lyrics ---------- */

type cinLyr struct {
	sig      string
	cur      int     // the line being sung (or about to be, in a pause)
	centre   int     // the line held in the middle
	sungTo   int     // words of cur already sung
	part     float64 // how much of the word being sung is sung
	dots     float64
	dotsOn   bool
	dotsLine int
	lead     float64
}

// cinemaLyricsState works out what the lyrics show now and glides the scroll.
func (f *FullView) cinemaLyricsState(lines []Line, on bool, snap MediaSnap, dt float64) cinLyr {
	c := &f.cin
	st := cinLyr{cur: -1, centre: 0}
	c.moving = false
	if !on || !c.lay.Lyrics {
		return st
	}
	l := c.lay
	size := float64(cfg.FsTextSize) * l.U * 0.72
	lkey, _, _ := lyrics.Get()
	if lkey != c.lyrKey || size != c.lyrSize || l.Lyr.W != c.lyrW || len(c.blocks) != len(lines) || cfg.FsFont+fmt.Sprint(cfg.FsBold) != c.lyrFont {
		c.lyrFont = cfg.FsFont + fmt.Sprint(cfg.FsBold)
		c.freeBlocks()
		c.lyrKey, c.lyrSize, c.lyrW, c.scrollSet = lkey, size, l.Lyr.W, false
		fn := font(cfg.FsFont, size, cfg.FsBold)
		sp, _ := measure(fn, " ")
		c.blocks = make([]cinBlock, len(lines))
		for i, L := range lines {
			ws := lineWords(L)
			wd := make([]float64, len(ws))
			for k, w := range ws {
				m, _ := measure(fn, w)
				wd[k] = float64(m)
			}
			c.blocks[i] = cinBlock{b: wrapBlock(wd, float64(sp), l.Lyr.W, size*1.2), words: ws, pad: size * 0.25}
			if len(ws) == 0 {
				c.blocks[i].b.H = size * 0.4
			}
		}
	}
	p := snap.Pos + 0.08 // the words light up on the beat, not a moment after
	cur := lineAt(lines, p)
	st.cur, st.centre = cur, cur
	// pauses: before the first line, and long gaps after a line has been sung
	if cur < 0 {
		if pr, ok := gapDots(p, 0, lines[0].T); ok {
			st.dots, st.dotsOn, st.dotsLine = pr, true, 0
		}
		st.centre = 0
	} else if cur+1 < len(lines) {
		end := lines[cur].T + math.Max(1.2, lines[cur].Sing) + 0.4
		if pr, ok := gapDots(p, end, lines[cur+1].T); ok {
			st.dots, st.dotsOn, st.dotsLine = pr, true, cur+1
			st.centre = cur + 1
		}
	}
	if cur >= 0 {
		L := &lines[cur]
		if wi := wordAt(L, p); wi >= 0 {
			st.sungTo = wi
			st.part = math.Max(0, math.Min(1, (p-L.Words[wi].T)/math.Max(0.12, wordDur(L, wi))))
		}
		if len(L.Words) == 0 {
			st.sungTo, st.part = 0, 1
		}
	}
	// the scroll glides to the line in the middle
	hs := make([]float64, len(c.blocks))
	for i := range c.blocks {
		hs[i] = c.blocks[i].b.H
	}
	ys := lyrOffsets(hs, st.centre, size*0.7, l.LyrMid)
	if len(ys) > 0 {
		if !c.scrollSet {
			c.scroll, c.scrollSet = ys[0], true
		}
		was := c.scroll
		c.scroll = springTo(c.scroll, ys[0], dt)
		c.moving = c.scroll != was
	}
	partPx := 0
	if st.cur >= 0 && st.cur < len(c.blocks) && st.sungTo < len(c.blocks[st.cur].b.Wd) {
		partPx = int(st.part * c.blocks[st.cur].b.Wd[st.sungTo])
	}
	st.sig = fmt.Sprint(st.cur, st.centre, st.sungTo, partPx, int(math.Round(c.scroll)), st.dotsOn, int(st.dots*30), c.help, size)
	return st
}

// lineWords: the words of a line as shown (the line's own words when it has timings).
func lineWords(L Line) []string {
	if len(L.Words) > 0 {
		ws := make([]string, len(L.Words))
		for i, w := range L.Words {
			ws[i] = w.W
		}
		return ws
	}
	return strings.Fields(L.Text)
}

// blockImage: a lyric line drawn once, white, into its own picture.
func (f *FullView) blockImage(i int) *cinBlock {
	c := &f.cin
	bk := &c.blocks[i]
	if bk.img != nil || len(bk.words) == 0 {
		return bk
	}
	pad := bk.pad
	lineH := c.lyrSize * 1.2
	w := int(c.lyrW + 2*pad)
	h := int(bk.b.H + 2*pad)
	bk.img = newCanvas(w, h)
	if bk.img == nil {
		return bk
	}
	g := bk.img.g
	gClear(g, 0)
	setTextHint(g, 4)
	fn := font(cfg.FsFont, c.lyrSize, cfg.FsBold)
	for k, word := range bk.words {
		x := float32(pad + bk.b.X[k])
		y := float32(pad + float64(bk.b.Row[k])*lineH)
		drawText(g, fn, word, x, y, float32(bk.b.Wd[k])+float32(c.lyrSize), 0xFFFFFFFF)
	}
	return bk
}

func (f *FullView) cinemaLyrics(lines []Line, st cinLyr) {
	c := &f.cin
	l := c.lay
	if !l.Lyrics || c.help {
		return
	}
	if cfg.FsStage != 0 {
		f.cinemaStageDraw()
		return
	}
	if len(c.blocks) == 0 {
		return
	}
	dst := pix(c.buf)
	dw, dh := c.buf.w, c.buf.h
	clip := [4]int{int(l.Lyr.X), int(l.Lyr.Y), int(l.Lyr.right()), int(l.Lyr.bottom())}
	gap := c.lyrSize * 0.7
	lineH := c.lyrSize * 1.2
	y := c.scroll
	for i := range c.blocks {
		h := c.blocks[i].b.H
		top := y
		y += h + gap
		if top+h < l.Lyr.Y-lineH || top > l.Lyr.bottom()+lineH {
			continue
		}
		if st.dotsOn && i == st.dotsLine {
			f.cinemaDots(c.buf.g, float64(l.Lyr.X), top-gap*0.5, st.dots)
		}
		dist := i - st.cur
		if st.dotsOn && dist == 0 {
			dist = -1 // the line before a pause has been sung
		}
		a := lyrAlpha(dist, top, h, l.Lyr.Y, l.Lyr.bottom())
		if a < 0.01 {
			continue
		}
		bk := f.blockImage(i)
		if bk.img == nil {
			continue
		}
		src := pix(bk.img)
		sw, sh := bk.img.w, bk.img.h
		pad := bk.pad
		bx, by := int(math.Round(l.Lyr.X-pad)), int(math.Round(top-pad))
		if dist != 0 {
			blendWhite(dst, dw, dh, src, sw, sh, bx, by, 0, 0, sw, sh, clip, uint32(a*256))
			continue
		}
		// the line being sung: unsung words soft, sung words bright, the word being sung
		// filling up from left to right
		blendWhite(dst, dw, dh, src, sw, sh, bx, by, 0, 0, sw, sh, clip, 108)
		for k := range bk.words {
			if k > st.sungTo {
				break
			}
			frac := 1.0
			if k == st.sungTo {
				frac = st.part
			}
			wx := pad + bk.b.X[k] - c.lyrSize*0.04
			wy := pad + float64(bk.b.Row[k])*lineH - c.lyrSize*0.1
			ww := (bk.b.Wd[k] + c.lyrSize*0.08) * frac
			if ww < 1 {
				continue
			}
			blendWhite(dst, dw, dh, src, sw, sh, bx, by, int(wx), int(wy), int(math.Ceil(wx+ww)), int(math.Ceil(wy+lineH+c.lyrSize*0.2)), clip, 256)
		}
	}
}

// cinemaDots: three dots that light up one by one through a pause in the singing.
func (f *FullView) cinemaDots(g uintptr, x, y, pr float64) {
	r := f.cin.lyrSize * 0.12
	for k := 0; k < 3; k++ {
		on := math.Max(0, math.Min(1, pr*3-float64(k)))
		a := 0.3 + 0.7*on
		rr := r * (0.85 + 0.25*on)
		fillCircle(f.cin.buf.g, withAlpha(0xFFFFFFFF, a), float32(x+r+float64(k)*r*3.2), float32(y), float32(rr))
	}
}

/* ---------- to the screen ---------- */

/* ---------- mouse and keys ---------- */

func (f *FullView) cinemaHit(x, y float64) int {
	l := f.cin.lay
	if f.cin.help {
		return cinHoverNone
	}
	for i, b := range l.Buttons {
		if b.grow(6*l.U).has(x, y) {
			return i
		}
	}
	if l.ProgHit.has(x, y) {
		return cinHoverProg
	}
	if f.cin.next != nil && l.Next.has(x, y) {
		return cinHoverNext
	}
	return cinHoverNone
}

func (f *FullView) cinemaMove(x, y float64) {
	c := &f.cin
	if c.drag {
		c.dragFrac = math.Max(0, math.Min(1, (x-c.lay.Prog.X)/c.lay.Prog.W))
		return
	}
	c.hover = f.cinemaHit(x, y)
}

// cinemaDown: true when the press was on something (a double-click there doesn't leave).
func (f *FullView) cinemaDown(x, y float64) bool {
	c := &f.cin
	snap := media.Snapshot()
	switch h := f.cinemaHit(x, y); h {
	case 0:
		f.toggleMode("shuffle", snap)
	case 1:
		f.stepPrev(snap)
	case 2:
		mediaPlayPause()
	case 3:
		f.stepNext(snap)
	case 4:
		f.toggleMode("repeat", snap)
	case cinHoverProg:
		if snap.Track == nil || snap.Track.Duration <= 0 {
			return true
		}
		c.drag = true
		c.dragFrac = math.Max(0, math.Min(1, (x-c.lay.Prog.X)/c.lay.Prog.W))
		fsCapture(f.hwnd, true)
	case cinHoverNext:
		f.stepNext(snap)
	default:
		return false
	}
	c.ctrlSig = ""
	return true
}

func (f *FullView) cinemaUp() {
	c := &f.cin
	if !c.drag {
		return
	}
	c.drag = false
	fsCapture(f.hwnd, false)
	snap := media.Snapshot()
	if snap.Track == nil || snap.Track.Duration <= 0 {
		return
	}
	at := c.dragFrac * snap.Track.Duration
	if !requestSeek(at - media.Offset()) {
		f.say("Jumping needs Windows media controls (Settings → Music)")
	}
}

func (f *FullView) cinemaKey(vk int) {
	c := &f.cin
	snap := media.Snapshot()
	switch vk {
	case 0x1B, 0x7A: // Esc, F11
		if c.help {
			c.help = false
			return
		}
		closeFullscreen()
	case 0x20:
		mediaPlayPause()
	case 0x25, 0x27: // ← → the lyrics effect; Ctrl+← → the song
		d := 1
		if vk == 0x25 {
			d = -1
		}
		if ctrlDown() {
			if d < 0 {
				f.stepPrev(snap)
			} else {
				f.stepNext(snap)
			}
			return
		}
		n := cycle(cfg.FsStage, len(styleNames)+1, d)
		f.set(func() { cfg.FsStage = n; cfg.FsLyrics = true }, "Lyrics · "+stageNames()[n])
	case 0x26, 0x28:
		d := 4
		if vk == 0x28 {
			d = -4
		}
		f.textSize(d)
	case 'L':
		f.set(func() { cfg.FsLyrics = !cfg.FsLyrics }, "Lyrics · "+onOff(!cfg.FsLyrics))
	case 'B':
		n := cycle(cfg.FsBg, len(fsBgNames), 1)
		f.set(func() { cfg.FsBg = n }, "Background · "+fsBgNames[n])
	case 'V':
		n := 2 - cfg.FsCover // the cover ↔ the record
		f.set(func() { cfg.FsCover = n }, "Cover · "+fsCoverNames[n])
	case 'R':
		n := cycle(cfg.FsDisc, len(discNames)+1, 1)
		name := "Like the Vinyl widget"
		if n > 0 {
			name = discNames[n-1]
		}
		f.set(func() { cfg.FsDisc = n; cfg.FsCover = 2 }, "Record · "+name)
	case 'A':
		n := cycle(cfg.FsArm, len(fsArmNames), 1)
		f.set(func() { cfg.FsArm = n; cfg.FsCover = 2 }, "Tonearm · "+fsArmNames[n])
	case 'E':
		n := cycle(cfg.FsEffect, len(effectNames), 1)
		f.set(func() { cfg.FsEffect = n }, "Text effect · "+effectNames[n])
	case 'T':
		n := cycle(cfg.FsTheme, len(themes), 1)
		f.set(func() { cfg.FsTheme = n }, "Lyrics colours · "+themes[n].Name)
	case 'C':
		f.set(func() { cfg.FsClock = !cfg.FsClock }, "Clock · "+onOff(!cfg.FsClock))
	case 'H', 0x70:
		c.help = !c.help
	case 'Q':
		f.say(fmt.Sprintf("%d×%d · %.1f ms a frame", f.winW, f.winH, c.frameMs))
		logf("cinema %dx%d: %.1f ms a frame — %s — %s", f.winW, f.winH, c.frameMs, profLine(), frameStats())
	case 'S':
		openSettingsAt("fullscreen")
		closeFullscreen()
	}
}

// cinemaWheel: the mouse wheel is the volume (Windows' own, with its sign).
func (f *FullView) cinemaWheel(up bool) {
	vk := uintptr(0xAE)
	if up {
		vk = 0xAF
	}
	mediaKey(vk)
}

// gImagePart draws the part (sx,sy,sw,sh) of img into (x,y,w,h).
func gImagePart(g, img uintptr, x, y, w, h, sx, sy, sw, sh float32) {
	gImageSrc(g, img, x, y, w, h, sx, sy, sw, sh)
}

/* ---------- the lyrics stage: any lyric style, big, on the right half ---------- */

// stageConfig: the settings as the stage's lyrics should see them — Cinema's font, size,
// colours and effect, the chosen style, as wide as the stage.
func (f *FullView) stageConfig(real *Config) *Config {
	l := f.cin.lay
	c := *real
	c.Widgets = map[string]*WidgetPos{}
	for k, v := range real.Widgets {
		vv := *v
		c.Widgets[k] = &vv
	}
	ui := uiScale()
	c.FPS = real.FsFPS
	c.HoverEdit = false
	c.ShowLyrics, c.LyricsPlace = true, 1
	c.LyricsStyle = clampI(real.FsStage-1, 0, len(styleNames)-1)
	c.FontName, c.Bold = real.FsFont, real.FsBold
	c.TextSize = float64(real.FsTextSize) * l.U / ui * 1.45
	c.Effect, c.Theme = real.FsEffect, real.FsTheme
	if c.Effect == fxSoftGlow || c.Effect == fxStrongGlow { // a real glow is added afterwards (bloom)
		c.Effect = fxNone
	}
	c.TextColor, c.AccentColor = real.FsTextColor, real.FsAccentColor
	c.LyricsWidth = l.Lyr.W * 0.92 / math.Max(1, float64(workWidth())) * 100
	lp := c.widget("lyrics")
	lp.Scale = 1
	if c.Motion == moPulse || c.Motion == moStretch || c.Motion == moGlow {
		c.Motion = moStill // the beat moves the finished words in Go (cinemaStage): no redraw per beat
	}
	return &c
}

// cinemaStage runs the stage's lyrics for this frame; the answer changes whenever they drew.
func (f *FullView) cinemaStage(now time.Time, snap MediaSnap, bass float32, lines []Line, dt float64) string {
	c := &f.cin
	l := c.lay
	if cfg.FsStage == 0 || !l.Lyrics || f.lyr == nil {
		return ""
	}
	style := clampI(cfg.FsStage-1, 0, len(styleNames)-1)
	switch cfg.FsEffect {
	case fxSoftGlow:
		c.bloomOn = 0.75 * cfg.GlowAmount
	case fxStrongGlow:
		c.bloomOn = 1.3 * cfg.GlowAmount
	default:
		c.bloomOn = 0
	}
	// the beat's motion, every frame and smooth: Pulse and Stretch scale the drawn words as
	// they are laid on the screen, Glow turns the bloom up — the words themselves are not redrawn
	target := 0.0
	mode := cfg.Motion
	if snap.Playing && (mode == moPulse || mode == moStretch || mode == moGlow) && cfg.FsFPS > 0 {
		target = math.Min(1, float64(bass)*1.3) * cfg.MotionAmount
	}
	c.mot += (target - c.mot) * math.Min(1, dt*22)
	if c.mot < 0.002 {
		c.mot = 0
	}
	c.motSx, c.motSy, c.bloomGain = 1, 1, 1
	switch mode {
	case moPulse:
		c.motSx = 1 + 0.09*c.mot
		c.motSy = c.motSx
	case moStretch:
		c.motSx, c.motSy = 1+0.16*c.mot, 1-0.05*c.mot
	case moGlow:
		if c.bloomOn > 0 {
			c.bloomGain = 1 + c.mot*1.4
		} else {
			c.bloomOn, c.bloomGain = 0.75*math.Max(0.5, cfg.GlowAmount), c.mot*1.6
		}
	}
	// a new line: keep the old one to fade it out under the new one (not Flow: it shows it itself)
	if idx := lineAt(lines, snap.Pos+visualLead(style)); idx != c.stageIdx {
		c.stageIdx = idx
		if w := f.lyr.w; style != stFlow && w.canvas != nil && w.visible && w.alpha > 0 && cfg.FsFPS > 0 {
			n := w.canvas.w * w.canvas.h
			if cap(c.stageOld) < n {
				c.stageOld = make([]uint32, n)
			}
			c.stageOld, c.stageOldW, c.stageOldH = c.stageOld[:n], w.canvas.w, w.canvas.h
			gdiFlush()
			{
				copy(c.stageOld, pix(w.canvas))
				c.stageOldAt, c.stageOldT = f.stageRectRaw(), 0
			}
		}
	}
	if c.stageOldT < 1 {
		c.stageOldT += math.Min(dt, 1.0/30) / 0.32 // (big words: out of the way quickly)
	}
	lockCfgUI()
	real, realCover := cfg, cover
	cfg = f.stageConfig(real)
	if f.hiImg != 0 && f.hiKey == cover.key {
		cover.img = f.hiImg
	}
	lyrBoxH = l.Lyr.H * 0.84
	lyrFitW, lyrFitH = l.Lyr.W*0.9, l.Lyr.H
	func() {
		defer func() {
			lyrBoxH, lyrFitW, lyrFitH = 0, 0, 0
			cfg, cover = real, realCover
			unlockCfgUI()
		}()
		secProf("")
		f.lyr.Update(now, snap, bass, uiScale())
		secProf("s.update")
		// the next line, ready before it is needed (once, in a frame that had time to spare)
		if i := c.stageIdx + 1; i > 0 && i < len(lines) && c.warmed != lines[i].Text+fmt.Sprint(style, cfg.FsTextSize) && time.Since(c.frameT0) < 6*time.Millisecond {
			c.warmed = lines[i].Text + fmt.Sprint(style, cfg.FsTextSize)
			f.lyr.warm(&lines[i], style, uiScale())
		}
	}()
	w := f.lyr.w
	if w.canvas != nil && w.frames != c.boxAt {
		c.boxAt = w.frames
		gdiFlush()
		c.box = contentBox(pix(w.canvas), w.canvas.w, w.canvas.h)
	}
	if c.bloomOn > 0 && w.canvas != nil && w.frames != c.bloomAt {
		c.bloomAt = w.frames
		F := float64(cfg.FsTextSize) * l.U * 1.45
		secProf("")
		c.bloom, c.bloomW, c.bloomH = bloomFromBox(pix(w.canvas), w.canvas.w, w.canvas.h, 4, max(2, int(F*0.09/4+0.5)), c.box)
		secProf("s.bloomFrom")
	}
	return fmt.Sprint(w.frames, w.alpha, w.visible, w.w, w.h, c.stageOldT < 1, int(c.stageOldT*40), c.bloomOn, int(c.mot*200))
}

// cinemaStageDraw lays the stage's lyrics over the right half, centred, never past its edges:
// the old line fading out (drifting up a little), the glow, then the words.
func (f *FullView) cinemaStageDraw() {
	c := &f.cin
	l := c.lay
	clip := [4]int{int(l.Lyr.X), int(l.Lyr.Y), int(l.Lyr.right()), int(l.Lyr.bottom())}
	blend := func(px []uint32, cw, ch, dx, dy int, alpha byte) {
		x0, y0 := max(dx, clip[0]), max(dy, clip[1])
		x1, y1 := min(dx+cw, clip[2]), min(dy+ch, clip[3])
		if x1 <= x0 || y1 <= y0 || alpha == 0 {
			return
		}
		gdiFlush()
		overBlit(pix(c.buf), c.buf.w, c.buf.h, px, cw, ch, dx, dy, [4]int{x0, y0, x1, y1}, int(alpha))
	}
	if c.stageOld != nil && c.stageOldT < 1 {
		t := smooth(c.stageOldT)
		lift := int(t * l.U * 60)
		blend(c.stageOld, c.stageOldW, c.stageOldH, int(c.stageOldAt.X), int(c.stageOldAt.Y)-lift, byte(255*(1-t)))
	}
	w := f.lyr.w
	cv := w.canvas
	if cv != nil && w.visible && w.alpha > 0 {
		r := f.stageRectRaw()
		dx, dy := int(r.X), int(r.Y)
		if c.bloomOn > 0 && c.bloom != nil && c.bloomW*4 >= cv.w-4 {
			col := mixColor(textColorFor(cfg.FsTheme, cfg.FsTextColor), cinTint(), 0.45) | 0xFF000000
			if k := c.bloomOn * c.bloomGain; k > 0.004 {
				bc := clip // the glow only reaches a little past the words
				if c.boxAt == w.frames && c.box[2] > c.box[0] {
					g := int(float64(cfg.FsTextSize)*l.U*0.6) + 4
					bc = [4]int{max(bc[0], dx+c.box[0]-g), max(bc[1], dy+c.box[1]-g), min(bc[2], dx+c.box[2]+g), min(bc[3], dy+c.box[3]+g)}
				}
				if bc[2] > bc[0] && bc[3] > bc[1] {
					addBloom(pix(c.buf), c.buf.w, c.buf.h, c.bloom, c.bloomW, c.bloomH, 4, dx, dy, bc, col, k*float64(w.alpha)/255)
				}
			}
		}
		wc := clip // the words only (most of the canvas is air)
		if c.boxAt == w.frames && c.box[2] > c.box[0] {
			b := c.box
			if c.motSx != 1 || c.motSy != 1 { // where the box lands, scaled about the canvas centre
				mx, my := float64(dx)+float64(cv.w)/2, float64(dy)+float64(cv.h)/2
				sc := func(v, m, k float64) float64 { return m + (v-m)*k }
				b = [4]int{int(sc(float64(dx+b[0]), mx, c.motSx)) - 2, int(sc(float64(dy+b[1]), my, c.motSy)) - 2,
					int(sc(float64(dx+b[2]), mx, c.motSx)) + 2, int(sc(float64(dy+b[3]), my, c.motSy)) + 2}
			} else {
				b = [4]int{dx + b[0], dy + b[1], dx + b[2], dy + b[3]}
			}
			wc = [4]int{max(wc[0], b[0]), max(wc[1], b[1]), min(wc[2], b[2]), min(wc[3], b[3])}
		}
		gdiFlush()
		if c.motSx != 1 || c.motSy != 1 {
			overBlitScaled(pix(c.buf), c.buf.w, c.buf.h, pix(cv), cv.w, cv.h, dx, dy, c.motSx, c.motSy, wc, int(w.alpha))
		} else if wc[2] > wc[0] && wc[3] > wc[1] {
			overBlit(pix(c.buf), c.buf.w, c.buf.h, pix(cv), cv.w, cv.h, dx, dy, wc, int(w.alpha))
		}
	}
	gdiFlush()
}

// stageRectRaw: where the stage's lyrics canvas sits (centred on the stage), unclipped.
func (f *FullView) stageRectRaw() rectF {
	l := f.cin.lay
	if f.lyr == nil || f.lyr.w.canvas == nil {
		return rectF{}
	}
	cv := f.lyr.w.canvas
	cx, cy := l.Lyr.X+l.Lyr.W/2, l.Lyr.Y+l.Lyr.H/2
	return rectF{math.Round(cx - float64(cv.w)/2), math.Round(cy - float64(cv.h)/2), float64(cv.w), float64(cv.h)}
}

// stageRect: what the stage covers now — its words, and the old line while it fades (clipped).
func (f *FullView) stageRect() rectF {
	l := f.cin.lay
	if cfg.FsStage == 0 || f.lyr == nil || !l.Lyrics {
		return rectF{}
	}
	r := rectF{}
	if f.lyr.w.canvas != nil && f.lyr.w.visible {
		r = f.stageRectRaw()
		if c := &f.cin; c.boxAt == f.lyr.w.frames && c.box[2] > c.box[0] { // only where the words are (and their glow)
			g := 0.0
			if c.bloomOn > 0 {
				g = float64(cfg.FsTextSize) * l.U * 0.6
			}
			r = rectF{r.X + float64(c.box[0]) - g, r.Y + float64(c.box[1]) - g, float64(c.box[2]-c.box[0]) + 2*g, float64(c.box[3]-c.box[1]) + 2*g}
		}
		if c := &f.cin; c.motSx > 1 || c.motSy > 1 { // the beat's Pulse / Stretch makes it a little bigger
			gx, gy := r.W*(math.Max(1, c.motSx)-1)/2+2, r.H*(math.Max(1, c.motSy)-1)/2+2
			r = rectF{r.X - gx, r.Y - gy, r.W + 2*gx, r.H + 2*gy}
		}
	}
	if c := &f.cin; c.stageOld != nil && c.stageOldT < 1 {
		o := c.stageOldAt
		o.Y -= l.U * 60
		o.H += l.U * 60
		r = unionRect(r, o)
	}
	return clipRectF(r, l.Lyr)
}

func clipRectF(r, to rectF) rectF {
	x0, y0 := math.Max(r.X, to.X), math.Max(r.Y, to.Y)
	x1, y1 := math.Min(r.right(), to.right()), math.Min(r.bottom(), to.bottom())
	if x1 <= x0 || y1 <= y0 {
		return rectF{}
	}
	return rectF{x0, y0, x1 - x0, y1 - y0}
}

func unionRect(a, b rectF) rectF {
	if a.W <= 0 || a.H <= 0 {
		return b
	}
	if b.W <= 0 || b.H <= 0 {
		return a
	}
	x0, y0 := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	return rectF{x0, y0, math.Max(a.right(), b.right()) - x0, math.Max(a.bottom(), b.bottom()) - y0}
}

// textColorFor: the lyrics colour of a theme (Cinema has its own theme and colours).
func textColorFor(theme int, custom string) uint32 {
	if theme == len(themes)-1 {
		return parseHex(custom)
	}
	return themes[theme%len(themes)].Text | 0xFF000000
}

/* ---------- the turning record (cover mode "Vinyl") ---------- */

// vinylConfig: the record as Cinema shows it — the Vinyl widget's look (or Cinema's own record),
// alone, as big as the cover would be.
func (f *FullView) vinylConfig(real *Config) *Config {
	c := *real
	c.Widgets = map[string]*WidgetPos{}
	for k, v := range real.Widgets {
		vv := *v
		c.Widgets[k] = &vv
	}
	ui := uiScale()
	c.FPS = real.FsFPS
	c.HoverEdit, c.Scratch, c.ClickPlay = false, false, false
	c.ShowVinyl, c.VinylLayout, c.VinylCorner, c.ShowList, c.AmbVinyl = true, 0, 0, false, false
	c.SwapAnim = true
	c.ShowArm = false // centred on its own the record looks better without the arm across it
	if real.FsDisc > 0 {
		c.DiscStyle = real.FsDisc - 1
	}
	_, _, R := f.recGeom()
	vp := c.widget("vinyl")
	vp.Set, vp.Scale = false, R/(190*ui)
	return &c
}

// recGeom: the record's centre and radius on screen. With the tonearm it is a little smaller and
// a little to the left, so that the arm fits in the same square.
func (f *FullView) recGeom() (cx, cy, R float64) {
	l := f.cin.lay
	A := l.Art.W
	cx, cy, R = l.Art.X+A/2, l.Art.Y+A/2, A/2*0.96
	if cfg.FsArm > 0 {
		cx, cy, R = cx-A*0.075, cy+A*0.03, A/2*0.80
	}
	return
}

// the tonearm's colours: tube, its light edge, its dark edge, headshell, cap, cap ring, base
var armPalettes = [][7]uint32{
	{0xFFCDD0D6, 0xFFF5F6F9, 0xFF8C9097, 0xFF34353A, 0xFFEDEEF1, 0xFF9DA0A7, 0xFF26272C}, // Silver
	{0xFF2A2B30, 0xFF5E6168, 0xFF121215, 0xFF1A1A1E, 0xFF2E2F34, 0xFFC3C6CC, 0xFF111114}, // Black
	{0xFFC8A257, 0xFFF3DFA6, 0xFF7E5F25, 0xFF2B2520, 0xFF202024, 0xFFD9B66A, 0xFF18181B}, // Gold
}

// cinemaArm draws the tonearm over the record (into the frame, inside clip).
func (f *FullView) cinemaArm(clip rectF) {
	c := &f.cin
	if cfg.FsArm <= 0 || f.vin == nil {
		return
	}
	cx, cy, R := f.recGeom()
	p := tonearm(cx, cy, R, f.vin.progress, c.armT)
	pal := armPalettes[(cfg.FsArm-1)%len(armPalettes)]
	g := c.buf.g
	clipRect(g, float32(clip.X), float32(clip.Y), float32(clip.W), float32(clip.H))
	defer clipReset(g)
	r := float32(R)
	F := func(x, y float64) (float32, float32) { return float32(x), float32(y) }
	Px, Py := F(p.Px, p.Py)
	Kx, Ky := F(p.Kx, p.Ky)
	Nx, Ny := F(p.Nx, p.Ny)
	W0x, W0y := F(p.W0x, p.W0y)
	W1x, W1y := F(p.W1x, p.W1y)
	lift := float32(c.armLift)
	// the rest post, the base under the pivot
	// the rest post: there when the arm is on it, faint while the needle is in the groove
	rx, ry := F(p.RestX, p.RestY)
	pa := 0.3 + 0.7*math.Max(0, math.Min(1, 1-c.armT))
	fillCircle(g, withAlpha(0xFF000000, 0.4*pa), rx+r*0.012, ry+r*0.02, r*0.05)
	fillCircle(g, withAlpha(pal[6], pa), rx, ry, r*0.045)
	fillCircle(g, withAlpha(pal[5], 0.5*pa), rx, ry, r*0.022)
	fillCircle(g, 0x70000000, Px+r*0.02, Py+r*0.035, r*0.215)
	fillCircle(g, pal[6], Px, Py, r*0.2)
	ring := newPen(pal[5]&0x60FFFFFF, r*0.008)
	drawEllipsePen(g, ring, Px-r*0.17, Py-r*0.17, r*0.34, r*0.34)
	freePen(ring)
	// its shadow on the record: further away (and softer) while lifted
	so := r * (0.025 + 0.06*lift)
	sa := uint32(0x58-int(0x20*lift)) << 24
	line(g, sa, r*0.06, Px+so, Py+so*1.4, Kx+so, Ky+so*1.4)
	line(g, sa, r*0.09, Kx+so, Ky+so*1.4, Nx+so, Ny+so*1.4)
	// counterweight
	line(g, pal[2], r*0.16, W0x, W0y, W1x, W1y)
	line(g, pal[0], r*0.13, W0x, W0y, W1x, W1y)
	line(g, pal[1]&0x90FFFFFF, r*0.03, W0x-r*0.03, W0y-r*0.02, W1x-r*0.03, W1y-r*0.02)
	// the tube, lit along one edge
	tw := r * (0.042 + 0.006*lift)
	nxp, nyp := float32(-math.Sin(p.Theta)), float32(math.Cos(p.Theta))
	line(g, pal[2], tw*1.25, Px, Py, Kx, Ky)
	line(g, pal[0], tw, Px, Py, Kx, Ky)
	line(g, pal[1], tw*0.32, Px-nxp*tw*0.22, Py-nyp*tw*0.22, Kx-nxp*tw*0.22, Ky-nyp*tw*0.22)
	// headshell and cartridge
	line(g, pal[3], r*0.085, Kx, Ky, Nx, Ny)
	hx, hy := Kx+(Nx-Kx)*0.35, Ky+(Ny-Ky)*0.35
	line(g, pal[0]&0xC0FFFFFF, r*0.02, hx, hy, hx+nxp*r*0.11, hy+nyp*r*0.11) // finger lift
	line(g, 0xFF0E0E10, r*0.06, Kx+(Nx-Kx)*0.6, Ky+(Ny-Ky)*0.6, Nx, Ny)
	fillCircle(g, pal[5], Nx, Ny, r*0.012)
	// the pivot cap: a polished dome
	fillCircle(g, pal[2], Px, Py, r*0.105)
	fillCircle(g, pal[4], Px, Py, r*0.092)
	fillCircle(g, 0xB0FFFFFF, Px-r*0.03, Py-r*0.032, r*0.034)
	fillCircle(g, 0x40FFFFFF, Px-r*0.012, Py-r*0.012, r*0.07)
}

// armStep moves the tonearm toward where it should be for this frame.
func (f *FullView) armStep(now time.Time, snap MediaSnap) {
	c := &f.cin
	dt := now.Sub(c.armAt).Seconds()
	c.armAt = now
	if dt <= 0 || dt > 0.25 {
		dt = 1.0 / 60
	}
	target := 0.0
	if snap.Playing && snap.Track != nil && (f.vin == nil || f.vin.swapT < 0) {
		target = 1
	}
	if cfg.FsArm <= 0 {
		c.armT, c.armV, c.armLift = target, 0, 0
		return
	}
	c.armT, c.armV = armSpring(c.armT, c.armV, target, dt)
	// lifted while it travels, lowered gently once it is over the groove
	lt := 1.0
	if target == 1 && math.Abs(c.armT-1) < 0.03 && math.Abs(c.armV) < 0.25 {
		lt = 0
	}
	rate := 6.0
	if lt == 0 {
		rate = 2.6 // the needle comes down slowly
	}
	c.armLift += (lt - c.armLift) * math.Min(1, dt*rate)
	if math.Abs(c.armLift-lt) < 0.002 {
		c.armLift = lt
	}
}

// cinemaVinyl turns the record for this frame; the answer changes whenever it drew.
func (f *FullView) cinemaVinyl(now time.Time, snap MediaSnap, bands [nBands]float32, bass float32) string {
	if cfg.FsCover != 2 || f.vin == nil {
		f.freeVinyl()
		return ""
	}
	lockCfgUI()
	real, realCover := cfg, cover
	cfg = f.vinylConfig(real)
	if f.hiImg != 0 && f.hiKey == cover.key {
		cover.img = f.hiImg
	}
	func() {
		defer func() {
			cfg, cover = real, realCover
			unlockCfgUI()
		}()
		secProf("")
		f.vin.Update(now, snap, bands, bass, uiScale())
		secProf("r.vinUpdate")
	}()
	f.armStep(now, snap)
	return fmt.Sprintf("%d %v %.4f %.3f %d", f.vin.w.frames, f.vin.w.visible, f.cin.armT, f.cin.armLift, cfg.FsArm)
}

func (f *FullView) freeVinyl() {
	v := f.vin
	if v == nil || v.w.canvas == nil {
		return
	}
	v.w.release()
	v.base.Free()
	v.label.Free()
	v.base, v.label, v.sig, v.recSig = nil, nil, "", ""
	v.endSwap()
	v.holo = nil
}

// vinylRect: where the record's canvas sits (its disc centred on the cover's place), clipped to
// the left column above the song title.
func (f *FullView) vinylRect() (rectF, int, int) {
	l := f.cin.lay
	v := f.vin
	if v == nil || v.w.canvas == nil {
		return rectF{}, 0, 0
	}
	cx, cy, _ := f.recGeom()
	dx := int(math.Round(cx - v.cx))
	dy := int(math.Round(cy - v.cy))
	right := l.W
	if l.Lyrics {
		right = l.Lyr.X - 8*l.U
	}
	r := rectF{float64(dx), float64(dy), float64(v.w.canvas.w), float64(v.w.canvas.h)}
	if cfg.FsArm > 0 { // and the arm, which reaches past the record on the right
		A := l.Art.W
		r = unionRect(r, rectF{l.Art.X - A*0.05, l.Art.Y - A*0.12, A * 1.12, A * 1.1})
	}
	r = clipRectF(r, rectF{0, 0, right, l.Title - 6*l.U})
	r = rectF{math.Floor(r.X), math.Floor(r.Y), math.Ceil(r.W), math.Ceil(r.H)}
	return r, dx, dy
}

func (f *FullView) cinemaVinylDraw() {
	c := &f.cin
	v := f.vin
	if cfg.FsCover != 2 || v == nil || v.w.canvas == nil || !v.w.visible || c.help {
		return
	}
	r, dx, dy := f.vinylRect()
	if r.W <= 0 || r.H <= 0 {
		return
	}
	x0, y0 := int(r.X), int(r.Y)
	w, h := int(r.W), int(r.H)
	gdiFlush()
	gFlush(v.w.canvas.g)
	cv := v.w.canvas
	feather := int(v.R * 0.12) // its glow fades out before the edge of its picture, never cut
	if v.swapT >= 0 {          // changing records: a record passing the edge of its picture fades there
		feather = int(v.R * 0.35)
	}
	overBlitFeather(pix(c.buf), c.buf.w, c.buf.h, pix(cv), cv.w, cv.h, dx, dy, [4]int{x0, y0, x0 + w, y0 + h}, feather)
	f.cinemaArm(r)
}
