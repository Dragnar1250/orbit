//go:build windows || darwin

package main

// Full screen: one opaque window over a whole monitor. A blurred / animated background, a big
// centre piece (album art, the turntable or the visualizer), its own lyrics, an "around the
// screen" frame, bass ripples, track info and a clock. Esc, F11 or a double-click close it.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"
	"unsafe"
)

var fs *FullView

type fsRipple struct{ born float64 }

type fsStar struct{ x, y, z, pz float64 }

type FullView struct {
	hwnd uintptr
	W, H int
	mon  RECT
	buf  *Canvas // the frame, drawn at W×H (may be smaller than the screen, see q)
	bg   *Canvas // the background, drawn small and stretched (soft and cheap)
	out  *Canvas // screen-size copy when the frame is drawn smaller (q < 1)
	winW int     // the screen
	winH int
	q    float64 // frame size / screen size

	frameMs  float64 // average time to make a frame
	slowRuns int
	perfAt   time.Time
	scaleTmp []uint32 // the half-way image of the smooth background stretch
	pendingQ float64  // a new frame scale to switch to before the next frame
	maxQ     float64  // the largest frame scale it opened with (a Retina budget on the Mac)
	glowKind int      // 1: a shadow + glow behind the centre piece (drawn with the background)
	tiny     *Bitmap  // the cover squeezed to a few pixels (blurred background)
	tinyK    string
	art      *Canvas // the big rounded cover (album art mode)
	artSig   string

	vin  *VinylView
	hal  *HaloView
	lyr  *LyricsView
	glow *GlowView

	last    time.Time
	t       float64
	ver     int
	bassS   float32
	bassAvg float32
	lastHit float64
	ripples []fsRipple
	stars   []fsStar
	rng     *rand.Rand

	hiKey, hiBusy string
	hiImg         uintptr
	hiVer         int

	lyrIdx         int
	lyrX, lyrY     float64 // wandering: where the lines glide to (centre of the text)
	lyrCX, lyrCY   float64
	mouseAt        time.Time
	mousePt        POINT
	cursorOff      bool
	hintAt         time.Time
	toast          string
	toastAt        time.Time
	cx, cy, radius float32 // the centre piece

	bgFull  *Canvas // the background at full size (see paintBg)
	bgSig   string
	bgAt    float64
	bgStill bool
	edge    edgeVideo // the music video window (video_windows.go)
	videoOn bool      // the video plays: the background is see-through
	veil    *Canvas   // the darkening over the video
	veilSig string
	wander  [4]float64 // wandering lyrics stay inside this box (x0, y0, x1, y1); zero = the whole screen
	skin    skinState
}

/* ---------- open / close ---------- */

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
	f.buf.Free()
	f.bg.Free()
	f.out.Free()
	f.buf, f.bg, f.out = nil, nil, nil
	f.tiny.Free()
	f.art.Free()
	f.vin.label.Free()
	f.vin.base.Free()
	f.vin.w.canvas.Free()
	f.hal.label.Free()
	f.hal.w.canvas.Free()
	f.lyr.w.canvas.Free()
	freeImage(f.hiImg)
	f.hiImg = 0
	f.bgFull.Free()
	f.veil.Free()
	f.veil = nil
	f.skin.free()
}

/* ---------- window messages ---------- */

// measure keeps an eye on how long a frame takes. If this PC can't keep up, the frame is drawn
// smaller (and stretched) until it can; the numbers go to orbit.log.
func (f *FullView) measure(d time.Duration, now time.Time) {
	ms := float64(d.Microseconds()) / 1000
	if f.frameMs == 0 {
		f.frameMs = ms
	}
	f.frameMs += (ms - f.frameMs) * 0.1
	if f.perfAt.IsZero() {
		f.perfAt = now
	}
	if now.Sub(f.perfAt) < 3*time.Second {
		return
	}
	f.perfAt = now
	budget := 1000 / float64(max(30, cfg.FsFPS)) * 0.85
	logf("full screen %dx%d drawn at %dx%d: %.1f ms a frame", f.winW, f.winH, f.W, f.H, f.frameMs)
	minQ, slow, runs := 0.75, budget*1.6, 3
	if gpuUpscale {
		// the GPU stretches it smoothly (on a Retina screen: still one pixel per point), so it
		// steps down as soon as frames come close to late — smooth first, sharp second
		minQ, slow, runs = 0.5, budget, 2
	}
	if f.frameMs > slow && f.q > minQ {
		f.slowRuns++
		if f.slowRuns >= runs {
			f.slowRuns = 0
			nq := math.Max(minQ, f.q-0.125)
			logf("full screen: too slow here, drawing at %.0f%% from now on", nq*100)
			f.pendingQ = nq
		}
	} else if f.frameMs < budget*0.45 && f.q < f.topQ() { // plenty of time again: sharper
		f.slowRuns--
		if f.slowRuns <= -3 {
			f.slowRuns = 0
			f.pendingQ = math.Min(f.topQ(), f.q+0.15)
			logf("full screen: fast enough, drawing at %.0f%%", f.pendingQ*100)
		}
	} else {
		f.slowRuns = 0
	}
}

func (f *FullView) topQ() float64 {
	if f.maxQ > 0 {
		return f.maxQ
	}
	return 1
}

// perfToast: how fast frames are made on this PC (Q) — handy when something feels slow.
func (f *FullView) perfToast() {
	f.say(fmt.Sprintf("%d×%d drawn at %d×%d · %.1f ms a frame · %d fps", f.winW, f.winH, f.W, f.H, f.frameMs, atomic.LoadInt32(&fpsNow)))
}

// wake: the mouse moved — show the cursor and the key hints for a moment.
func (f *FullView) wake() {
	f.mouseAt = time.Now()
	if f.cursorOff {
		f.cursorOff = false
		showCursor(true)
	}
	if time.Since(f.hintAt) > 4*time.Second && !f.skinOn() {
		f.hintAt = time.Now().Add(-1500 * time.Millisecond) // a shorter show than at the start
	}
}

func (f *FullView) say(s string) { f.toast, f.toastAt = s, time.Now() }

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

func (f *FullView) key(vk int) {
	switch vk {
	case 0x1B, 0x7A: // Esc, F11
		closeFullscreen()
	case 0x20: // Space
		mediaPlayPause()
		f.say("Play / pause")
	case 'A', 'M':
		f.set(func() { cfg.FsMode = cycle(cfg.FsMode, len(fsModeNames), 1) }, "Mode · "+fsModeNames[cycle(cfg.FsMode, len(fsModeNames), 1)])
	case 'B':
		f.set(func() { cfg.FsBg = cycle(cfg.FsBg, len(fsBgNames), 1) }, "Background · "+fsBgNames[cycle(cfg.FsBg, len(fsBgNames), 1)])
	case 'V':
		n := cycle(cfg.FsVis, len(visNames)+1, 1)
		name := "Same as the widget"
		if n > 0 {
			name = visNames[n-1]
		}
		f.set(func() { cfg.FsVis = n }, "Visualizer · "+name)
	case 'L':
		f.set(func() { cfg.FsLyrics = !cfg.FsLyrics }, "Lyrics · "+onOff(!cfg.FsLyrics))
	case 0x25, 0x27: // ← →
		d := 1
		if vk == 0x25 {
			d = -1
		}
		n := cycle(cfg.FsLyricsStyle, len(styleNames), d)
		f.set(func() { cfg.FsLyricsStyle = n; cfg.FsLyrics = true }, "Lyrics style · "+styleNames[n])
	case 0x26, 0x28: // ↑ ↓
		d := 4
		if vk == 0x28 {
			d = -4
		}
		f.textSize(d)
	case 'P':
		f.set(func() { cfg.FsLyricsPos = cycle(cfg.FsLyricsPos, len(fsLyrPosNames), 1) }, "Lyrics · "+fsLyrPosNames[cycle(cfg.FsLyricsPos, len(fsLyrPosNames), 1)])
	case 'E':
		f.set(func() { cfg.FsEffect = cycle(cfg.FsEffect, len(effectNames), 1) }, "Text effect · "+effectNames[cycle(cfg.FsEffect, len(effectNames), 1)])
	case 'T':
		n := cycle(cfg.FsTheme, len(themes), 1)
		f.set(func() { cfg.FsTheme = n }, "Lyrics colours · "+themes[n].Name)
	case 'F':
		f.set(func() { cfg.FsFrame = !cfg.FsFrame }, "Frame · "+onOff(!cfg.FsFrame))
	case 'G':
		n := cycle(cfg.FsFrameStyle, len(glowStyleNames), 1)
		f.set(func() { cfg.FsFrameStyle = n; cfg.FsFrame = true }, "Frame · "+glowStyleNames[n])
	case 'C':
		f.set(func() { cfg.FsClock = !cfg.FsClock }, "Clock · "+onOff(!cfg.FsClock))
	case 'I':
		f.set(func() { cfg.FsInfo = cycle(cfg.FsInfo, len(fsInfoNames), 1) }, "Song info · "+fsInfoNames[cycle(cfg.FsInfo, len(fsInfoNames), 1)])
	case 'R':
		f.set(func() { cfg.FsRipple = !cfg.FsRipple }, "Bass ripple · "+onOff(!cfg.FsRipple))
	case 'H', 0x70: // H, F1
		f.hintAt = time.Now()
	case 'Q':
		f.perfToast()
	case 0xBC, 0xBE: // , and . : the video earlier / later against the song
		d := 0.5
		if vk == 0xBC {
			d = -0.5
		}
		video.Lock()
		video.nudge(d)
		off := video.Offset
		video.Unlock()
		f.say(fmt.Sprintf("Video %+.1f s", off))
	case 'N':
		video.Lock()
		video.skip()
		id := video.current()
		video.Unlock()
		if id == "" {
			f.say("No other video for this song")
		} else {
			f.say("Trying another video")
		}
	case 'D':
		f.set(func() { cfg.FsKind = 1 }, "Desktop skin")
	case 'S':
		openSettingsAt("fullscreen")
		closeFullscreen()
	}
}

/* ---------- every frame ---------- */

func (f *FullView) Update(now time.Time, snap MediaSnap, bands [nBands]float32, bass float32) {
	if f.pendingQ > 0 { // switch to a smaller frame between frames
		q := f.pendingQ
		f.pendingQ = 0
		if !f.setupBuffers(q) {
			closeFullscreen()
			return
		}
	}
	dt := math.Max(0, math.Min(0.1, now.Sub(f.last).Seconds()))
	f.last = now
	f.t += dt
	if snap.Version != f.ver {
		f.ver = snap.Version
		if snap.Track != nil {
			pushHistory(snap.Track) // the desktop turntable is asleep: keep its list up to date
		}
		f.followSong(snap)
		f.skin.list.sig, f.skin.centre.sig = "", ""
	}
	if cfg.FsKind == 1 {
		f.spotifyTick(snap) // the real "up next" when Spotify is connected
	}
	for _, n := range spotify.takeNotes() { // what Spotify answered to a change asked from here
		f.say(n)
	}
	f.vin.version = snap.Version // (pushed above)
	updateCover(snap.Track)
	f.fetchHiRes(snap.Track)
	playing := snap.Playing && snap.Track != nil
	if !playing {
		bass = 0
	}
	f.bassS += (bass - f.bassS) * 0.35
	if cfg.FsRipple && playing && bass > 0.45 && bass > f.bassAvg*1.3+0.06 && f.t-f.lastHit > 0.3 {
		f.lastHit = f.t
		f.ripples = append(f.ripples, fsRipple{born: f.t})
		if len(f.ripples) > 5 {
			f.ripples = f.ripples[1:]
		}
	}
	f.bassAvg += (bass - f.bassAvg) * 0.06
	// frames per second: full speed while something moves, little when nothing does
	fps := cfg.FsFPS
	busy := playing || now.Sub(f.mouseAt) < 3*time.Second || f.toast != "" || now.Sub(f.hintAt) < 6*time.Second || f.skin.searchOn || len(f.ripples) > 0
	if !busy {
		fps = 6
	} else if !fsInFront(f) && fps > 15 {
		fps = 15 // something else is in front: we may be half hidden
	}
	atomic.StoreInt32(&fpsNow, int32(fps))
	if !f.cursorOff && !f.skinOn() && now.Sub(f.mouseAt) > 2500*time.Millisecond {
		pt := cursorPos()
		if pt.X >= f.mon.Left && pt.X < f.mon.Right && pt.Y >= f.mon.Top && pt.Y < f.mon.Bottom && fsInFront(f) {
			f.cursorOff = true // only our own screen, and only while we are in front
			showCursor(false)
		} else {
			f.mouseAt = now
		}
	}

	wantVideo := (cfg.FsKind == 1 && cfg.SkinBg == len(skinBgNames)-1) || (cfg.FsKind == 0 && cfg.FsBg == fsbVideo)
	f.videoTick(snap, wantVideo, now)

	// the views read the global settings: lend them a copy with the full-screen choices
	cfgMu.Lock()
	real, realCover := cfg, cover
	defer func() {
		cfg, cover = real, realCover
		cfgMu.Unlock()
	}()
	c := f.fsConfig(real)
	cfg = c
	if f.hiImg != 0 && f.hiKey == cover.key {
		cover.img = f.hiImg
	}
	ui := uiScale()
	skin := c.FsKind == 1
	if !skin {
		switch c.FsMode {
		case fsmVinyl:
			f.vin.Update(now, snap, bands, bass, ui)
		case fsmVis:
			f.hal.Update(now, snap, bands, bass, true, ui)
		}
	}
	if (!skin && c.FsLyrics) || (skin && c.SkinLyrics) {
		f.lyr.Update(now, snap, bass, ui)
	} else {
		f.lyr.w.visible = false
	}
	if (!skin && c.FsFrame) || (skin && c.SkinFrame) {
		f.glow.step(dt, snap, bands, bass)
	}
	t0 := time.Now()
	if skin {
		f.renderSkin(now, snap)
	} else {
		f.wander = [4]float64{}
		f.render(now, snap, dt)
	}
	cfg, cover = real, realCover
	f.blit()
	f.measure(time.Since(t0), now)
}

// fsConfig: the settings as the full-screen views should see them.
func (f *FullView) fsConfig(real *Config) *Config {
	c := *real
	c.Widgets = map[string]*WidgetPos{}
	for k, v := range real.Widgets {
		vv := *v
		c.Widgets[k] = &vv
	}
	ui := uiScale()
	side := float64(min(f.W, f.H))
	c.FPS = real.FsFPS
	c.HoverEdit = false
	// turntable: alone in the middle, no list, no corner
	c.ShowVinyl, c.VinylLayout, c.VinylCorner, c.ShowList, c.AmbVinyl = true, 0, 0, false, false
	vp := c.widget("vinyl")
	vp.Set, vp.Scale = false, side*0.32/(190*ui)
	// visualizer
	c.ShowHalo, c.HaloCard, c.AmbHalo, c.HaloBackdrop = true, 0, false, 0
	if real.FsVis > 0 {
		c.HaloStyle = real.FsVis - 1
	}
	c.HaloPalette = real.FsPalette
	hp := c.widget("halo")
	hp.Set, hp.Scale = false, side*0.66/(460*ui)
	// lyrics
	c.ShowLyrics, c.LyricsPlace = true, 1
	c.LyricsStyle = real.FsLyricsStyle
	c.FontName, c.Bold = real.FsFont, real.FsBold
	c.TextSize = float64(real.FsTextSize) * float64(f.H) / 1080 / ui
	c.Effect, c.Theme = real.FsEffect, real.FsTheme
	c.TextColor, c.AccentColor = real.FsTextColor, real.FsAccentColor
	c.LyricsWidth = float64(real.FsLyricsWidth) * float64(f.W) / math.Max(1, float64(workWidth()))
	lp := c.widget("lyrics")
	lp.Scale = 1
	// frame
	c.GlowStyle, c.GlowColorMode, c.GlowBright, c.GlowReach = real.FsFrameStyle, real.FsFrameColor, real.FsFrameBright, real.FsFrameReach
	c.GlowWhere, c.GlowMode, c.GlowScreens = 0, 0, 0
	if real.FsKind == 1 { // desktop skin: its own lyrics style and edge light
		c.LyricsStyle = real.SkinLyricsStyle
		c.GlowStyle = real.SkinFrameStyle
	}
	return &c
}

// the colours of the full-screen scene follow its visualizer palette (lyrics colours, album, presets)
func (f *FullView) accent() uint32 {
	a, _ := f.palette()
	return a | 0xFF000000
}

func (f *FullView) palette() (uint32, uint32) {
	a, b := visColors()
	if cfg.FsPalette == 1 && albumAccent != 0 { // album: a second, neighbouring hue for depth
		b = hueShift(mixColor(a, 0xFFFFFFFF, 0.15), 0.09)
	}
	return a | 0xFF000000, b | 0xFF000000
}

/* ---------- high-resolution covers ---------- */

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
		f.vin.sig, f.hal.sig, f.artSig = "", "", ""
		f.skin.centre.sig, f.bgSig = "", ""
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

// prefetchHiRes: while a song plays on the desktop, fetch its big cover in the background, so
// full screen has it the moment it opens.
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

/* ---------- drawing ---------- */

func (f *FullView) render(now time.Time, snap MediaSnap, dt float64) {
	g := f.buf.g
	W, H := float32(f.W), float32(f.H)
	mode := cfg.FsMode
	lyrOn := cfg.FsLyrics && f.lyr.w.visible && f.lyr.w.canvas != nil
	// where the centre piece sits
	f.cx, f.cy = W/2, H/2
	if mode != fsmLyrics {
		if (cfg.FsLyrics && cfg.FsLyricsPos == 0) || cfg.FsInfo == 2 {
			f.cy = H * 0.43
		} else if cfg.FsLyrics && cfg.FsLyricsPos == 1 {
			f.cy = H * 0.56
		}
	}
	side := float32(min(f.W, f.H))
	switch mode {
	case fsmArt:
		f.radius = side * 0.23
	case fsmVinyl:
		f.radius = float32(f.vin.R) * 1.04
	case fsmVis:
		f.radius = side * 0.3
	default:
		f.radius = 0
	}

	f.glowKind = 0
	if mode == fsmArt {
		f.glowKind = 1
	}
	f.paintBg(cfg.FsBg, false)
	if cfg.FsBg == fsbStars {
		f.drawStars(g, dt)
	}
	f.drawRipples(g)

	switch mode {
	case fsmArt:
		f.drawArt(g)
	case fsmVinyl:
		if c := f.vin.w.canvas; c != nil && f.vin.w.visible {
			f.blend(c, int(f.cx)-int(f.vin.cx), int(f.cy)-int(f.vin.cy), 255)
		}
	case fsmVis:
		if c := f.hal.w.canvas; c != nil && f.hal.w.visible {
			f.blend(c, int(f.cx)-c.w/2, int(f.cy)-c.h/2, 255)
		}
	}
	if cfg.FsInfo > 0 {
		f.drawInfo(g, snap)
	}
	if lyrOn {
		f.placeLyrics(now)
		c := f.lyr.w.canvas
		f.blend(c, int(f.lyrCX)-c.w/2, int(f.lyrCY)-c.h/2, f.lyr.w.alpha)
	}
	if cfg.FsClock {
		f.drawClock(g, now)
	}
	if cfg.FsFrame {
		T := float32(90 * float64(f.H) / 1080 * cfg.GlowReach)
		f.glow.drawAll(g, RECT{0, 0, int32(f.W), int32(f.H)}, T)
	}
	f.drawHints(g, now)
}

// pix: a canvas' pixels as a Go slice (the DIB memory itself).
func pix(c *Canvas) []uint32 {
	if c == nil || c.bits == 0 {
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(c.bits)), c.w*c.h)
}

// setupBuffers (re)makes the frame buffers for frame scale q.
func (f *FullView) setupBuffers(q float64) bool {
	f.buf.Free()
	f.bg.Free()
	f.out.Free()
	f.bgFull.Free()
	f.art.Free()
	f.buf, f.bg, f.out, f.bgFull, f.art = nil, nil, nil, nil, nil
	f.skin.clock.free()
	f.skin.list.free()
	f.skin.centre.free()
	f.skin.bar.free()
	f.q = q
	f.W, f.H = int(math.Round(float64(f.winW)*q)), int(math.Round(float64(f.winH)*q))
	if q >= 0.999 {
		f.q, f.W, f.H = 1, f.winW, f.winH
	}
	f.buf = newCanvas(f.W, f.H)
	if f.buf != nil {
		setTextHint(f.buf.g, 3) // crisp clock, song info and hints
	}
	f.bg = newCanvas(max(16, f.W/4), max(16, f.H/4))
	f.bgFull = newCanvas(f.W, f.H)
	if f.q < 1 && !gpuUpscale {
		f.out = newCanvas(f.winW, f.winH)
	}
	f.bgSig, f.artSig, f.lyrX, f.lyrCX, f.lyrCY = "", "", 0, 0, 0
	if f.glow != nil {
		f.glow.mons = []RECT{{0, 0, int32(f.W), int32(f.H)}}
	}
	return f.buf != nil && f.bg != nil && (f.q == 1 || f.out != nil || gpuUpscale)
}

// paintBg puts the background on the frame. The small background is drawn at most 24 times a
// second and stretched (in Go, on all cores) into a full-size copy; every frame just copies that.
// A sharp cover is drawn only once per song.
func (f *FullView) paintBg(style int, sharp bool) {
	if f.videoOn && style == fsbVideo {
		f.paintVeil()
		return
	}
	still := sharp && (cover.img != 0 || f.hiImg != 0)
	sig := fmt.Sprint(style, sharp, cover.key, cover.ver, f.hiVer, cfg.FsPalette, cfg.FsTheme, cfg.SkinDim, albumAccent, f.glowKind, f.W)
	target := f.bgFull
	if target == nil { // no memory for the copy: straight onto the frame, every frame
		target = f.buf
		f.bgSig = ""
	}
	if sig != f.bgSig || (!still && f.t-f.bgAt >= 1.0/24) {
		f.bgSig, f.bgAt = sig, f.t
		if still {
			f.drawSharpCover(target.g, float32(f.W), float32(f.H))
			f.drawSharpCover(f.bg.g, float32(f.bg.w), float32(f.bg.h)) // the frosted panels read this one
		} else {
			f.drawBackground(style)
			scaleSmooth(pix(target), target.w, target.h, pix(f.bg), f.bg.w, f.bg.h, &f.scaleTmp)
		}
		f.bgStill = still
	}
	if target != f.buf {
		copy(pix(f.buf), pix(target))
	}
}

func strokeArc(g uintptr, c uint32, cx, cy, rx, ry, start, sweep, width float32) {
	p := newPen(c, width)
	drawArc(g, p, cx-rx, cy-ry, 2*rx, 2*ry, start, sweep)
	freePen(p)
}

func (f *FullView) drawBackground(style int) {
	b := f.bg
	g := b.g
	bw, bh := float32(b.w), float32(b.h)
	k := bw / float32(f.W) // frame px → background px
	cx, cy, R := f.cx*k, f.cy*k, f.radius*k
	if R == 0 {
		R = float32(math.Min(float64(bw), float64(bh))) * 0.2
	}
	t := f.t
	a, a2 := f.palette()
	bs := float64(f.bassS)
	if style == fsbVideo { // no video (yet): the blurred cover meanwhile
		style = fsbArt
	}
	if style == fsbArt && cover.img == 0 {
		style = fsbNebula // no picture to blur
	}
	switch style {
	case fsbArt: // the cover, blurred into a slowly drifting wash (Ken Burns)
		key := fmt.Sprint(cover.key, cover.ver, f.hiVer)
		if f.tiny == nil || f.tinyK != key {
			f.tiny.Free()
			f.tiny = newBitmap(20, 20)
			f.tinyK = key
			if f.tiny != nil {
				setInterp(f.tiny.g, interpHighQualityBicubic)
				drawCoverSquare(f.tiny.g, cover.img, 0, 0, 20)
			}
		}
		if f.tiny != nil {
			s := float32(1.35 + 0.1*math.Sin(t*0.06) + 0.05*bs)
			w := float32(math.Max(float64(bw), float64(bh))) * s
			dx := float32(math.Sin(t*0.041)) * bw * 0.08
			dy := float32(math.Cos(t*0.033)) * bh * 0.08
			setCopyMode(g, true)
			setInterp(g, interpHighQualityBicubic)
			gImage(g, f.tiny.bmp, (bw-w)/2+dx, (bh-w)/2+dy, w, w)
			setCopyMode(g, false)
			fillRect(g, 0x70000000, 0, 0, bw, bh)
			softGlow(g, cx, cy, R*2.2, withAlpha(a, 0.18+0.3*bs))
		}
	case fsbBlackHole:
		f.blackHole(g, bw, bh, cx, cy, R, a, a2, bs)
	case fsbAurora:
		fillRectGrad(g, 0, 0, bw, bh, 0, 0, 0, bh, 0xFF02040C, 0xFF0A1226)
		cols := []uint32{0xFF2BFFA8, 0xFF2BC8FF, 0xFF9B5CFF, a}
		if cfg.FsPalette > 0 || albumAccent != 0 {
			cols = []uint32{a, a2, hueShift(a, 0.18), hueShift(a2, -0.12)}
		}
		step := float32(math.Max(4, float64(bw)/40))
		for i, col := range cols {
			fi := float64(i)
			base := float64(bh) * (0.18 + 0.1*fi)
			for x := float32(0); x < bw; x += step {
				u := float64(x / bw)
				y := base + float64(bh)*0.08*math.Sin(u*5.5+t*(0.22+0.05*fi)+fi*1.7) + float64(bh)*0.04*math.Sin(u*13+t*0.5+fi)
				ln := float64(bh) * (0.22 + 0.12*math.Sin(u*3.2+t*0.3+fi*2) + 0.18*bs)
				al := 0.28 + 0.2*math.Sin(u*7+t*0.4+fi*3)
				fillRectGrad(g, x, float32(y), step+1, float32(ln), 0, float32(y), 0, float32(y+ln), withAlpha(col, 0.05), withAlpha(col, math.Max(0.05, al)))
				fillRectGrad(g, x, float32(y+ln), step+1, float32(ln*0.6), 0, float32(y+ln), 0, float32(y+ln*1.6), withAlpha(col, math.Max(0.05, al)), col&0x00FFFFFF)
			}
		}
		f.dust(g, bw, bh*0.55, 60, t)
	case fsbNebula:
		fillRect(g, 0xFF04040A, 0, 0, bw, bh)
		cols := []uint32{a, a2, hueShift(a, 0.2), hueShift(a, -0.15), mixColor(a2, 0xFFFFFFFF, 0.3)}
		for i, col := range cols {
			fi := float64(i)
			x := bw * float32(0.5+0.32*math.Sin(t*0.05*(1+fi*0.3)+fi*2.1))
			y := bh * float32(0.5+0.3*math.Cos(t*0.043*(1+fi*0.2)+fi*1.3))
			r := float32(math.Max(float64(bw), float64(bh))) * float32(0.32+0.08*math.Sin(t*0.1+fi)+0.05*bs)
			softGlow(g, x, y, r, withAlpha(col, 0.36))
		}
		f.dust(g, bw, bh, 90, t)
		softGlow(g, cx, cy, R*1.8, withAlpha(a, 0.12+0.28*bs))
	case fsbFlow: // two gradients turning slowly through the colours
		h := math.Mod(t*0.012, 1)
		c0, c1 := hueShift(a, h), hueShift(a2, h+0.3)
		ang := t * 0.07
		x0, y0 := bw/2+float32(math.Cos(ang))*bw*0.6, bh/2+float32(math.Sin(ang))*bh*0.6
		x1, y1 := bw/2-float32(math.Cos(ang))*bw*0.6, bh/2-float32(math.Sin(ang))*bh*0.6
		fillRectGrad(g, 0, 0, bw, bh, x0, y0, x1, y1, mixColor(c0, 0xFF000000, 0.45)|0xFF000000, mixColor(c1, 0xFF000000, 0.55)|0xFF000000)
		ang2 := -t * 0.05
		fillRectGrad(g, 0, 0, bw, bh, bw/2+float32(math.Cos(ang2))*bw*0.5, bh/2+float32(math.Sin(ang2))*bh*0.5,
			bw/2-float32(math.Cos(ang2))*bw*0.5, bh/2-float32(math.Sin(ang2))*bh*0.5, withAlpha(hueShift(c0, 0.5), 0.35), 0x00000000)
		softGlow(g, cx, cy, R*2.4, withAlpha(mixColor(c0, 0xFFFFFFFF, 0.3), 0.15+0.3*bs))
	case fsbStars:
		fillRectGrad(g, 0, 0, bw, bh, 0, 0, 0, bh, 0xFF020208, 0xFF070816)
		softGlow(g, cx, cy, R*3, withAlpha(a, 0.16+0.2*bs))
	default: // plain
		fillRect(g, mixColor(a, 0xFF000000, 0.92)|0xFF000000, 0, 0, bw, bh)
		softGlow(g, cx, cy, R*2.6, withAlpha(a, 0.16+0.22*bs))
	}
	if f.glowKind == 1 { // shadow + glow behind the cover / card (soft, so cheap to draw here)
		softGlow(g, cx, cy+R*0.08, R*1.3, 0xA0000000)
		softGlow(g, cx, cy, R*float32(1.45+0.3*bs), withAlpha(a, 0.3+0.45*bs))
	}
	// vignette: the edges fall off into the dark
	ex := bw * 0.78
	ey := bh * 0.78
	radialFill(g, bw/2, bh/2, ex, ey, 0x00000000, 0xA0000000)
	p := newPath()
	addEllipse(p, bw/2-ex, bh/2-ey, 2*ex, 2*ey)
	clipExclude(g, p)
	fillRect(g, 0xA0000000, 0, 0, bw, bh)
	clipReset(g)
	freePath(p)
}

// drawSharpCover: the cover filling the whole screen, darkened for the panels on top (once per song).
func (f *FullView) drawSharpCover(g uintptr, W, H float32) {
	img := cover.img
	if f.hiImg != 0 && f.hiKey == cover.key {
		img = f.hiImg
	}
	fillRect(g, 0xFF000000, 0, 0, W, H)
	if iw, ih := imageSize(img); iw > 0 && ih > 0 {
		sc := float32(math.Max(float64(W)/float64(iw), float64(H)/float64(ih)))
		dw, dh := float32(iw)*sc, float32(ih)*sc
		setInterp(g, interpHighQualityBicubic)
		gImage(g, img, (W-dw)/2, (H-dh)*0.35, dw, dh) // keep faces (usually in the upper half) in view
	}
	fillRect(g, withAlpha(0xFF000000, cfg.SkinDim), 0, 0, W, H)
	fillRectGrad(g, 0, 0, W*0.4, H, 0, 0, W*0.4, 0, 0x8C000000, 0x00000000)
	fillRectGrad(g, 0, H*0.6, W, H*0.4, 0, H*0.6, 0, H, 0x00000000, 0xA0000000)
	radialFill(g, W/2, H/2, W*0.8, H*0.8, 0x00000000, 0x90000000)
	if f.glowKind == 1 { // the card's shadow, baked in
		k := W / float32(f.W)
		softGlow(g, f.cx*k, f.cy*k+30*k, 300*k*float32(f.H)/1080, 0x90000000)
	}
}

// paintVeil: the frame over the music video — see-through, just darker toward the edges and
// behind the panels so everything on top stays readable (never fully clear: clicks stay ours).
func (f *FullView) paintVeil() {
	W, H := float32(f.W), float32(f.H)
	dim := cfg.SkinDim
	if cfg.FsKind == 0 {
		dim = 0.25
	}
	sig := fmt.Sprint(f.W, f.H, cfg.FsKind, dim)
	if f.veil == nil || f.veilSig != sig {
		f.veil.Free()
		f.veil = newCanvas(f.W, f.H)
		f.veilSig = sig
		if f.veil != nil {
			g := f.veil.g
			gClear(g, 0)
			fillRect(g, withAlpha(0xFF000000, math.Max(0.08, dim*0.8)), 0, 0, W, H)
			if cfg.FsKind == 1 {
				fillRectGrad(g, 0, 0, W*0.38, H, 0, 0, W*0.38, 0, 0x70000000, 0x00000000)
			}
			fillRectGrad(g, 0, H*0.62, W, H*0.38, 0, H*0.62, 0, H, 0x00000000, 0x90000000)
			fillRectGrad(g, 0, 0, W, H*0.25, 0, 0, 0, H*0.25, 0x50000000, 0x00000000)
		}
	}
	f.bgSig = "" // the normal background draws again when the video stops
	if f.veil != nil {
		copy(pix(f.buf), pix(f.veil))
	} else {
		gClear(f.buf.g, 0x30000000)
	}
}

// dust: tiny twinkling stars, always in the same places.
func (f *FullView) dust(g uintptr, bw, bh float32, n int, t float64) {
	s := uint32(7)
	for i := 0; i < n; i++ {
		s = hash32(s + 0x9E3779B9)
		x := float32(s%10000) / 10000 * bw
		s = hash32(s)
		y := float32(s%10000) / 10000 * bh
		s = hash32(s)
		tw := 0.35 + 0.35*math.Sin(t*(0.6+float64(s%100)/60)+float64(i))
		fillCircle(g, withAlpha(0xFFFFFFFF, tw), x, y, 0.45+float32(s%3)*0.2)
	}
}

// blackHole: a tilted accretion disc turning around a dark shadow, the far side bent over the top.
func (f *FullView) blackHole(g uintptr, bw, bh, cx, cy, R float32, a, a2 uint32, bs float64) {
	fillRect(g, 0xFF020205, 0, 0, bw, bh)
	f.dust(g, bw, bh, 110, f.t)
	hole := R * 0.62
	softGlow(g, cx, cy, hole*5, withAlpha(a, 0.22+0.25*bs))
	spin := float32(f.t * (14 + 50*bs))
	hot := mixColor(a2, 0xFFFFFFFF, 0.55) | 0xFF000000
	ring := func(front bool) {
		for i := 0; i < 16; i++ {
			fi := float32(i)
			rx := hole * (1.35 + fi*0.17)
			ry := rx * 0.22
			u := float64(i) / 15
			col := mixColor(hot, a, u)
			al := (1 - u*0.8) * (0.55 + 0.4*bs)
			w := hole * (0.16 - float32(u)*0.08)
			start := spin*(1.6-float32(u)) + fi*37
			lo, hi := float32(180), float32(360) // back half (top) behind the hole, front half over it
			if front {
				lo, hi = 0, 180
			}
			for s := 0; s < 3; s++ {
				st := float32(math.Mod(float64(start+float32(s)*120), 360))
				for _, off := range []float32{0, -360} { // an arc may wrap past 360°
					a0 := float32(math.Max(float64(st+off), float64(lo)))
					a1 := float32(math.Min(float64(st+80+off), float64(hi)))
					if a1 > a0 {
						strokeArc(g, withAlpha(col, al), cx, cy, rx, ry, a0, a1-a0, w)
					}
				}
			}
			strokeArc(g, withAlpha(col, al*0.25), cx, cy, rx, ry, map[bool]float32{true: 0, false: 180}[front], 180, w*0.7)
		}
	}
	ring(false)
	// lensed far side: the light from behind bends up and over the shadow
	for i := 0; i < 6; i++ {
		r := hole * (1.08 + float32(i)*0.07)
		strokeArc(g, withAlpha(mixColor(hot, a, float64(i)/6), 0.5-float64(i)*0.07+0.3*bs), cx, cy, r, r*0.92, 180, 180, hole*0.07)
	}
	fillCircle(g, 0xFF000000, cx, cy, hole)
	strokeCircle(g, withAlpha(hot, 0.85), cx, cy, hole*1.02, hole*0.05) // photon ring
	strokeCircle(g, withAlpha(a, 0.4), cx, cy, hole*1.1, hole*0.05)
	ring(true)
}

func (f *FullView) newStar(anywhere bool) fsStar {
	z := 1.0
	if anywhere {
		z = 0.05 + f.rng.Float64()*0.95
	}
	return fsStar{x: f.rng.Float64()*2 - 1, y: f.rng.Float64()*2 - 1, z: z, pz: z}
}

func (f *FullView) drawStars(g uintptr, dt float64) {
	W, H := float64(f.W), float64(f.H)
	speed := 0.12 + 0.9*float64(f.bassS)
	cx, cy := float64(f.cx), float64(f.cy)
	sc := math.Max(W, H) * 0.5
	for i := range f.stars {
		s := &f.stars[i]
		s.pz = s.z
		s.z -= dt * speed
		if s.z <= 0.03 {
			f.stars[i] = f.newStar(false)
			continue
		}
		x, y := cx+s.x/s.z*sc, cy+s.y/s.z*sc
		px, py := cx+s.x/s.pz*sc, cy+s.y/s.pz*sc
		if x < -20 || y < -20 || x > W+20 || y > H+20 {
			f.stars[i] = f.newStar(false)
			continue
		}
		b := math.Min(1, (1-s.z)*1.3)
		line(f.buf.g, withAlpha(0xFFFFFFFF, b*0.85), float32(0.6+1.8*(1-s.z)), float32(px), float32(py), float32(x), float32(y))
	}
	_ = g
}

func (f *FullView) drawRipples(g uintptr) {
	if len(f.ripples) == 0 {
		return
	}
	R := f.radius
	if R == 0 {
		R = float32(f.H) * 0.15
	}
	reach := float32(math.Hypot(float64(f.W), float64(f.H))) * 0.45
	c := f.accent()
	out := f.ripples[:0]
	for _, r := range f.ripples {
		u := (f.t - r.born) / 1.8
		if u >= 1 {
			continue
		}
		out = append(out, r)
		e := float32(1 - math.Pow(1-u, 3))
		rad := R*1.02 + (reach-R)*e
		strokeCircle(g, withAlpha(c, (1-u)*0.5), f.cx, f.cy, rad, float32(2+10*(1-u)))
	}
	f.ripples = out
}

func (f *FullView) drawArt(g uintptr) {
	S := int(f.radius * 2)
	img := cover.img
	if f.hiImg != 0 && f.hiKey == cover.key {
		img = f.hiImg
	}
	sig := fmt.Sprint(cover.key, cover.ver, f.hiVer, S, img == f.hiImg)
	if f.art == nil || f.artSig != sig {
		f.artSig = sig
		f.art.Free()
		f.art = newCanvas(S, S)
		if f.art != nil {
			ag := f.art.g
			setInterp(ag, interpHighQualityBicubic)
			rr := float32(S) * 0.035
			p := roundRectPath(0, 0, float32(S), float32(S), rr)
			clipPath(ag, p)
			if img != 0 {
				drawCoverSquare(ag, img, 0, 0, float32(S))
			} else {
				a, a2 := f.palette()
				fillRectGrad(ag, 0, 0, float32(S), float32(S), 0, 0, float32(S), float32(S), mixColor(a, 0xFF000000, 0.3)|0xFF000000, mixColor(a2, 0xFF000000, 0.6)|0xFF000000)
				fn := font("Segoe UI Symbol", float64(S)*0.3, false)
				tw, th := measure(fn, "♪")
				drawText(ag, fn, "♪", (float32(S)-tw)/2, (float32(S)-th)/2, tw+4, 0xCCFFFFFF)
			}
			fillRectGrad(ag, 0, 0, float32(S), float32(S)*0.5, 0, 0, 0, float32(S)*0.5, 0x22FFFFFF, 0x00FFFFFF)
			clipReset(ag)
			strokePath(ag, p, 0x30FFFFFF, 1.5)
			freePath(p)
		}
	}
	if f.art != nil { // copied, not resampled: the glow behind it (in the background) does the breathing
		f.blend(f.art, int(f.cx)-S/2, int(f.cy)-S/2, 255)
	}
}

func fmtTime(s float64) string {
	if s < 0 {
		s = 0
	}
	n := int(s)
	return fmt.Sprintf("%d:%02d", n/60, n%60)
}

func (f *FullView) drawInfo(g uintptr, snap MediaSnap) {
	t := snap.Track
	if t == nil {
		return
	}
	k := float32(f.H) / 1080
	acc := f.accent()
	prog := float32(0)
	if t.Duration > 0 {
		prog = float32(math.Max(0, math.Min(1, snap.Pos/t.Duration)))
	}
	big := font("Segoe UI Semibold", float64(30*k), false)
	small := font("Segoe UI", float64(21*k), false)
	tiny := font("Segoe UI", float64(15*k), false)
	if cfg.FsInfo == 1 { // bottom left: a small cover, the names and the time
		pad := 56 * k
		th := 96 * k
		x, y := pad, float32(f.H)-pad-th
		if img := cover.img; img != 0 {
			softGlow(g, x+th/2, y+th/2+4*k, th*0.7, 0x90000000)
			p := roundRectPath(x, y, th, th, 8*k)
			clipPath(g, p)
			drawCoverSquare(g, img, x, y, th)
			clipReset(g)
			freePath(p)
			x += th + 22*k
		}
		maxW := float32(f.W) * 0.32
		drawText(g, big, t.Title, x, y+4*k, maxW, 0xF2FFFFFF)
		drawText(g, small, t.Artist, x, y+44*k, maxW, 0xB0FFFFFF)
		if t.Duration > 0 {
			bw := float32(f.W) * 0.18
			by := y + th - 14*k
			fillRect(g, 0x40FFFFFF, x, by, bw, 3*k)
			fillRect(g, acc, x, by, bw*prog, 3*k)
			fillCircle(g, 0xFFFFFFFF, x+bw*prog, by+1.5*k, 5*k)
			drawText(g, tiny, fmtTime(snap.Pos)+" / "+fmtTime(t.Duration), x+bw+14*k, by-9*k, 200*k, 0x90FFFFFF)
		}
		return
	}
	// under the centre piece
	y := f.cy + f.radius + 34*k
	if cfg.FsMode == fsmLyrics {
		y = float32(f.H) * 0.78
	}
	center := func(fn *Font, s string, y float32, c uint32) {
		w, _ := measure(fn, s)
		mw := float32(f.W) * 0.6
		if w > mw {
			w = mw
		}
		drawText(g, fn, s, f.cx-w/2, y, mw, c)
	}
	center(big, t.Title, y, 0xF2FFFFFF)
	center(small, t.Artist, y+42*k, 0xB0FFFFFF)
	if t.Duration > 0 {
		bw := float32(math.Max(float64(f.radius)*1.4, float64(260*k)))
		by := y + 84*k
		fillRect(g, 0x38FFFFFF, f.cx-bw/2, by, bw, 3*k)
		fillRect(g, acc, f.cx-bw/2, by, bw*prog, 3*k)
		drawText(g, tiny, fmtTime(snap.Pos), f.cx-bw/2, by+8*k, 100*k, 0x80FFFFFF)
		drawTextRight(g, tiny, fmtTime(t.Duration), f.cx+bw/2, by+8*k, 100*k, 0x80FFFFFF)
	}
}

// infoBottom: where the info under the centre piece ends (lyrics go below it).
func (f *FullView) infoBottom() float32 {
	k := float32(f.H) / 1080
	if cfg.FsInfo == 2 && cfg.FsMode != fsmLyrics {
		return f.cy + f.radius + 34*k + 120*k
	}
	return f.cy + f.radius + 30*k
}

func (f *FullView) placeLyrics(now time.Time) {
	c := f.lyr.w.canvas
	W, H := float64(f.W), float64(f.H)
	hw, hh := float64(c.w)/2, float64(c.h)/2
	tx, ty := W/2, H/2
	pos := cfg.FsLyricsPos
	if cfg.FsMode == fsmLyrics && pos != 3 {
		pos = -1
	}
	if f.skinOn() {
		pos = 3
	}
	switch pos {
	case 0:
		ty = float64(f.infoBottom()) + hh*0.85
	case 1:
		ty = float64(f.cy-f.radius) - hh*0.85 - H*0.02
	case 2:
		ty = H*0.1 + hh*0.6
	case 3: // wandering: every line finds a new spot around the centre piece
		if f.lyr.idx != f.lyrIdx {
			f.lyrIdx = f.lyr.idx
			x0, y0, x1, y1 := 0.0, 0.0, W, H
			if f.wander[2] > 0 {
				x0, y0, x1, y1 = f.wander[0], f.wander[1], f.wander[2], f.wander[3]
			}
			f.lyrX = 0
			for try := 0; try < 24; try++ {
				x := x0 + hw + 40 + f.rng.Float64()*math.Max(1, x1-x0-2*hw-80)
				y := y0 + hh + 40 + f.rng.Float64()*math.Max(1, y1-y0-2*hh-80)
				r := float64(f.radius) * 1.1
				if math.Abs(x-float64(f.cx)) > hw+r || math.Abs(y-float64(f.cy)) > hh+r || r == 0 {
					f.lyrX, f.lyrY = x, y
					break
				}
			}
			if f.lyrX == 0 {
				f.lyrX, f.lyrY = (x0+x1)/2, y0+(y1-y0)*0.8
			}
		}
		tx, ty = f.lyrX, f.lyrY
	}
	tx = math.Max(hw, math.Min(W-hw, tx))
	ty = math.Max(hh, math.Min(H-hh, ty))
	if f.lyrCX == 0 && f.lyrCY == 0 || pos != 3 {
		f.lyrCX, f.lyrCY = tx, ty
		return
	}
	f.lyrCX += (tx - f.lyrCX) * 0.12 // glide
	f.lyrCY += (ty - f.lyrCY) * 0.12
}

func (f *FullView) drawClock(g uintptr, now time.Time) {
	k := float32(f.H) / 1080
	big := font("Segoe UI Light", float64(84*k), false)
	small := font("Segoe UI", float64(20*k), false)
	s := now.Format("15:04")
	x := float32(f.W) - 64*k
	y := 48 * k
	drawTextRight(g, big, s, x, y, 600*k, 0xEEFFFFFF)
	drawTextRight(g, small, formatDate(now, true), x-4*k, y+108*k, 600*k, 0x99FFFFFF)
}

func (f *FullView) pill(g uintptr, s string, cx, cy float32, alpha float64, fn *Font) {
	if alpha <= 0 {
		return
	}
	k := float32(f.H) / 1080
	w, h := measure(fn, s)
	pw, ph := w+44*k, h+20*k
	p := roundRectPath(cx-pw/2, cy-ph/2, pw, ph, ph/2)
	fillPathColor(g, p, withAlpha(0xFF101014, 0.78*alpha))
	strokePath(g, p, withAlpha(0xFFFFFFFF, 0.14*alpha), 1)
	freePath(p)
	drawText(g, fn, s, cx-w/2, cy-h/2, w+4, withAlpha(0xFFFFFFFF, 0.92*alpha))
}

func (f *FullView) drawHints(g uintptr, now time.Time) {
	k := float32(f.H) / 1080
	fn := font("Segoe UI", float64(17*k), false)
	if d := now.Sub(f.hintAt).Seconds(); d < 5 && f.skinOn() {
		a := math.Min(1, math.Min(d*4, (5-d)*2))
		f.pill(g, "Esc  exit    ·    Space  play/pause    ·    ←  →  previous / next    ·    /  search    ·    B  background    ·    M  middle    ·    C  clock    ·    T  clock colour    ·    L  lyrics    ·    F  edge light    ·    D  music display    ·    Q  speed",
			float32(f.W)/2, 46*k, a, fn)
	} else if d < 5 {
		a := math.Min(1, math.Min(d*4, (5-d)*2))
		f.pill(g, "Esc  exit    ·    A  mode    ·    B  background    ·    ←  →  lyrics style    ·    ↑  ↓  size    ·    L  lyrics    ·    F  frame    ·    Space  play/pause    ·    H  all keys",
			float32(f.W)/2, float32(f.H)-46*k, a, fn)
		if d < 5 && now.Sub(f.hintAt) < 5*time.Second && f.toast == "" {
			// first seconds: a second row with the rest
			f.pill(g, "D  desktop skin    ·    V  visualizer    ·    P  lyrics place    ·    E  text effect    ·    T  colours    ·    G  frame style    ·    C  clock    ·    I  info    ·    R  ripple    ·    S  settings    ·    Q  speed",
				float32(f.W)/2, float32(f.H)-96*k, a*0.9, fn)
		}
	}
	if f.toast != "" {
		d := now.Sub(f.toastAt).Seconds()
		if d > 1.8 {
			f.toast = ""
			return
		}
		a := math.Min(1, math.Min(d*6, (1.8-d)*3))
		f.pill(g, f.toast, float32(f.W)/2, 64*k, a, font("Segoe UI Semibold", float64(20*k), false))
	}
}

/* ---------- input (the window system turns its events into these) ---------- */

func (f *FullView) onKey(vk int) {
	if f.skinOn() {
		if f.skin.searchOn { // typing: the letters arrive as characters (↑ ↓ pick a suggestion)
			f.searchKey(vk)
			return
		}
		if !f.skinKey(vk) {
			switch vk {
			case 'H', 0x70:
				f.hintAt = time.Now()
			case 'S', 'Q', 0xBC, 0xBE, 'N':
				f.key(vk)
			}
		}
		return
	}
	f.key(vk)
}

func (f *FullView) onChar(r rune) {
	if f.skinOn() && f.skin.searchOn {
		f.skinChar(r)
	}
}

func (f *FullView) onDown(x, y float32) {
	if f.skinOn() {
		f.skinClick(x, y)
	}
}

func (f *FullView) onUp(x float32) {
	if f.skinOn() {
		f.seekUp(x)
	}
}

func (f *FullView) onDouble(x, y float32) {
	if f.skinOn() && f.skinClick(x, y) {
		return // a quick second click on a button is still a click
	}
	closeFullscreen()
}

func (f *FullView) onRight() {
	f.wake()
	showMenu()
	fsRefocus()
}

func (f *FullView) onWheel(x, y float32, d int) {
	if f.skinOn() && f.skinWheel(x, y, d > 0) {
		return
	}
	f.textSize(map[bool]int{true: 4, false: -4}[d > 0])
}

// onMove: pt in window pixels (to notice real moves), x, y in frame pixels.
func (f *FullView) onMove(pt POINT, x, y float32) {
	if pt == f.mousePt { // (Windows repeats mouse moves without a real move)
		return
	}
	f.mousePt = pt
	f.wake()
	if f.skinOn() {
		if f.skin.dragging {
			f.seekMove(x)
		} else {
			f.skinHover(x, y)
		}
	}
}
