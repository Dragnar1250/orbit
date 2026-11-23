//go:build windows || darwin

package main

// Desktop skin — the second kind of full screen: a big clock, "Continue playing", a now playing
// card (or a cover flow), a glass player bar with search, lyrics and a light round the edges.
// Every panel is drawn once into its own layer and only redrawn when what it shows changes.

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	vkMediaNext = 0xB0
	vkMediaPrev = 0xB1
)

type fsHit struct {
	x, y, w, h float32
	id         string
	arg        int
}

func (h fsHit) has(x, y float32) bool { return x >= h.x && y >= h.y && x < h.x+h.w && y < h.y+h.h }

type skinLayer struct {
	c    *Canvas
	sig  string
	x, y int
}

func (l *skinLayer) free() { l.c.Free(); l.c = nil; l.sig = "" }

// ensure gives a layer of the given size (reusing the old one when it fits).
func (l *skinLayer) ensure(w, h int) *Canvas {
	if l.c == nil || l.c.w != w || l.c.h != h {
		l.c.Free()
		l.c = newCanvas(w, h)
	} else {
		gClear(l.c.g, 0)
	}
	if l.c == nil {
		l.sig = "" // try again next frame
	} else {
		setTextHint(l.c.g, 3) // grid-fitted anti-aliasing: crisp small text
	}
	return l.c
}

type skinState struct {
	clock, list, centre, bar skinLayer
	drop                     skinLayer   // search suggestions
	searchBox                [4]float32  // the search field on the bar (x, y, w, h)
	sel                      int         // the highlighted suggestion (-1: none)
	shown                    []searchHit // the suggestions on screen
	hits                     []fsHit
	hover                    string
	hoverArg                 int
	search                   string
	searchOn                 bool
	pressed                  string
	pressedAt                time.Time
	thumbs                   map[string]uintptr // history covers
	upNext                   []HistItem         // songs that come next (known after going back)
	navUntil                 time.Time          // Orbit is stepping through songs: hold the song list
	listQuad                 [4][2]float64
	listSize                 [2]float64
	listHit                  func(x, y float32) int
	cardHits                 []fsHit
	cardQuads                map[int][4][2]float64
	spAsked                  time.Time
	dragging                 bool    // the time line is being dragged
	dragFrac                 float64 // where (0..1)
	seekX0, seekW            float32
	volUntil                 time.Time // the volume sign on the bar
	volDir                   int
}

func (s *skinState) free() {
	s.clock.free()
	s.list.free()
	s.centre.free()
	s.bar.free()
	s.drop.free()
	for _, img := range s.thumbs {
		freeImage(img)
	}
	s.thumbs = nil
}

func (f *FullView) skinOn() bool { return cfg.FsKind == 1 }

// thumb: the cover of a song in "Continue playing" (the playing one uses the live cover).
func (f *FullView) thumb(h HistItem) uintptr {
	if h.Key == cover.key && cover.img != 0 {
		return cover.img
	}
	if f.skin.thumbs == nil {
		f.skin.thumbs = map[string]uintptr{}
	}
	if img, ok := f.skin.thumbs[h.Key]; ok {
		return img
	}
	img := uintptr(0)
	if h.CoverFile != "" {
		img = imageFromFile(h.CoverFile)
	}
	f.skin.thumbs[h.Key] = img
	return img
}

// frost: the background behind a panel, blurred — the small background stretched up is
// already soft, so it is just drawn inside the panel.
func (f *FullView) frost(g uintptr, px, py, pw, ph float32, path uintptr) {
	if f.bg == nil || f.videoOn { // over the video: just the tinted glass
		return
	}
	clipPath(g, path)
	setInterp(g, interpBilinear)
	gImage(g, f.bg.bmp, -px, -py, float32(f.W), float32(f.H))
	clipReset(g)
}

// glass: a frosted rounded panel with a thin light edge.
func (f *FullView) glass(g uintptr, x, y, w, h, r, ox, oy float32) {
	p := roundRectPath(x, y, w, h, r)
	f.frost(g, ox+x, oy+y, w, h, p)
	a := cfg.SkinGlass
	fillPathColor(g, p, withAlpha(0xFF0E0E14, 0.25+0.45*a))
	fillRectGrad(g, x, y, w, h*0.5, 0, y, 0, y+h*0.5, withAlpha(0xFFFFFFFF, 0.07*a+0.03), 0x00FFFFFF)
	strokePath(g, p, withAlpha(0xFFFFFFFF, 0.1+0.12*a), 1.2)
	freePath(p)
}

func clockColours(mode int, acc uint32) (uint32, uint32, uint32) {
	switch mode {
	case 1:
		return 0xFFFFFFFF, 0xFFDCDCE6, 0x55FFFFFF
	case 2:
		return 0xFFFFD36B, 0xFFFF4D8D, 0xAAFF6A6A
	case 3:
		return 0xFFEFFAFF, 0xFF5AB8FF, 0x995AB8FF
	case 4:
		return 0xFFFF6BF2, 0xFF7A5CFF, 0xCCB45CFF
	}
	return mixColor(acc, 0xFFFFFFFF, 0.7) | 0xFF000000, acc | 0xFF000000, withAlpha(acc, 0.8)
}

func skinClockFont(i int) (string, bool) {
	switch i {
	case 1:
		return fontFor(0, 200, fontList)
	case 2:
		return fontFor(0, 900, fontList)
	case 3:
		return fontFor(2, 400, fontList)
	}
	return fontFor(1, 300, fontList)
}

/* ---------- drawing ---------- */

func (f *FullView) renderSkin(now time.Time, snap MediaSnap) {
	g := f.buf.g
	W, H := float32(f.W), float32(f.H)
	k := H / 1080
	sk := &f.skin
	sk.hits = sk.hits[:0]
	// songs come and go: don't collect covers forever. Only here, between frames — freeing one
	// while a panel is being drawn with it would crash.
	if len(sk.thumbs) > 24 {
		for key, img := range sk.thumbs {
			freeImage(img)
			delete(sk.thumbs, key)
		}
		sk.list.sig, sk.centre.sig = "", ""
	}

	// the centre piece (the lyrics keep away from it)
	f.cx, f.cy, f.radius = W*0.52, H*0.5, 0
	if cfg.SkinCentre != 2 {
		f.radius = 250 * k
	}
	top := float64(H * 0.1)
	if cfg.SkinClock {
		top = float64(H * 0.4) // below the clock
	}
	if cfg.SkinCentre == 1 { // seven cards: to the right of the list, under the clock
		cw := 230 * k * flowWidth(cfg.SkinFlowCount)
		f.cx, f.cy = float32(math.Max(float64(W)*0.575, float64(430*k+cw/2))), H*0.54
		top = float64(H * 0.7)
	}
	f.wander = [4]float64{float64(430 * k), top, float64(W - 40*k), float64(H - 150*k)}
	f.glowKind = 0
	if cfg.SkinCentre != 2 && snap.Track != nil {
		f.glowKind = 1
	}
	if cfg.SkinBg == 0 {
		f.paintBg(fsbArt, true)
	} else {
		f.paintBg(cfg.SkinBg-1, false)
	}
	if cfg.SkinBg == fsbStars+1 {
		f.drawStars(g, 1.0/float64(max(1, cfg.FsFPS)))
	}
	f.drawRipples(g)

	if cfg.SkinClock {
		f.skinClock(now, k)
	}
	if cfg.SkinList {
		f.skinList(snap, k)
	}
	switch cfg.SkinCentre {
	case 0:
		f.skinCard(snap, k)
	case 1:
		f.skinFlow(snap, k)
	}
	if cfg.SkinLyrics && f.lyr.w.visible && f.lyr.w.canvas != nil {
		f.placeLyrics(now)
		c := f.lyr.w.canvas
		f.blend(c, int(f.lyrCX)-c.w/2, int(f.lyrCY)-c.h/2, f.lyr.w.alpha)
	}
	if cfg.SkinBar {
		f.skinBar(now, snap, k)
	} else {
		sk.searchBox = [4]float32{W/2 - 115*k, H - 90*k, 230 * k, 40 * k}
	}
	f.skinDrop(snap, k)
	if cfg.SkinFrame {
		T := float32(90 * float64(f.H) / 1080 * cfg.GlowReach)
		f.glow.drawAll(g, RECT{0, 0, int32(f.W), int32(f.H)}, T)
	}
	f.drawHints(g, now)
}

func (f *FullView) put(l *skinLayer) {
	if l.c != nil {
		f.blend(l.c, l.x, l.y, 255)
	}
}

// the big clock in the top-right corner, with the date above it
func (f *FullView) skinClock(now time.Time, k float32) {
	l := &f.skin.clock
	main, ampm, _ := clockParts(now, cfg.Clock24h, false)
	date := ""
	if cfg.SkinDate {
		date = formatDate(now, false)
	}
	acc := f.accent()
	fam, bold := skinClockFont(cfg.SkinClockFont)
	sig := fmt.Sprint(main, ampm, date, cfg.SkinClockFont, cfg.SkinClockColor, acc, f.H)
	if sig != l.sig {
		l.sig = sig
		size := 300 * k
		if cfg.SkinClockFont != 0 {
			size = 230 * k
		}
		fn := font(fam, float64(size), bold)
		tw, th := measure(fn, main)
		pad := 40 * k
		cw, ch := int(tw+2*pad+80*k), int(th+2*pad+40*k)
		c := l.ensure(cw, ch)
		if c == nil {
			return
		}
		g := c.g
		top, bot, glow := clockColours(cfg.SkinClockColor, acc)
		p := newPath()
		x := float32(cw) - pad - tw
		if ampm != "" {
			x -= 70 * k
		}
		y := pad + 36*k
		addTextPath(p, fn, main, x, y)
		b := pathBounds(p)
		for i, w := range []float32{26, 14, 6} {
			pen := newPen(withAlpha(glow, []float64{0.06, 0.1, 0.16}[i]), w*k)
			drawPathPen(g, pen, p)
			freePen(pen)
		}
		fillPathGradient(g, p, top, bot, b.Y, b.Y+b.H)
		freePath(p)
		if ampm != "" {
			drawText(g, font(fam, float64(size*0.18), true), ampm, x+tw+10*k, y+th*0.62, 200*k, withAlpha(bot, 0.85))
		}
		if date != "" {
			drawTextRight(g, font("Segoe UI Semibold", float64(22*k), false), date, b.X+b.W*0.55, b.Y-34*k, 400*k, 0xD8FFFFFF)
		}
		l.x, l.y = f.W-cw-int(30*k), int(10*k)
	}
	f.put(l)
}

// gImagePart draws the part (sx,sy,sw,sh) of img into (x,y,w,h).
func gImagePart(g, img uintptr, x, y, w, h, sx, sy, sw, sh float32) {
	gImageSrc(g, img, x, y, w, h, sx, sy, sw, sh)
}

// shadowText: text with a soft dark halo, readable on any cover.
func shadowText(g uintptr, fn *Font, s string, x, y, maxW float32, c uint32) {
	drawText(g, fn, s, x+1, y+1.5, maxW, withAlpha(0xFF000000, 0.55*float64(c>>24)/255))
	drawText(g, fn, s, x, y, maxW, c)
}

// "Continue playing": the songs you played, on a panel tilted back like a page in space.
// Clicking one goes back to it in your player (no app opens).
func (f *FullView) skinList(snap MediaSnap, k float32) {
	l := &f.skin.list
	hist, ahead := f.listRows(snap)
	_, cur := f.pastSongs(snap)
	x, y := 34*k, float32(f.H)*0.16
	rowH := 62 * k
	pw := 360 * k
	top := 92 * k
	ph := top + rowH*float32(max(1, len(hist))) + 20*k
	hov := -1
	if f.skin.hover == "song" {
		hov = f.skin.hoverArg
	}
	keys := make([]string, len(hist))
	for i, h := range hist {
		keys[i] = h.Key
	}
	svc := serviceNames[serviceFor(cfg.SkinService, snap.Source)]
	angle := 0.32 // radians: the right edge leans back
	sig := fmt.Sprint(strings.Join(keys, "|"), cur, hov, ahead, f.spotifyOn(), cover.ver, f.accent(), cfg.SkinGlass, f.bgSig, f.bgStillBucket(), svc, f.H, f.videoOn)
	if sig != l.sig {
		l.sig = sig
		flat := newCanvas(int(pw)+2, int(ph)+2)
		if flat == nil {
			return
		}
		defer flat.Free()
		g := flat.g
		setTextHint(g, 3) // grid-fitted: crisp small text
		bp := roundRectPath(0, 0, pw, ph, 20*k)
		f.frost(g, x, y, pw, ph, bp)
		fillPathColor(g, bp, withAlpha(0xFF0E0E14, 0.35+0.35*cfg.SkinGlass))
		fillRectGrad(g, 0, 0, pw, ph*0.3, 0, 0, 0, ph*0.3, 0x14FFFFFF, 0x00FFFFFF)
		strokePath(g, bp, 0x38FFFFFF, 1.2)
		freePath(bp)
		hs := 50 * k
		hp := roundRectPath(14*k, 14*k, hs, hs, 12*k)
		if cover.img != 0 && snap.Track != nil {
			clipPath(g, hp)
			setInterp(g, interpHighQualityBicubic)
			drawCoverSquare(g, cover.img, 14*k, 14*k, hs)
			clipReset(g)
		} else {
			fillPathColor(g, hp, withAlpha(f.accent(), 0.6))
		}
		strokePath(g, hp, 0x40FFFFFF, 1)
		freePath(hp)
		hf := font("Segoe UI Semibold", float64(21*k), false)
		head, sub := "Up Next", "Next on "+svc
		if !ahead {
			head, sub = "Recently Played", "Connect Spotify in Settings to see what's next"
			if f.spotifyOn() {
				sub = "Waiting for Spotify's queue…"
			}
		}
		shadowText(g, hf, head, 78*k, 16*k, pw, 0xF5FFFFFF)
		tw, _ := measure(hf, head)
		if fi := iconFont(); fi != "" {
			shadowText(g, font(fi, float64(11*k), false), "\uE70D", 78*k+tw+8*k, 26*k, 30*k, 0xB0FFFFFF)
		}
		shadowText(g, font("Segoe UI", float64(14*k), false), sub, 78*k, 44*k, pw-90*k, 0xA0FFFFFF)
		tf, af := font("Segoe UI Semibold", float64(16.5*k), false), font("Segoe UI", float64(13.5*k), false)
		if len(hist) == 0 {
			shadowText(g, af, "Songs you play show up here", 18*k, top+10*k, pw-40*k, 0x99FFFFFF)
		}
		for i, h := range hist {
			ry := top + rowH*float32(i)
			if h.Key == cur || i == hov {
				p := roundRectPath(8*k, ry, pw-16*k, rowH-6*k, 14*k)
				a := 0.1
				if h.Key == cur {
					a = 0.2
				}
				fillPathColor(g, p, withAlpha(0xFFFFFFFF, a))
				strokePath(g, p, 0x44FFFFFF, 1)
				freePath(p)
			}
			ts := 46 * k
			tx, ty := 16*k, ry+(rowH-6*k-ts)/2
			p := roundRectPath(tx, ty, ts, ts, 8*k)
			if img := f.thumb(h); img != 0 {
				clipPath(g, p)
				setInterp(g, interpHighQualityBicubic)
				drawCoverSquare(g, img, tx, ty, ts)
				clipReset(g)
			} else {
				fillPathColor(g, p, 0x30FFFFFF)
			}
			freePath(p)
			shadowText(g, tf, h.Title, tx+ts+14*k, ty+2*k, pw-ts-60*k, 0xF5FFFFFF)
			artist := h.Artist
			if h.Key == cur {
				artist = "Now playing · " + artist
			}
			shadowText(g, af, artist, tx+ts+14*k, ty+25*k, pw-ts-60*k, 0xA0FFFFFF)
			if ahead && i > 0 { // the step: how many songs ahead
				shadowText(g, font("Segoe UI Semibold", float64(12*k), false), fmt.Sprint(i), pw-34*k, ry+(rowH-6*k)/2-9*k, 24*k, 0x70FFFFFF)
			}
		}
		// tilt it: the flat panel drawn in perspective onto the layer
		quad := tiltQuad(0, 0, float64(pw), float64(ph), angle, float64(1500*k))
		minX, minY, maxX, maxY := quadBounds(quad)
		c := l.ensure(int(maxX-minX)+4, int(maxY-minY)+4)
		if c == nil {
			return
		}
		for i := range quad {
			quad[i][0] -= minX - 2
			quad[i][1] -= minY - 2
		}
		warpOnto(pix(c), c.w, c.h, pix(flat), flat.w, flat.h, quad, 1)
		l.x, l.y = int(x+float32(minX)-2), int(y+float32(minY)-2)
		f.skin.listQuad = quad
		f.skin.listSize = [2]float64{float64(pw), float64(ph)}
	}
	// clicks: back into the flat panel through the inverse of the tilt
	if inv, ok := squareToQuad(f.skin.listQuad).inverse(); ok {
		lx, ly := float64(l.x), float64(l.y)
		f.skin.listHit = func(px, py float32) int {
			u, v := inv.apply(float64(px)-lx, float64(py)-ly)
			if u < 0 || u > 1 || v < 0 || v > 1 {
				return -2
			}
			fy := v * f.skin.listSize[1]
			i := int((fy - float64(top)) / float64(rowH))
			if fy < float64(top) || i >= len(hist) {
				return -1
			}
			return i
		}
	}
	f.put(l)
}

func quadBounds(q [4][2]float64) (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = q[0][0], q[0][1], q[0][0], q[0][1]
	for _, p := range q {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	return
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

// bgStillBucket: panels over a moving background refresh their frost every few seconds.
func (f *FullView) bgStillBucket() int {
	if f.bgStill {
		return 0
	}
	return int(f.t / 3)
}

// the now playing card in the middle
func (f *FullView) skinCard(snap MediaSnap, k float32) {
	l := &f.skin.centre
	title, artist := "Not playing", "Play something in your music app"
	if snap.Track != nil {
		title, artist = snap.Track.Title, snap.Track.Artist
	}
	svc := serviceLabel(cfg.SkinService, snap.Source)
	img := cover.img
	if f.hiImg != 0 && f.hiKey == cover.key {
		img = f.hiImg
	}
	cw, ch := 380*k, 470*k
	x, y := int(f.cx-cw/2), int(f.cy-ch/2)
	sig := fmt.Sprint(title, artist, svc, cover.key, cover.ver, f.hiVer, img != 0, f.accent(), cfg.SkinGlass, f.bgSig, f.bgStillBucket(), snap.Playing)
	if sig != l.sig {
		l.sig = sig
		c := l.ensure(int(cw)+2, int(ch)+2)
		if c == nil {
			return
		}
		g := c.g
		f.glass(g, 0, 0, cw, ch, 26*k, float32(x), float32(y))
		cs := 300 * k
		cx := (cw - cs) / 2
		softGlow(g, cw/2, 24*k+cs/2+10*k, cs*0.62, 0x70000000)
		p := roundRectPath(cx, 24*k, cs, cs, 20*k)
		if img != 0 {
			clipPath(g, p)
			setInterp(g, interpHighQualityBicubic)
			drawCoverSquare(g, img, cx, 24*k, cs)
			clipReset(g)
		} else {
			a, b := f.palette()
			fillRectGrad(g, cx, 24*k, cs, cs, cx, 24*k, cx+cs, 24*k+cs, mixColor(a, 0xFF000000, 0.3)|0xFF000000, mixColor(b, 0xFF000000, 0.6)|0xFF000000)
			fn := font("Segoe UI Symbol", float64(cs*0.3), false)
			tw, th := measure(fn, "♪")
			drawText(g, fn, "♪", cw/2-tw/2, 24*k+cs/2-th/2, tw+4, 0xCCFFFFFF)
		}
		strokePath(g, p, 0x40FFFFFF, 1.5)
		freePath(p)
		centre := func(fn *Font, s string, y float32, col uint32) {
			w, _ := measure(fn, s)
			if w > cw-40*k {
				w = cw - 40*k
			}
			drawText(g, fn, s, cw/2-w/2, y, cw-40*k, col)
		}
		centre(font("Segoe UI Semibold", float64(25*k), false), title, 342*k, 0xF5FFFFFF)
		centre(font("Segoe UI", float64(17*k), false), artist, 378*k, 0xAAFFFFFF)
		// "Playing on …" pill
		pf := font("Segoe UI", float64(13*k), false)
		label := "Paused · " + svc
		if snap.Playing {
			label = "Playing on " + svc
		}
		tw, _ := measure(pf, label)
		pw := tw + 44*k
		px := cw/2 - pw/2
		pp := roundRectPath(px, 418*k, pw, 30*k, 15*k)
		fillPathColor(g, pp, 0x22FFFFFF)
		freePath(pp)
		fillCircle(g, serviceColors[serviceFor(cfg.SkinService, snap.Source)], px+17*k, 433*k, 5*k)
		drawText(g, pf, label, px+30*k, 424*k, tw+8, 0xCCFFFFFF)
		l.x, l.y = x, y
	}
	if snap.Track != nil {
		f.skin.hits = append(f.skin.hits, fsHit{float32(l.x), float32(l.y), cw, 330 * k, "play", 0}) // the cover: play / pause right here
	}
	f.put(l)
}

// flowCard draws one cover-flow card (frosted frame, cover, two lines) into a canvas.
func (f *FullView) flowCard(img uintptr, title, sub string, cs, k float32, front bool, dim float64) *Canvas {
	pad := cs * 0.045
	capH := cs * 0.24
	w, h := cs+2*pad, cs+2*pad+capH
	b := newCanvas(int(w)+1, int(h)+1)
	if b == nil {
		return nil
	}
	g := b.g
	setTextHint(g, 3)
	p := roundRectPath(0.5, 0.5, w-1, h-1, cs*0.07)
	fillPathGradient(g, p, 0xE8302A30, 0xE8141218, 0, h)
	if front {
		fillPathGradient(g, p, withAlpha(f.accent(), 0.24), withAlpha(f.accent(), 0.05), 0, h)
	}
	fillRectGrad(g, 0, 0, w, h*0.3, 0, 0, 0, h*0.3, 0x22FFFFFF, 0x00FFFFFF)
	strokePath(g, p, 0x60FFFFFF, 1.4)
	freePath(p)
	cp := roundRectPath(pad, pad, cs, cs, cs*0.045)
	if img != 0 {
		clipPath(g, cp)
		setInterp(g, interpHighQualityBicubic)
		drawCoverSquare(g, img, pad, pad, cs)
		clipReset(g)
	} else {
		fillPathGradient(g, cp, 0xFF3A3A46, 0xFF1A1A22, pad, pad+cs)
		fn := font("Segoe UI Symbol", float64(cs*0.28), false)
		tw, th := measure(fn, "♪")
		drawText(g, fn, "♪", pad+cs/2-tw/2, pad+cs/2-th/2, tw+4, 0x66FFFFFF)
	}
	strokePath(g, cp, 0x30FFFFFF, 1)
	freePath(cp)
	tf := font("Segoe UI Semibold", float64(cs*0.068), false)
	sf := font("Segoe UI", float64(cs*0.054), false)
	center := func(fn *Font, s string, y float32, c uint32) {
		tw, _ := measure(fn, s)
		if tw > w-2*pad {
			tw = w - 2*pad
		}
		drawText(g, fn, s, w/2-tw/2, y, w-2*pad, c)
	}
	center(tf, strings.ToUpper(title), pad+cs+capH*0.14, 0xF2FFFFFF)
	center(sf, sub, pad+cs+capH*0.54, 0xA0FFFFFF)
	if dim > 0 { // cards further back are darker
		dp := roundRectPath(0, 0, w, h, cs*0.07)
		fillPathColor(g, dp, withAlpha(0xFF000000, dim))
		freePath(dp)
	}
	return b
}

// flowSlot: one card of the cover flow. d < 0: before the playing song, d > 0: after.
type flowSlot struct {
	d             int
	item          HistItem
	img           uintptr
	title, artist string
	back, ahead   int // clicking it: go back this many songs / skip ahead this many
}

// flowSlots: three songs before the playing one on the left; on the right the songs that come
// next when Orbit knows them (after you went back), otherwise more of the earlier ones.
func (f *FullView) flowSlots(snap MediaSnap) []flowSlot {
	hist, cur := f.pastSongs(snap)
	start := 0
	if len(hist) > 0 && hist[0].Key == cur {
		start = 1
	}
	var past []HistItem
	for _, h := range hist[start:] {
		if h.Key != cur {
			past = append(past, h)
		}
	}
	var out []flowSlot
	n := cfg.SkinFlowCount
	for i := 0; i < n && i < len(past); i++ {
		out = append(out, flowSlot{d: -(i + 1), item: past[i], img: f.thumb(past[i]), title: past[i].Title, artist: past[i].Artist, back: i + 1})
	}
	if next := f.nextSongs(snap); len(next) > 0 {
		for j := 0; j < n && j < len(next); j++ {
			u := next[j]
			out = append(out, flowSlot{d: j + 1, item: u, img: f.thumb(u), title: u.Title, artist: u.Artist, ahead: j + 1})
		}
	} else {
		for j := 0; j < n && n+j < len(past); j++ {
			h := past[n+j]
			out = append(out, flowSlot{d: j + 1, item: h, img: f.thumb(h), title: h.Title, artist: "Earlier · " + h.Artist, back: n + 1 + j})
		}
	}
	return out
}

// cover flow: three songs on each side of the playing one, turned toward it in real perspective.
// Click a card to go to that song.
func (f *FullView) skinFlow(snap MediaSnap, k float32) {
	l := &f.skin.centre
	slots := f.flowSlots(snap)
	cs := 230 * k
	cw, ch := cs*flowWidth(cfg.SkinFlowCount), cs*1.62
	x, y := int(f.cx-cw/2), int(f.cy-ch/2)
	keys := []string{}
	for _, s := range slots {
		keys = append(keys, fmt.Sprint(s.d, s.item.Key, s.artist))
	}
	title, artist := "Not playing", "Play something in your music app"
	if snap.Track != nil {
		title, artist = snap.Track.Title, snap.Track.Artist
	}
	hov := -99 // pointing at a card is drawn on top each frame (below), not baked in: no rebuild per move
	sig := fmt.Sprint(strings.Join(keys, "|"), title, cover.ver, f.hiVer, f.H, f.accent())
	type cardHit struct {
		box  [4]float32
		slot int
	}
	if sig != l.sig {
		l.sig = sig
		c := l.ensure(int(cw), int(ch))
		if c == nil {
			return
		}
		g := c.g
		mid := float64(ch) * 0.45
		var hits []fsHit
		f.skin.cardQuads = map[int][4][2]float64{}
		draw := func(s flowSlot, front bool) {
			a := abs(s.d)
			sc := []float64{1, 0.8, 0.64, 0.52}[a]
			li := []float64{1, 0.78, 0.6, 0.44}[a]
			if s.d == hov && !front {
				li = math.Min(1, li+0.18) // pointed at: a little brighter
			}
			card := f.flowCard(s.img, s.title, s.artist, cs, k, front, 1-li)
			if card == nil {
				return
			}
			defer card.Free()
			w, h := float64(card.w)*sc, float64(card.h)*sc
			dx := []float64{0, 1.02, 1.82, 2.48}[a] * float64(cs)
			if s.d < 0 {
				dx = -dx
			}
			px := float64(cw)/2 + dx - w/2
			py := mid - h/2
			ang := 0.0
			if s.d != 0 {
				ang = 0.62 * float64(sign(s.d)) // turned toward the middle
			}
			q := tiltQuad(px, py, w, h, ang, float64(1100*k))
			minX, minY, maxX, maxY := quadBounds(q)
			softGlow(g, float32((minX+maxX)/2), float32(maxY)-float32(h)*0.04, float32(w)*0.58, 0x80000000)
			warpOnto(pix(c), c.w, c.h, pix(card), card.w, card.h, q, 1)
			// a faint reflection on the floor
			refl := [4][2]float64{{q[3][0], q[3][1] + 4}, {q[2][0], q[2][1] + 4}, {q[2][0], q[2][1] + 4 + h*0.18}, {q[3][0], q[3][1] + 4 + h*0.18}}
			fillRectGrad(g, float32(refl[0][0]), float32(refl[0][1]), float32(refl[1][0]-refl[0][0]), float32(h*0.18), 0, float32(refl[0][1]), 0, float32(refl[0][1]+h*0.18), withAlpha(0xFFFFFFFF, 0.05*li), 0x00FFFFFF)
			hits = append(hits, fsHit{float32(minX) + float32(x), float32(minY) + float32(y), float32(maxX - minX), float32(maxY - minY), "card", s.d})
			f.skin.cardQuads[s.d] = q
		}
		for _, want := range []int{3, 2, 1} { // far ones first
			for _, s := range slots {
				if abs(s.d) == want {
					draw(s, false)
				}
			}
		}
		img := cover.img
		if f.hiImg != 0 && f.hiKey == cover.key {
			img = f.hiImg
		}
		draw(flowSlot{d: 0, img: img, title: title, artist: artist}, true)
		// page dots
		n := len(slots) + 1
		dw := 10 * k
		dx0 := cw/2 - float32(n-1)*dw/2
		nLeft := 0
		for _, s := range slots {
			if s.d < 0 {
				nLeft++
			}
		}
		for i := 0; i < n; i++ {
			col, r := uint32(0x55FFFFFF), 2.6*k
			if i == nLeft {
				col, r = 0xEEFFFFFF, 3.6*k
			}
			fillCircle(g, col, dx0+float32(i)*dw, ch-12*k, r)
		}
		l.x, l.y = x, y
		f.skin.cardHits = hits
	}
	// in drawing order: hitAt looks from the end, so the front card wins
	f.skin.hits = append(f.skin.hits, f.skin.cardHits...)
	f.put(l)
	if f.skin.hover == "card" { // the card under the mouse: a light outline along its tilted edges
		if q, ok := f.skin.cardQuads[f.skin.hoverArg]; ok {
			ox, oy := float32(l.x), float32(l.y)
			for i := 0; i < 4; i++ {
				a, b := q[i], q[(i+1)%4]
				line(f.buf.g, withAlpha(f.accent(), 0.9), 2.2*k, ox+float32(a[0]), oy+float32(a[1]), ox+float32(b[0]), oy+float32(b[1]))
			}
		}
	}
}

// flowWidth: the cover flow's width in cover sizes, for n cards on each side.
func flowWidth(n int) float32 {
	return []float32{1.4, 3.3, 4.8, 5.9}[clampI(n, 0, 3)]
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// the player bar at the bottom: the song on the left, a big round play button in the middle with
// the time line under it, search and the music app on the right — frosted, with a soft shadow.
func (f *FullView) skinBar(now time.Time, snap MediaSnap, k float32) {
	l := &f.skin.bar
	sk := &f.skin
	bw := float32(math.Min(float64(940*k), float64(f.W)*0.64))
	bh := 84 * k
	m := 26 * k // room for the shadow around it
	x, y := int((float32(f.W)-bw)/2-m), int(float32(f.H)-bh-36*k-m)
	prog, pos, dur := 0.0, snap.Pos, 0.0
	title, artist := "Not Playing", "Play something in your music app"
	if t := snap.Track; t != nil {
		title, artist = t.Title, t.Artist
		dur = t.Duration
		if dur > 0 {
			prog = math.Max(0, math.Min(1, pos/dur))
		}
	}
	if sk.dragging && dur > 0 { // dragging along the time line: show where it will land
		prog = sk.dragFrac
		pos = prog * dur
	}
	vol := now.Before(sk.volUntil)
	shuf, rep, canShuf, canRep := f.modes(snap)
	svc := serviceFor(cfg.SkinService, snap.Source)
	caret := sk.searchOn && now.UnixMilli()/530%2 == 0
	pressed := ""
	if now.Sub(sk.pressedAt) < 180*time.Millisecond {
		pressed = sk.pressed
	}
	hov := sk.hover
	cx := bw / 2
	lineW := bw * 0.34
	sx := bw - 300*k // search field
	sig := fmt.Sprint(int(prog*float64(lineW)), int(pos), snap.Playing, sk.dragging, vol, sk.volDir, shuf, rep, canShuf, canRep, title, artist, hov, pressed, sk.search, sk.searchOn, caret, svc, cover.ver, f.accent(), cfg.SkinGlass, f.bgSig, f.bgStillBucket(), f.videoOn)
	icon := iconFont()
	if sig != l.sig {
		l.sig = sig
		c := l.ensure(int(bw+2*m)+2, int(bh+2*m)+2)
		if c == nil {
			return
		}
		g := c.g
		// shadow, then the frosted body with a light rim on top
		for i, a := range []float64{0.18, 0.14, 0.1} {
			d := float32(i+1) * 7 * k
			sp := roundRectPath(m-d/2, m+d*0.6, bw+d, bh+d*0.4, bh/2+d)
			fillPathColor(g, sp, withAlpha(0xFF000000, a))
			freePath(sp)
		}
		gTranslate(g, m, m)
		body := roundRectPath(0, 0, bw, bh, bh/2)
		f.frost(g, float32(x)+m, float32(y)+m, bw, bh, body)
		fillPathGradient(g, body, withAlpha(0xFF24222C, 0.55+0.35*cfg.SkinGlass), withAlpha(0xFF0E0E14, 0.6+0.35*cfg.SkinGlass), 0, bh)
		fillRectGrad(g, 0, 0, bw, bh*0.5, 0, 0, 0, bh*0.5, 0x1CFFFFFF, 0x00FFFFFF)
		strokePath(g, body, 0x50FFFFFF, 1.2)
		freePath(body)
		// left: the song
		ts := 56 * k
		tx, ty := 14*k, (bh-ts)/2
		p := roundRectPath(tx, ty, ts, ts, 14*k)
		if cover.img != 0 && snap.Track != nil {
			clipPath(g, p)
			setInterp(g, interpHighQualityBicubic)
			drawCoverSquare(g, cover.img, tx, ty, ts)
			clipReset(g)
		} else {
			fillPathGradient(g, p, withAlpha(f.accent(), 0.8), 0xFF1A1A22, ty, ty+ts)
		}
		strokePath(g, p, 0x40FFFFFF, 1)
		freePath(p)
		infoW := cx - 140*k - (tx + ts + 14*k)
		if hov == "song-info" {
			hp := roundRectPath(8*k, 8*k, ts+infoW+30*k, bh-16*k, (bh-16*k)/2)
			fillPathColor(g, hp, 0x14FFFFFF)
			freePath(hp)
		}
		drawText(g, font("Segoe UI Semibold", float64(16*k), false), title, tx+ts+14*k, bh/2-21*k, infoW, 0xF5FFFFFF)
		drawText(g, font("Segoe UI", float64(13*k), false), artist, tx+ts+14*k, bh/2+1*k, infoW, 0xA0FFFFFF)
		// middle: previous, a big round play / pause, next — and the time line under them
		cy := bh * 0.42
		glyph := func(cx, cy float32, g1, alt string, size float32, col uint32) {
			fam, t := icon, g1
			if fam == "" {
				fam, t = "Segoe UI Symbol", alt
			}
			fn := font(fam, float64(size*k), false)
			tw, th := measure(fn, t)
			drawText(g, fn, t, cx-tw/2, cy-th/2, tw+4, col)
		}
		side := func(id string, x float32, g1, alt string) {
			if hov == id || pressed == id {
				a := 0.12
				if pressed == id {
					a = 0.26
				}
				fillCircle(g, withAlpha(0xFFFFFFFF, a), x, cy, 19*k)
			}
			glyph(x, cy, g1, alt, 15, 0xE8FFFFFF)
		}
		side("prev", cx-58*k, "\uE892", "⏮")
		side("next", cx+58*k, "\uE893", "⏭")
		// shuffle and repeat: lit in the accent colour when on, with a dot under them
		mode := func(id string, x float32, g1, alt string, on, can bool) {
			if hov == id || pressed == id {
				fillCircle(g, withAlpha(0xFFFFFFFF, map[bool]float64{true: 0.24, false: 0.11}[pressed == id]), x, cy, 17*k)
			}
			col := uint32(0xC8FFFFFF)
			switch {
			case on:
				col = mixColor(f.accent(), 0xFFFFFFFF, 0.25) | 0xFF000000
			case !can:
				col = 0x50FFFFFF
			}
			glyph(x, cy, g1, alt, 13, col)
			if on {
				fillCircle(g, col, x, cy+13*k, 2*k)
			}
		}
		mode("shuffle", cx-112*k, "\uE8B1", "⤮", shuf == 1, canShuf)
		rg, ra := "\uE8EE", "🔁"
		if rep == 1 {
			rg, ra = "\uE8ED", "🔂"
		}
		mode("repeat", cx+112*k, rg, ra, rep > 0, canRep)
		pr := 23 * k
		if hov == "play" {
			pr = 24.5 * k
		}
		if pressed == "play" {
			pr = 21 * k
		}
		softGlow(g, cx, cy+3*k, pr*1.7, withAlpha(f.accent(), 0.35))
		fillCircle(g, 0xFFFFFFFF, cx, cy, pr)
		play, alt := "\uE768", "▶"
		if snap.Playing {
			play, alt = "\uE769", "⏸"
		}
		glyph(cx+1*k, cy, play, alt, 17, 0xFF101014)
		ly := bh - 15*k
		lx0 := cx - lineW/2
		tf := font("Segoe UI", float64(11*k), false)
		if dur > 0 {
			drawTextRight(g, tf, fmtTime(pos), lx0-10*k, ly-7*k, 60*k, 0x99FFFFFF)
			drawText(g, tf, fmtTime(dur), lx0+lineW+10*k, ly-7*k, 60*k, 0x99FFFFFF)
		}
		lh := 3 * k // thicker under the mouse: it can be clicked and dragged
		big := (hov == "seek" || sk.dragging) && dur > 0
		if big {
			lh = 5 * k
		}
		lp := roundRectPath(lx0, ly-lh/2, lineW, lh, lh/2)
		fillPathColor(g, lp, 0x30FFFFFF)
		freePath(lp)
		if prog > 0 {
			fp := roundRectPath(lx0, ly-lh/2, lineW*float32(prog), lh, lh/2)
			fillPathGradient(g, fp, mixColor(f.accent(), 0xFFFFFFFF, 0.3)|0xFF000000, f.accent(), ly-lh/2, ly+lh/2)
			freePath(fp)
		}
		if prog > 0 || big {
			r := 4 * k
			if big {
				r = 6.5 * k
				softGlow(g, lx0+lineW*float32(prog), ly, 14*k, withAlpha(f.accent(), 0.5))
			}
			fillCircle(g, 0xFFFFFFFF, lx0+lineW*float32(prog), ly, r)
		}
		if vol { // the wheel over the bar changed the volume
			vt := ""
			if sk.volDir < 0 {
				vt = ""
			}
			if icon != "" {
				glyph(lx0+lineW+62*k, ly, vt, "", 11, withAlpha(f.accent(), 1)|0xFF000000)
			}
		}
		// right: search, and the music app it searches in
		fh := 40 * k
		fw := 230 * k
		fp := roundRectPath(sx, (bh-fh)/2, fw, fh, fh/2)
		fa := 0.09
		if sk.searchOn {
			fa = 0.18
		} else if hov == "search" {
			fa = 0.14
		}
		fillPathColor(g, fp, withAlpha(0xFFFFFFFF, fa))
		if sk.searchOn {
			strokePath(g, fp, withAlpha(f.accent(), 0.9), 1.4)
		} else {
			strokePath(g, fp, 0x22FFFFFF, 1)
		}
		freePath(fp)
		glyph(sx+20*k, bh/2, "\uE721", "⌕", 13, 0xB0FFFFFF)
		sf := font("Segoe UI", float64(14*k), false)
		txt, col := sk.search, uint32(0xF0FFFFFF)
		if txt == "" && !sk.searchOn {
			txt, col = "Search "+serviceNames[svc], 0x80FFFFFF
		}
		tw, th := measure(sf, txt)
		room := fw - 52*k
		tx2 := sx + 38*k
		if tw > room {
			tx2 -= tw - room
			clipRect(g, sx+36*k, 0, room+4*k, bh)
		}
		drawText(g, sf, txt, tx2, bh/2-th/2, tw+8, col)
		clipReset(g)
		if caret {
			ccx := tx2 + tw + 1.5*k
			if sk.search == "" {
				ccx = sx + 38*k
			}
			fillRect(g, 0xF0FFFFFF, ccx, bh/2-9*k, 1.5*k, 18*k)
		}
		dx := bw - 36*k
		if hov == "svc" {
			fillCircle(g, 0x22FFFFFF, dx, bh/2, 21*k)
		}
		softGlow(g, dx, bh/2, 22*k, withAlpha(serviceColors[svc], 0.35))
		fillCircle(g, serviceColors[svc], dx, bh/2, 14*k)
		lf := font("Segoe UI Semibold", float64(13*k), false)
		letter := string([]rune(serviceNames[svc])[:1])
		lw, lh := measure(lf, letter)
		drawText(g, lf, letter, dx-lw/2, bh/2-lh/2, lw+4, 0xFF101010)
		gReset(g)
		l.x, l.y = x, y
	}
	X, Y := float32(l.x)+m, float32(l.y)+m
	cy := bh * 0.42
	sk.hits = append(sk.hits,
		fsHit{X - m/2, Y - m/2, bw + m, bh + m, "bar", 0}, // the bar itself, under its buttons (wheel = volume)
		fsHit{X + 8*k, Y + 8*k, cx - 140*k, bh - 16*k, "song-info", 0},
		fsHit{X + cx - 132*k, Y + cy - 19*k, 38 * k, 38 * k, "shuffle", 0},
		fsHit{X + cx + 93*k, Y + cy - 19*k, 38 * k, 38 * k, "repeat", 0},
		fsHit{X + cx - 80*k, Y + cy - 20*k, 40 * k, 40 * k, "prev", 0},
		fsHit{X + cx + 38*k, Y + cy - 20*k, 40 * k, 40 * k, "next", 0},
		fsHit{X + cx - 26*k, Y + cy - 26*k, 52 * k, 52 * k, "play", 0},
		fsHit{X + sx, Y + (bh-40*k)/2, 230 * k, 40 * k, "search", 0},
		fsHit{X + bw - 58*k, Y + bh/2 - 22*k, 44 * k, 44 * k, "svc", 0},
		fsHit{X + cx - lineW/2 - 8*k, Y + bh - 24*k, lineW + 16*k, 20 * k, "seek", 0})
	sk.seekX0, sk.seekW = X+cx-lineW/2, lineW
	sk.searchBox = [4]float32{X + sx, Y + (bh-40*k)/2, 230 * k, 40 * k}
	f.put(l)
}

/* ---------- mouse and keys ---------- */

func (f *FullView) hitAt(x, y float32) (fsHit, bool) {
	for i := len(f.skin.hits) - 1; i >= 0; i-- {
		if f.skin.hits[i].has(x, y) {
			return f.skin.hits[i], true
		}
	}
	if cfg.SkinList && f.skin.listHit != nil { // the tilted list: rows through the inverse tilt
		switch i := f.skin.listHit(x, y); {
		case i >= 0:
			return fsHit{id: "song", arg: i}, true
		case i == -1:
			return fsHit{id: "panel"}, true // on the panel, between rows
		}
	}
	return fsHit{}, false
}

func (f *FullView) skinHover(x, y float32) {
	h, ok := f.hitAt(x, y)
	if !ok {
		h = fsHit{}
	}
	f.skin.hover, f.skin.hoverArg = h.id, h.arg
}

// skinClick: true when the click hit something (so a double-click there doesn't close)
func (f *FullView) skinClick(x, y float32) bool {
	h, ok := f.hitAt(x, y)
	sk := &f.skin
	if !ok && sk.searchOn {
		f.closeSearch()
	}
	if !ok {
		return false
	}
	if ok && h.id != "search" && h.id != "sugg" && h.id != "suggq" && h.id != "drop" && sk.searchOn {
		f.closeSearch()
	}
	sk.pressed, sk.pressedAt = h.id, time.Now()
	// everything is controlled right here; only an explicit search opens the music app
	snap := media.Snapshot()
	switch h.id {
	case "prev":
		f.stepPrev(snap)
	case "next":
		f.stepNext(snap)
	case "play", "song-info":
		mediaPlayPause()
	case "song": // a row of the list: the next songs skip ahead, the earlier ones go back
		rows, ahead := f.listRows(snap)
		_, cur := f.pastSongs(snap)
		if h.arg < len(rows) {
			switch {
			case rows[h.arg].Key == cur:
				mediaPlayPause()
			case ahead:
				f.goNext(h.arg, snap) // row 0 is the playing song, so row i is i songs ahead
			default:
				f.goBack(f.backCount(rows[h.arg].Key, snap), snap)
			}
		}
	case "card":
		if h.arg == 0 {
			mediaPlayPause()
			break
		}
		for _, sl := range f.flowSlots(snap) {
			if sl.d == h.arg {
				if sl.back > 0 {
					f.goBack(sl.back, snap)
				} else if sl.ahead > 0 {
					f.goNext(sl.ahead, snap)
				}
			}
		}
	case "shuffle", "repeat":
		f.toggleMode(h.id, snap)
	case "sugg", "suggq":
		if h.arg < len(sk.shown) {
			f.pickHit(sk.shown[h.arg], h.id == "suggq")
		}
	case "seek": // click or drag along the time line
		if snap.Track != nil && snap.Track.Duration > 0 {
			sk.dragging, sk.dragFrac = true, f.seekFrac(x)
			sk.bar.sig = ""
			fsCapture(f.hwnd, true)
		}
	case "search":
		sk.searchOn = true
	case "svc":
		f.serviceMenu()
	}
	return true
}

func (f *FullView) serviceMenu() {
	id := popupMenu(serviceNames, cfg.SkinService, f.hwnd) + 1
	if id > 0 && id <= len(serviceNames) {
		f.set(func() { cfg.SkinService = id - 1 }, "Search with · "+serviceNames[id-1])
	}
}

// openSearch opens the music app / site on a search for q.
func (f *FullView) openSearch(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	svc := serviceFor(cfg.SkinService, media.Snapshot().Source)
	ok := openURL(searchURL(svc, q))
	if !ok && svc == svcSpotify { // no Spotify app here: the web player
		ok = openURL(searchURL(svcSpotifyWeb, q))
	}
	if !ok {
		f.say("Couldn't open " + serviceNames[svc])
		return
	}
	f.say("Searching " + serviceNames[svc] + " for “" + q + "”")
}

// skinChar: a typed character while the search box is active.
func (f *FullView) skinChar(r rune) {
	sk := &f.skin
	switch r {
	case 13: // Enter: the highlighted suggestion (or the first one), else the music app
		i := sk.sel
		if i < 0 && len(sk.shown) > 0 {
			i = 0
		}
		if i >= 0 && i < len(sk.shown) {
			f.pickHit(sk.shown[i], false)
		} else {
			q := sk.search
			f.closeSearch()
			f.openSearch(q)
		}
	case 27:
		f.closeSearch()
	default:
		sk.search = editText(sk.search, r, 120)
		sk.sel = -1
		finder.ask(sk.search)
	}
}

func (f *FullView) skinKey(vk int) bool {
	switch vk {
	case 0x1B, 0x7A:
		closeFullscreen()
	case 0x20:
		mediaPlayPause()
		f.say("Play / pause")
	case 0x25:
		f.stepPrev(media.Snapshot())
	case 0x27:
		f.stepNext(media.Snapshot())
	case 0xBF, 'K': // "/" or K: search
		f.skin.searchOn = true
	case 'B':
		n := cycle(cfg.SkinBg, len(skinBgNames), 1)
		f.set(func() { cfg.SkinBg = n }, "Background · "+skinBgNames[n])
	case 'C':
		f.set(func() { cfg.SkinClock = !cfg.SkinClock }, "Clock · "+onOff(!cfg.SkinClock))
	case 'T':
		n := cycle(cfg.SkinClockColor, len(skinClockColorNames), 1)
		f.set(func() { cfg.SkinClockColor = n }, "Clock colour · "+skinClockColorNames[n])
	case 'M':
		n := cycle(cfg.SkinCentre, len(skinCentreNames), 1)
		f.set(func() { cfg.SkinCentre = n }, "Middle · "+skinCentreNames[n])
	case 'L':
		f.set(func() { cfg.SkinLyrics = !cfg.SkinLyrics }, "Lyrics · "+onOff(!cfg.SkinLyrics))
	case 'F':
		f.set(func() { cfg.SkinFrame = !cfg.SkinFrame }, "Edge light · "+onOff(!cfg.SkinFrame))
	case 'G':
		n := cycle(cfg.SkinFrameStyle, len(glowStyleNames), 1)
		f.set(func() { cfg.SkinFrameStyle = n; cfg.SkinFrame = true }, "Edge light · "+glowStyleNames[n])
	case 'P':
		f.set(func() { cfg.SkinList = !cfg.SkinList }, "Up next list · "+onOff(!cfg.SkinList))
	case 'D':
		f.set(func() { cfg.FsKind = 0 }, "Music display")
	default:
		return false
	}
	return true
}

/* ---------- moving between songs right here ---------- */

// Windows lets an app press the player's Previous / Next keys, nothing more — so going to a song
// from the list is a few quick presses. Orbit keeps the song list in step while it does that,
// and remembers the songs it stepped back over: those are what the player plays next.

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
	f.skin.navUntil = time.Now().Add(time.Duration(n)*260*time.Millisecond + 2500*time.Millisecond)
	historyHold = f.skin.navUntil
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
	f.skin.upNext = append(ahead, f.skin.upNext...)
	if len(f.skin.upNext) > 12 {
		f.skin.upNext = f.skin.upNext[:12]
	}
	rest := append([]HistItem(nil), past[n-1:]...)
	withCfg(func() { cfg.History = rest })
	saveConfigSoon()
	presses := n
	if snap.Pos > 3 { // Previous first rewinds the song itself when it has played a while
		presses++
	}
	f.pressKeys(vkMediaPrev, presses)
	f.skin.list.sig, f.skin.centre.sig = "", ""
	f.say("Back to “" + past[n-1].Title + "”")
}

// goNext j songs of the known "up next" ones.
func (f *FullView) goNext(j int, snap MediaSnap) {
	next := f.nextSongs(snap)
	if j < 1 || j > len(next) {
		return
	}
	f.skin.upNext = next
	spotify.stale() // the queue moves on: ask again once the song has changed
	hist, cur := f.pastSongs(snap)
	var front []HistItem
	for i := j - 1; i >= 0; i-- {
		front = append(front, f.skin.upNext[i])
	}
	if snap.Track != nil && (len(hist) == 0 || hist[0].Key != cur) {
		front = append(front, HistItem{Key: snap.Track.Key, Title: snap.Track.Title, Artist: snap.Track.Artist})
	}
	target := f.skin.upNext[j-1]
	f.skin.upNext = f.skin.upNext[j:]
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
	f.skin.list.sig, f.skin.centre.sig = "", ""
	f.say("On to “" + target.Title + "”")
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
		f.skin.spAsked = time.Now()
	}
	if snap.Track == nil || time.Now().Before(f.skin.navUntil) || len(f.skin.upNext) == 0 {
		return
	}
	for i, u := range f.skin.upNext {
		if u.Key == snap.Track.Key { // it played on into the songs we knew
			f.skin.upNext = f.skin.upNext[i+1:]
			return
		}
	}
	f.skin.upNext = nil // something else is playing: the old "next" is no longer known
}

// spotifyOn: the user connected their Spotify account in the settings.
func (f *FullView) spotifyOn() bool { return cfg.SpotifyClientID != "" && cfg.SpotifyRefresh != "" }

// nextSongs: what plays after the playing song — Spotify's real queue when connected, else the
// songs Orbit knows from going back. Empty when unknown.
func (f *FullView) nextSongs(snap MediaSnap) []HistItem {
	if f.spotifyOn() && snap.Track != nil && time.Now().After(f.skin.navUntil) {
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
	return f.skin.upNext
}

// listRows: the left list — the playing song, then what comes next (ahead=true); when nothing is
// known about what comes next, the songs played before it.
func (f *FullView) listRows(snap MediaSnap) (rows []HistItem, ahead bool) {
	hist, cur := f.pastSongs(snap)
	if next := f.nextSongs(snap); len(next) > 0 && snap.Track != nil {
		rows = append(rows, HistItem{Key: cur, Title: snap.Track.Title, Artist: snap.Track.Artist, CoverFile: coverFileFor(hist, cur)})
		for _, n := range next {
			if len(rows) >= 8 {
				break
			}
			rows = append(rows, n)
		}
		return rows, true
	}
	if len(hist) > 8 {
		hist = hist[:8]
	}
	return hist, false
}

func coverFileFor(hist []HistItem, key string) string {
	for _, h := range hist {
		if h.Key == key {
			return h.CoverFile
		}
	}
	return ""
}

// spotifyTick: keep the queue fresh while full screen shows it (every 15 s, sooner when it is for
// another song; the song changing asks at once).
func (f *FullView) spotifyTick(snap MediaSnap) {
	if !f.spotifyOn() {
		return
	}
	wait := 15 * time.Second
	if _, playing := spotify.snapshot(); snap.Track != nil && normStr(playing) != normStr(snap.Track.Title) && time.Now().After(f.skin.navUntil) {
		wait = 4 * time.Second // the queue is for another song (it changed while skipping): ask soon
	}
	if time.Since(f.skin.spAsked) < wait {
		return
	}
	f.skin.spAsked = time.Now()
	spotify.refreshQueue()
}

func (f *FullView) seekFrac(x float32) float64 {
	if f.skin.seekW <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, float64((x-f.skin.seekX0)/f.skin.seekW)))
}

// seekMove / seekUp: dragging the time line; the song jumps when the button is let go.
func (f *FullView) seekMove(x float32) {
	if f.skin.dragging {
		f.skin.dragFrac = f.seekFrac(x)
	}
}

func (f *FullView) seekUp(x float32) {
	sk := &f.skin
	if !sk.dragging {
		return
	}
	sk.dragging = false
	fsCapture(f.hwnd, false)
	snap := media.Snapshot()
	if snap.Track == nil || snap.Track.Duration <= 0 {
		return
	}
	at := f.seekFrac(x) * snap.Track.Duration
	if requestSeek(at - media.Offset()) {
		f.say(fmtTime(at) + " / " + fmtTime(snap.Track.Duration))
	} else {
		f.say("Jumping needs Windows media controls (Settings → Music)")
	}
	sk.bar.sig = ""
}

// skinWheel: the wheel over the bar is the volume (Windows' own, with its sign); elsewhere false.
func (f *FullView) skinWheel(x, y float32, up bool) bool {
	h, ok := f.hitAt(x, y)
	if !ok || !strings.Contains(" bar seek prev next play song-info search svc shuffle repeat ", " "+h.id+" ") {
		return false
	}
	vk, d := uintptr(0xAE), -1 // VK_VOLUME_DOWN
	if up {
		vk, d = 0xAF, 1
	}
	mediaKey(vk)
	f.skin.volUntil, f.skin.volDir = time.Now().Add(900*time.Millisecond), d
	return true
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
	f.skin.bar.sig = ""
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
		return what + ": connect Spotify again in Settings (Orbit needs one more permission)"
	}
	return what + ": your player doesn't allow it from outside — connect Spotify in Settings"
}

func (f *FullView) closeSearch() {
	sk := &f.skin
	sk.searchOn, sk.sel, sk.shown = false, -1, nil
	finder.reset()
}

// searchKey: ↑ ↓ move through the suggestions while typing. true: handled.
func (f *FullView) searchKey(vk int) bool {
	sk := &f.skin
	switch vk {
	case 0x26: // ↑
		if n := len(sk.shown); n > 0 {
			if sk.sel <= 0 {
				sk.sel = n - 1
			} else {
				sk.sel--
			}
		}
		return true
	case 0x28: // ↓
		if len(sk.shown) > 0 {
			sk.sel = (sk.sel + 1) % len(sk.shown)
		}
		return true
	}
	return false
}

// pickHit: play the song right here (Spotify connected), or add it to the queue; without
// Spotify, look it up in the music app.
func (f *FullView) pickHit(h searchHit, queue bool) {
	can := f.spotifyCan()
	f.closeSearch()
	f.skin.search = ""
	switch {
	case h.URI != "" && can && queue:
		spotify.do("queue", h.URI)
		f.say("Adding “" + h.Title + "” to the queue")
	case h.URI != "" && can:
		spotify.do("play", h.URI)
		f.say("Playing “" + h.Title + "”")
	default:
		f.openSearch(strings.TrimSpace(h.Title + " " + firstArtist(h.Artist)))
	}
}

// skinDrop: the suggestions above the search field — the songs you played first, then what
// Spotify (or Apple Music) finds. Click one, or ↑ ↓ and Enter.
func (f *FullView) skinDrop(snap MediaSnap, k float32) {
	sk := &f.skin
	l := &sk.drop
	q := strings.TrimSpace(sk.search)
	if !sk.searchOn || len([]rune(q)) < 1 {
		sk.shown = nil
		return
	}
	known := append([]HistItem(nil), cfg.History...)
	known = append(known, f.nextSongs(snap)...)
	local := localMatches(q, known, 4)
	oq, online, ver := finder.results()
	waiting := false
	if normStr(oq) != normStr(q) {
		online = nil
		waiting = len([]rune(q)) >= 2
	}
	hits := mergeHits(local, online, 6)
	sk.shown = hits
	if sk.sel >= len(hits) {
		sk.sel = len(hits) - 1
	}
	can := f.spotifyCan()
	rowH := 58 * k
	pw := float32(math.Max(float64(440*k), float64(sk.searchBox[2])))
	head, foot := 40*k, 34*k
	n := len(hits)
	ph := head + rowH*float32(max(n, 1)) + foot + 8*k
	m := 20 * k
	px := sk.searchBox[0] + sk.searchBox[2] - pw // right edges line up
	if px < 20*k {
		px = 20 * k
	}
	py := sk.searchBox[1] - 22*k - ph
	if sk.searchBox[1] > float32(f.H)*0.5 && py < 20*k {
		py = 20 * k
	}
	hov := -1
	if sk.hover == "sugg" || sk.hover == "suggq" {
		hov = sk.hoverArg
	}
	keys := make([]string, n)
	for i, h := range hits {
		keys[i] = h.Key + h.File
	}
	sig := fmt.Sprint(q, strings.Join(keys, "|"), ver, waiting, sk.sel, hov, sk.hover, can, f.accent(), cfg.SkinGlass, f.bgSig, f.H, f.videoOn, px, py)
	if sig != l.sig {
		l.sig = sig
		c := l.ensure(int(pw+2*m)+2, int(ph+2*m)+2)
		if c == nil {
			return
		}
		g := c.g
		for i, a := range []float64{0.2, 0.14, 0.08} { // shadow
			d := float32(i+1) * 6 * k
			sp := roundRectPath(m-d/2, m+d*0.5, pw+d, ph+d*0.4, 18*k+d)
			fillPathColor(g, sp, withAlpha(0xFF000000, a))
			freePath(sp)
		}
		gTranslate(g, m, m)
		body := roundRectPath(0, 0, pw, ph, 18*k)
		f.frost(g, px, py, pw, ph, body)
		fillPathGradient(g, body, withAlpha(0xFF24222C, 0.78+0.2*cfg.SkinGlass), withAlpha(0xFF0E0E14, 0.85+0.15*cfg.SkinGlass), 0, ph)
		strokePath(g, body, 0x50FFFFFF, 1.2)
		freePath(body)
		hf := font("Segoe UI Semibold", float64(12*k), false)
		src := "Apple Music"
		if f.spotifyOn() {
			src = "Spotify"
		}
		label := "SONGS · " + strings.ToUpper(src)
		if waiting {
			label += " · SEARCHING…"
		}
		drawText(g, hf, label, 18*k, 14*k, pw-36*k, 0x80FFFFFF)
		tf, af := font("Segoe UI Semibold", float64(15.5*k), false), font("Segoe UI", float64(12.5*k), false)
		if n == 0 {
			msg := "No songs found"
			if waiting {
				msg = "Looking…"
			}
			drawText(g, af, msg, 18*k, head+18*k, pw-36*k, 0x99FFFFFF)
		}
		icon := iconFont()
		for i, h := range hits {
			ry := head + rowH*float32(i)
			if i == sk.sel || i == hov {
				p := roundRectPath(8*k, ry, pw-16*k, rowH-6*k, 12*k)
				a := 0.09
				if i == sk.sel {
					a = 0.17
				}
				fillPathColor(g, p, withAlpha(0xFFFFFFFF, a))
				if i == sk.sel {
					strokePath(g, p, withAlpha(f.accent(), 0.7), 1.2)
				}
				freePath(p)
			}
			ts := 42 * k
			tx, ty := 16*k, ry+(rowH-6*k-ts)/2
			p := roundRectPath(tx, ty, ts, ts, 7*k)
			img := uintptr(0)
			if h.File != "" {
				img = f.thumb(HistItem{Key: "sf:" + h.File, CoverFile: h.File})
			} else if h.Key == cover.key {
				img = cover.img
			}
			if img != 0 {
				clipPath(g, p)
				setInterp(g, interpHighQualityBicubic)
				drawCoverSquare(g, img, tx, ty, ts)
				clipReset(g)
			} else {
				fillPathGradient(g, p, withAlpha(f.accent(), 0.5), 0xFF1A1A22, ty, ty+ts)
				if icon != "" {
					fn := font(icon, float64(14*k), false)
					w2, h2 := measure(fn, "\uE8D6")
					drawText(g, fn, "\uE8D6", tx+(ts-w2)/2, ty+(ts-h2)/2, w2+4, 0xA0FFFFFF)
				}
			}
			freePath(p)
			textW := pw - ts - 110*k
			drawText(g, tf, h.Title, tx+ts+12*k, ty+1*k, textW, 0xF5FFFFFF)
			sub := h.Artist
			if h.From == "played" {
				sub = "Played · " + sub
			}
			drawText(g, af, sub, tx+ts+12*k, ty+22*k, textW, 0x99FFFFFF)
			// right side: what Enter / a click does
			rx := pw - 26*k
			cyr := ry + (rowH-6*k)/2
			if h.URI != "" && can {
				if i == hov && sk.hover == "suggq" {
					fillCircle(g, 0x30FFFFFF, rx-34*k, cyr, 14*k)
				}
				if icon != "" {
					fn := font(icon, float64(12*k), false)
					w2, h2 := measure(fn, "\uE710") // +: add to the queue
					drawText(g, fn, "\uE710", rx-34*k-w2/2, cyr-h2/2, w2+4, 0xC0FFFFFF)
					fn = font(icon, float64(13*k), false)
					w2, h2 = measure(fn, "\uE768") // ▶ play
					drawText(g, fn, "\uE768", rx-w2/2, cyr-h2/2, w2+4, withAlpha(f.accent(), 1)|0xFF000000)
				}
			} else if icon != "" {
				fn := font(icon, float64(11*k), false)
				w2, h2 := measure(fn, "\uE8A7") // opens in the app
				drawText(g, fn, "\uE8A7", rx-w2/2, cyr-h2/2, w2+4, 0x80FFFFFF)
			}
		}
		hint := "↑ ↓ choose · Enter play · + add to the queue · Esc close"
		if !can {
			hint = "Enter opens it in " + serviceNames[serviceFor(cfg.SkinService, snap.Source)] + " · connect Spotify in Settings to play here"
			if f.spotifyOn() {
				hint = "Connect Spotify again in Settings to play songs right here"
			}
		}
		drawText(g, font("Segoe UI", float64(11.5*k), false), hint, 18*k, ph-foot+6*k, pw-36*k, 0x70FFFFFF)
		gReset(g)
		l.x, l.y = int(px-m), int(py-m)
	}
	f.put(l)
	// hits: on top of everything else (added last)
	X, Y := float32(l.x)+m, float32(l.y)+m
	sk.hits = append(sk.hits, fsHit{X, Y, pw, ph, "drop", 0})
	for i, h := range hits {
		ry := Y + head + rowH*float32(i)
		sk.hits = append(sk.hits, fsHit{X + 8*k, ry, pw - 16*k, rowH - 6*k, "sugg", i})
		if h.URI != "" && can {
			sk.hits = append(sk.hits, fsHit{X + pw - 26*k - 50*k, ry + 6*k, 32 * k, rowH - 18*k, "suggq", i})
		}
	}
}
