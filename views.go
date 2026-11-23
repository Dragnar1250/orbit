//go:build windows || darwin

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/* ===================== Shared colours / fonts / cover ===================== */

var albumAccent uint32 = 0xFFF2F2F2

func isCustomTheme() bool { return cfg.Theme == len(themes)-1 }

func accent() uint32 {
	if isCustomTheme() {
		return parseHex(cfg.AccentColor)
	}
	t := themes[cfg.Theme%len(themes)]
	if t.Accent == 0 {
		return albumAccent
	}
	return t.Accent
}

func textColor() uint32 {
	if isCustomTheme() {
		return parseHex(cfg.TextColor)
	}
	return themes[cfg.Theme%len(themes)].Text
}

var fontCache = map[string]*Font{}

// trimCaches runs at the start of a frame, when nothing is being drawn.
func trimCaches() {
	if len(fontCache) > 96 {
		for k, f := range fontCache {
			f.Free()
			delete(fontCache, k)
		}
	}
	trimBrushes()
}

func font(name string, size float64, bold bool) *Font {
	size = math.Round(size*2) / 2
	if size < 6 {
		size = 6
	}
	k := fmt.Sprintf("%s|%.1f|%v", name, size, bold)
	if f, ok := fontCache[k]; ok {
		return f
	}
	// never free fonts here: runs built earlier in this frame still use them (that freed a font
	// in the middle of drawing and crashed 1.1–1.4) — trimCaches() cleans up between frames
	f := newFont(name, float32(size), bold)
	fontCache[k] = f
	return f
}

// Cover holds the decoded album art of the current song (shared by vinyl and halo).
type Cover struct {
	key string
	ver int
	img uintptr
}

var cover Cover

func updateCover(t *Track) bool {
	if t == nil {
		if cover.img != 0 || cover.key != "" {
			freeImage(cover.img)
			cover = Cover{}
			return true
		}
		return false
	}
	if cover.key == t.Key && cover.ver == t.CoverVer {
		return false
	}
	freeImage(cover.img)
	cover = Cover{key: t.Key, ver: t.CoverVer, img: imageFromBytes(t.Cover)}
	if cover.img != 0 {
		albumAccent = sampleAccent(cover.img)
		saveHistoryCover(t.Key, cover.img)
	}
	return true
}

func sampleAccent(img uintptr) uint32 {
	b := newBitmap(24, 24)
	if b == nil {
		return 0xFFF2F2F2
	}
	defer b.Free()
	gImage(b.g, img, 0, 0, 24, 24)
	px := make([]uint32, 0, 576)
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			px = append(px, pixel(b.bmp, x, y))
		}
	}
	return accentFromPixels(px)
}

// drawCoverSquare draws img cropped to a centred square into (x, y, s, s).
func drawCoverSquare(g, img uintptr, x, y, s float32) {
	if img == 0 {
		return
	}
	w, h := imageSize(img)
	if w <= 0 || h <= 0 {
		return
	}
	scale := s / float32(math.Min(float64(w), float64(h)))
	dw, dh := float32(w)*scale, float32(h)*scale
	gImage(g, img, x+(s-dw)/2, y+(s-dh)/2, dw, dh)
}

/* ---------- Recently played (with small cover files) ---------- */

func historyCoverPath(key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(appDir(), "cache", "cover-"+hex.EncodeToString(h[:8])+".png")
}

func saveHistoryCover(key string, img uintptr) {
	p := historyCoverPath(key)
	if _, err := os.Stat(p); err == nil {
		return
	}
	b := newBitmap(96, 96)
	if b == nil {
		return
	}
	drawCoverSquare(b.g, img, 0, 0, 96)
	if savePNG(b.bmp, p) {
		withCfg(func() {
			for i := range cfg.History {
				if cfg.History[i].Key == key {
					cfg.History[i].CoverFile = p
				}
			}
		})
		saveConfigSoon()
	}
	b.Free()
}

// historyHold: full screen is stepping through songs with the player's keys; it keeps the
// song list itself until then.
var historyHold time.Time

func pushHistory(t *Track) {
	if time.Now().Before(historyHold) {
		return
	}
	if looksLikeID(t.Title) { // a player sent a file hash instead of a name: not a song to list
		return
	}
	h := HistItem{Key: t.Key, Title: t.Title, Artist: t.Artist}
	if _, err := os.Stat(historyCoverPath(t.Key)); err == nil {
		h.CoverFile = historyCoverPath(t.Key)
	}
	withCfg(func() {
		out := []HistItem{h}
		for _, x := range cfg.History {
			if x.Key != t.Key {
				out = append(out, x)
			}
		}
		if len(out) > 8 {
			out = out[:8]
		}
		cfg.History = out
	})
	saveConfigSoon()
}

var histImgs = map[string]uintptr{}

func historyImage(it HistItem) uintptr {
	if it.CoverFile == "" {
		return 0
	}
	if img, ok := histImgs[it.CoverFile]; ok {
		return img
	}
	img := uintptr(0)
	if _, err := os.Stat(it.CoverFile); err == nil {
		img = imageFromFile(it.CoverFile)
	}
	histImgs[it.CoverFile] = img
	return img
}

// editFrame draws the arrange box: faint fill (so it can be grabbed), dashed outline,
// corner handles and the name + size.
func editFrame(g uintptr, w, h float32, name, label string) {
	if !editMode && hoverName == name && !time.Now().Before(flashUntil[name]) {
		drawHover(g, w, h, name)
		return
	}
	if widgetLocked(name) {
		label += "  ·  locked"
	}
	bg := roundRectPath(3, 3, w-6, h-6, 10)
	fillPathColor(g, bg, 0x1EFFFFFF)
	freePath(bg)
	p := newPen(0xDDFFFFFF, 2)
	penDash(p)
	path := roundRectPath(3, 3, w-6, h-6, 10)
	drawPathPen(g, p, path)
	freePath(path)
	freePen(p)
	hs := float32(handlePx()) * 0.62
	for _, c := range [][2]float32{{3, 3}, {w - 3 - hs, 3}, {3, h - 3 - hs}, {w - 3 - hs, h - 3 - hs}} {
		fillRect(g, 0xFFFFFFFF, c[0], c[1], hs, hs)
		fillRect(g, withAlpha(accent(), 1), c[0]+2, c[1]+2, hs-4, hs-4)
	}
	text := fmt.Sprintf("%s · %d%%", label, int(math.Round(cfg.widget(name).Scale*100)))
	f := font("Segoe UI", 13, true)
	tw, _ := measure(f, text)
	fillRect(g, 0xC8000000, hs+10, 8, tw+16, 24)
	drawText(g, f, text, hs+18, 11, tw+4, 0xFFFFFFFF)
}

/* ===================== Turntable ===================== */

type VinylView struct {
	w        *Widget
	base     *Bitmap // glow, rings, (grooves), track list
	label    *Bitmap // rotating part (cover)
	sig      string
	needFull bool // next frame must push the whole window (layers rebuilt / resized)
	angle    float64
	last     time.Time
	version  int
	R        float64
	sx, sy   float64 // +1/-1: mirror for the right / top corners
	cx, cy   float64 // disc centre in window pixels
	progress float64
	drawnAt  time.Time
	armT     float64 // 0 = needle up on its rest, 1 = on the record
	ambT     float64 // ambient light clock
	playing  bool
	bands    [nBands]float32
	bass     float32
	layerVer int // bumped when base / label are rebuilt (the Mac keeps them on the GPU)
}

func newVinylView() *VinylView {
	v := &VinylView{w: newWidget("vinyl", "Turntable"), last: time.Now(), version: -1, sx: 1, sy: 1, armT: 1}
	v.w.useSoft(0.5, false) // ambient light and the bass rings on the Mac's soft layer
	return v
}

// Local layout (bottom-left corner): disc centre at (1.08R, 1.12R), list to the right.
func (v *VinylView) geometry(ui float64) (R float64, W, H int) {
	sc := cfg.widget("vinyl").Scale
	R = 190 * ui * sc
	W, H, _ = widgetSize("vinyl", sc) // one formula for drawing and for dragging
	return
}

// tx/ty map local (bottom-left layout) coordinates to window coordinates.
func (v *VinylView) tx(lx float64) float32 {
	if v.sx > 0 {
		return float32(lx)
	}
	return float32(float64(v.w.w) - lx)
}
func (v *VinylView) ty(ly float64) float32 {
	if v.sy > 0 {
		return float32(ly)
	}
	return float32(float64(v.w.h) - ly)
}

func (v *VinylView) Update(now time.Time, snap MediaSnap, bands [nBands]float32, bass float32, ui float64) {
	dt := now.Sub(v.last).Seconds()
	v.last = now
	if !cfg.ShowVinyl {
		if v.w.canvas != nil {
			v.w.release()
			v.base.Free()
			v.label.Free()
			v.base, v.label, v.sig = nil, nil, ""
		}
		return
	}
	if snap.Version != v.version {
		v.version = snap.Version
		if snap.Track != nil {
			pushHistory(snap.Track)
		}
		needRedraw = true
	}
	coverChanged := updateCover(snap.Track)
	R, W, H := v.geometry(ui)
	corner := cfg.VinylCorner
	v.sx, v.sy = 1, 1
	if corner == 1 || corner == 3 {
		v.sx = -1
	}
	if corner == 2 || corner == 3 {
		v.sy = -1
	}

	p := cfg.widget("vinyl")
	wa := workArea()
	x, y := p.X, p.Y
	if !p.Set { // default: tucked into the chosen corner, partly off-screen like a real deck
		x = int(wa.Left) - int(R*0.32)
		if v.sx < 0 {
			x = int(wa.Right) - W + int(R*0.32)
		}
		y = int(wa.Bottom) - H + int(R*0.30)
		if v.sy < 0 {
			y = int(wa.Top) - int(R*0.30)
		}
	}
	if cfg.VinylLayout == 1 && (drag == nil || drag.w != v.w) {
		// corner: about half the record tucked into the corner of the screen, always there
		ins := R * 0.45
		if v.sx > 0 {
			x = int(float64(wa.Left) + ins - R*1.08)
		} else {
			x = int(float64(wa.Right) - ins - (float64(W) - R*1.08))
		}
		if v.sy > 0 {
			y = int(float64(wa.Bottom) - ins - R*1.12)
		} else {
			y = int(float64(wa.Top) + ins - (float64(H) - R*1.12))
		}
	}
	sizeChanged := v.w.w != W || v.w.h != H
	v.w.resize(W, H)
	if sizeChanged || layoutDirty || !v.w.visible {
		v.w.moveTo(x, y)
		v.needFull = true
	}
	v.R = R
	v.cx, v.cy = float64(v.tx(R*1.08)), float64(v.ty(R*1.12))

	key, title := "", ""
	if snap.Track != nil {
		key, title = snap.Track.Key, snap.Track.Title
	}
	sig := fmt.Sprintf("%s|%d|%d|%.1f|%v|%v|%d|%x|%s|%d|%d|%d|%v|%s|%s|%v", key, cover.ver, cfg.Theme, R, cfg.ShowList, editMode,
		len(cfg.History), albumAccent, title, cfg.DiscStyle, corner, cfg.ListCount, cfg.ShowArm, cfg.TextColor, cfg.AccentColor, framed("vinyl"))
	sig += fmt.Sprintf("|%d|%s|%.2f|%v|%d|%s|%d", cfg.DiscColorMode, cfg.DiscColor, cfg.DiscGlow, widgetLocked("vinyl"), cfg.VinylLayout, snapArtist(snap), hoverBtn)
	if sig != v.sig || coverChanged || styleDirty {
		v.sig = sig
		v.buildLayers()
		needRedraw = true
	}
	if snap.Track != nil && snap.Track.Duration > 0 {
		pr := math.Max(0, math.Min(1, snap.Pos/snap.Track.Duration))
		if math.Abs(pr-v.progress) > 0.002 {
			v.progress = pr
			needRedraw = true
		}
	}
	spinning := snap.Playing && snap.Track != nil && cfg.FPS > 0 && cfg.SpinRPM > 0
	if spinning {
		v.angle = math.Mod(v.angle+dt*cfg.SpinRPM*6*v.sx*v.sy, 360)
	}
	// needle: lowers onto the record when the music starts, lifts off when it stops
	target := 1.0
	if cfg.ArmLift && !(snap.Playing && snap.Track != nil) {
		target = 0
	}
	if cfg.FPS == 0 {
		v.armT = target
	} else if v.armT != target {
		step := math.Min(1, dt) * 1.6
		if math.Abs(target-v.armT) <= step {
			v.armT = target
		} else if target > v.armT {
			v.armT += step
		} else {
			v.armT -= step
		}
		needRedraw = true
	}
	v.bands, v.bass = bands, bass
	v.ambT += dt
	v.playing = snap.Playing
	if cfg.AmbVinyl && cfg.AmbBright > 0 && cfg.FPS > 0 && snap.Playing && (cfg.AmbEffect != 0 || cfg.AmbColorMode == 3) {
		needRedraw = true // the light moves while the music plays
	}
	if !spinning && !needRedraw && v.w.visible {
		return // nothing moves → nothing to draw (no CPU while paused)
	}
	v.drawnAt = now
	v.draw()
}

// drawLayered (the Mac): the still picture and the label stay on the GPU, which turns the label;
// a frame only redraws what moves with the music — the light and the bass rings (on the soft
// layer, a quarter of the pixels), the groove light, the shine and the arm.
func (v *VinylView) drawLayered() {
	g := v.w.g()
	R, cx, cy := float32(v.R), float32(v.cx), float32(v.cy)
	gClear(g, 0) // (and the soft layer)
	amb := cfg.AmbVinyl && cfg.AmbBright > 0
	rings := cfg.DiscGlow > 0 && v.bass > 0.02
	if (amb || rings) && softBegin(g) {
		if amb { // under the still picture: only what is around the record shows
			bass := float64(v.bass)
			if !v.playing {
				bass = 0
			}
			ambientBlobs(g, cx, cy, R*float32(1.62*cfg.AmbSize), v.ambT, bass, float32(v.sx))
		}
		if rings {
			gc := accent()
			if cfg.DiscStyle != discPicture && cfg.DiscStyle != discBlack {
				gc = discColor()
			}
			a := math.Min(1, float64(v.bass)*1.4) * cfg.DiscGlow / 2
			for i, wd := range []float32{0.16, 0.1, 0.05} {
				strokeCircle(g, withAlpha(gc, a*[]float64{0.18, 0.3, 0.55}[i]), cx, cy, R*(1.0+wd/2), R*wd)
			}
		}
		softEnd(g)
	}
	if cfg.GrooveLight {
		v.drawGrooveLight(g)
	}
	if cfg.Shine {
		fillPie(g, 0x14FFFFFF, cx-R, cy-R, 2*R, 2*R, -62, 34)
		fillPie(g, 0x10FFFFFF, cx-R, cy-R, 2*R, 2*R, 118, 30)
	}
	if cfg.ShowArm {
		v.drawArm(g)
	}
	v.w.present()
	v.w.setBase(v.base, v.layerVer)
	v.w.spin(v.label, v.layerVer, float64(cx), float64(cy), v.angle)
	v.needFull = false
}

// discColor is the colour of coloured / splatter / marble / clear records.
func discColor() uint32 {
	switch cfg.DiscColorMode {
	case 1:
		if albumAccent != 0 {
			return albumAccent | 0xFF000000
		}
	case 2:
		return parseHex(cfg.DiscColor)
	}
	return accent() | 0xFF000000
}

// rotatesWhole: records whose pattern is not round must turn as one image.
func rotatesWhole(style int) bool {
	return style == discPicture || style == discSplatter || style == discMarble
}

func (v *VinylView) buildLayers() {
	R, cx, cy := float32(v.R), float32(v.cx), float32(v.cy)
	acc := accent()
	style := cfg.DiscStyle
	picture := style == discPicture
	dc := discColor()
	key := ""
	if t := media.Snapshot().Track; t != nil {
		key = t.Key
	}

	// rotating part: the cover label (or the whole record for picture / splatter / marble)
	L := int(R * 1.28)
	if rotatesWhole(style) {
		L = int(R * 1.98)
	}
	v.label.Free()
	v.label = newBitmap(L, L)
	if v.label != nil {
		g := v.label.g
		setInterp(g, interpHighQualityBicubic)
		half := float32(L) / 2
		k := half / (R * 0.99) // local px per record px
		labelR := half         // cover radius inside this bitmap
		if !picture && rotatesWhole(style) {
			labelR = R * 0.64 * k
			paintPattern(g, style, dc, half, key)
			grooves(g, half, half, R*0.66*k, half*0.995, float32(math.Max(2.2, float64(R)*0.012))*k, 0x10FFFFFF, 0x1EFFFFFF)
			strokeCircle(g, 0x60000000, half, half, half-1.5, 3)
			strokeCircle(g, 0x40FFFFFF, half, half, half-0.5, 1)
		}
		fillCircle(g, 0xFF1C1C20, half, half, labelR)
		if cover.img != 0 {
			clip := clipCircle(g, half, half, labelR)
			drawCoverSquare(g, cover.img, half-labelR, half-labelR, 2*labelR)
			clipReset(g)
			freePath(clip)
		} else {
			fillCircle(g, withAlpha(acc, 0.35), half, half, labelR*0.98)
			f := font("Segoe UI Symbol", float64(labelR*0.5), false)
			tw, th := measure(f, "♪")
			drawText(g, f, "♪", half-tw/2, half-th/2, tw+4, 0xCCFFFFFF)
		}
		if picture {
			// grooves, white rings and the spindle hole turn with the cover (they are round,
			// so it looks the same) — one image to rotate per frame instead of two layers
			step := float32(math.Max(2.6, float64(R)*0.014)) * k
			i := 0
			for r := R * 0.36 * k; r < half*0.995; r += step {
				col := uint32(0x16000000)
				if i%4 == 0 {
					col = 0x14FFFFFF
				}
				strokeCircle(g, col, half, half, r, 0.9)
				i++
			}
			for _, rr := range []float32{0.34, 0.62, 0.9} {
				strokeCircle(g, 0x8CFFFFFF, half, half, R*rr*k, 1.3)
			}
			strokeCircle(g, 0x70000000, half, half, half-1.5, 3)
			strokeCircle(g, 0x40FFFFFF, half, half, half-0.5, 1)
			fillCircle(g, 0xFF0B0B0D, half, half, R*0.03*k+2)
		} else {
			strokeCircle(g, 0x66000000, half, half, labelR-1.5, 3)
			strokeCircle(g, 0x22FFFFFF, half, half, labelR*0.16, 1.2)
			fillCircle(g, 0xFF0B0B0D, half, half, labelR*0.045+2)
			fillCircle(g, 0x55FFFFFF, half-labelR*0.012, half-labelR*0.012, labelR*0.015+0.8)
		}
	}
	v.needFull = true
	v.layerVer++

	v.base.Free()
	v.base = newBitmap(v.w.w, v.w.h)
	if v.base == nil {
		return
	}
	g := v.base.g
	gClear(g, 0)
	glowC := acc
	if style != discPicture && style != discBlack {
		glowC = dc
	}
	softGlow(g, cx, cy+R*0.04*float32(v.sy), R*1.2, 0xAA000000)
	if cfg.DiscGlow > 0 {
		softGlow(g, cx, cy, R*1.16, withAlpha(glowC, 0.2*cfg.DiscGlow))
	}
	strokeCircle(g, withAlpha(acc, 0.55), cx, cy, R*1.035, 1.4)
	strokeCircle(g, withAlpha(acc, 0.22), cx, cy, R*1.075, 1)
	step := float32(math.Max(2.2, float64(R)*0.012))
	switch style {
	case discBlack:
		fillCircle(g, 0xFF0A0A0C, cx, cy, R)
		grooves(g, cx, cy, R*0.66, R*0.985, step, 0x12FFFFFF, 0x22FFFFFF)
		strokeCircle(g, 0x50FFFFFF, cx, cy, R-0.8, 1.2)
	case discColoured:
		fillCircle(g, mixColor(dc, 0xFF000000, 0.25)|0xFF000000, cx, cy, R)
		softGlow(g, cx-R*0.25, cy-R*0.3, R*0.9, withAlpha(mixColor(dc, 0xFFFFFFFF, 0.5), 0.35))
		grooves(g, cx, cy, R*0.66, R*0.985, step, 0x14000000, 0x24FFFFFF)
		strokeCircle(g, 0x60FFFFFF, cx, cy, R-0.8, 1.2)
	case discClear: // see-through: tinted glass with grooves
		fillCircle(g, withAlpha(dc, 0.28), cx, cy, R)
		grooves(g, cx, cy, R*0.66, R*0.985, step, 0x18FFFFFF, 0x30FFFFFF)
		strokeCircle(g, withAlpha(mixColor(dc, 0xFFFFFFFF, 0.6), 0.8), cx, cy, R-1, 2)
		strokeCircle(g, 0x30FFFFFF, cx, cy, R*0.66, 1)
	case discGold:
		fillCircle(g, 0xFF8A6418, cx, cy, R)
		softGlow(g, cx-R*0.3, cy-R*0.35, R*1.1, 0x90FFE7A0)
		grooves(g, cx, cy, R*0.66, R*0.985, step, 0x22000000, 0x40FFF3C8)
		strokeCircle(g, 0xC0FFE9A8, cx, cy, R-0.8, 1.6)
	default: // picture / splatter / marble: the rotating image is the whole record
		fillCircle(g, 0xFF0A0A0C, cx, cy, R)
	}
	if cfg.VinylLayout == 1 {
		v.drawCard(g)
	} else if cfg.ShowList {
		v.drawList(g)
	}
	if framed("vinyl") {
		editFrame(g, float32(v.w.w), float32(v.w.h), "vinyl", "Turntable")
	}
}

func grooves(g uintptr, cx, cy, from, to, step float32, c, every5 uint32) {
	i := 0
	for r := from; r < to; r += step {
		col := c
		if i%5 == 0 {
			col = every5
		}
		strokeCircle(g, col, cx, cy, r, 0.8)
		i++
	}
}

// paintPattern fills a whole-record bitmap with a splatter or marble pattern (same song = same pattern).
func paintPattern(g uintptr, style int, dc uint32, half float32, key string) {
	seed := uint32(2166136261)
	for _, b := range []byte(key) {
		seed = (seed ^ uint32(b)) * 16777619
	}
	rnd := func() float32 { seed = hash32(seed + 0x9E3779B9); return float32(seed%10000) / 10000 }
	clip := clipCircle(g, half, half, half)
	defer func() { clipReset(g); freePath(clip) }()
	switch style {
	case discSplatter:
		fillCircle(g, mixColor(dc, 0xFFFFFFFF, 0.15)|0xFF000000, half, half, half)
		cols := []uint32{0xE6FFFFFF, 0xE6101012, mixColor(dc, 0xFF000000, 0.55) | 0xE6000000, mixColor(dc, 0xFFFFFFFF, 0.6) | 0xE6000000}
		for i := 0; i < 70; i++ {
			ang, dist := rnd()*2*math.Pi, half*float32(math.Sqrt(float64(rnd())))
			x, y := half+dist*float32(math.Cos(float64(ang))), half+dist*float32(math.Sin(float64(ang)))
			r := half * (0.012 + rnd()*rnd()*0.09)
			c := cols[int(rnd()*float32(len(cols)))%len(cols)]
			fillEllipse(g, c, x-r, y-r*(0.7+rnd()*0.6), 2*r, 2*r*(0.7+rnd()*0.6))
			for j := 0; j < 3; j++ { // droplets around a splat
				a2, d2 := rnd()*2*math.Pi, r*(1.3+rnd()*1.8)
				rr := r * (0.12 + rnd()*0.25)
				fillCircle(g, c, x+d2*float32(math.Cos(float64(a2))), y+d2*float32(math.Sin(float64(a2))), rr)
			}
		}
	case discMarble:
		fillCircle(g, mixColor(dc, 0xFF000000, 0.35)|0xFF000000, half, half, half)
		cols := []uint32{withAlpha(mixColor(dc, 0xFFFFFFFF, 0.55), 0.45), withAlpha(0xFFFFFFFF, 0.35), withAlpha(mixColor(dc, 0xFF000000, 0.6), 0.5), withAlpha(dc, 0.6)}
		for i := 0; i < 26; i++ {
			c := cols[i%len(cols)]
			w := half * (0.02 + rnd()*0.1)
			cx, cy := half+(rnd()-0.5)*half*1.6, half+(rnd()-0.5)*half*1.6
			rx, ry := half*(0.3+rnd()*0.9), half*(0.2+rnd()*0.7)
			p := newPen(c, w)
			drawArc(g, p, cx-rx, cy-ry, 2*rx, 2*ry, rnd()*360, 80+rnd()*200)
			freePen(p)
		}
		for i := 0; i < 10; i++ { // thin white veins
			cx, cy := half+(rnd()-0.5)*half*1.4, half+(rnd()-0.5)*half*1.4
			rx, ry := half*(0.2+rnd()*0.8), half*(0.2+rnd()*0.8)
			p := newPen(0x70FFFFFF, 1+rnd()*1.5)
			drawArc(g, p, cx-rx, cy-ry, 2*rx, 2*ry, rnd()*360, 60+rnd()*160)
			freePen(p)
		}
	}
}

// drawList puts "now playing" + recently played on an arc around the open side of the record.
func (v *VinylView) drawList(g uintptr) {
	R := float32(v.R)
	snap := media.Snapshot()
	type item struct {
		title, artist string
		img           uintptr
		now           bool
	}
	var items []item
	if snap.Track != nil {
		items = append(items, item{snap.Track.Title, snap.Track.Artist, cover.img, true})
	}
	for _, h := range cfg.History {
		if len(items) >= 1+cfg.ListCount {
			break
		}
		if snap.Track != nil && h.Key == snap.Track.Key {
			continue
		}
		items = append(items, item{h.Title, h.Artist, historyImage(h), false})
	}
	if len(items) == 0 {
		items = append(items, item{"Nothing playing", "Play something on Spotify", 0, true})
	}
	acc := accent()
	txt := textColor()
	ra := v.R * 1.15
	for i, it := range items {
		deg := -30 + float64(i)*18
		a := deg * math.Pi / 180
		ax := float32(v.cx + v.sx*ra*math.Cos(a))
		ay := float32(v.cy + v.sy*ra*math.Sin(a))
		gTranslate(g, ax, ay)
		gRotate(g, float32(deg*0.55*v.sx*v.sy))
		cs := R * 0.17
		ts, as := R*0.085, R*0.064
		if it.now {
			cs, ts = R*0.2, R*0.1
		}
		alpha := 0.78
		if it.now {
			alpha = 1
		}
		x0 := float32(0) // cover position (right side: grows to the left)
		if v.sx < 0 {
			x0 = -cs
		}
		path := roundRectPath(x0, -cs/2, cs, cs, cs*0.14)
		clipPath(g, path)
		fillRect(g, 0xFF26262B, x0, -cs/2, cs, cs)
		drawCoverSquare(g, it.img, x0, -cs/2, cs)
		clipReset(g)
		freePath(path)
		gap := R * 0.06
		maxW := R * 1.3
		fT := font("Segoe UI", float64(ts), true)
		fA := font("Segoe UI", float64(as), false)
		y := -ts * 0.95
		text := func(f *Font, s string, yy float32, c uint32) {
			if v.sx > 0 {
				drawText(g, f, s, cs+gap, yy, maxW, c)
			} else {
				drawTextRight(g, f, s, -cs-gap, yy, maxW, c)
			}
		}
		if it.now {
			text(font("Segoe UI", float64(R*0.05), true), "NOW PLAYING", -cs/2-R*0.005, withAlpha(acc, 0.95))
			y = -ts * 0.62
		}
		text(fT, it.title, y, withAlpha(txt, alpha))
		text(fA, it.artist, y+ts*1.22, withAlpha(txt, alpha*0.62))
		gReset(g)
	}
}

func (v *VinylView) draw() {
	if v.w.canvas == nil || v.base == nil {
		return
	}
	if v.w.gpuLayers() {
		v.drawLayered()
		return
	}
	g := v.w.g()
	R, cx, cy := float32(v.R), float32(v.cx), float32(v.cy)
	// only the record + tonearm change between frames: redraw and push just that square
	D := R * 1.36
	if cfg.AmbVinyl && cfg.AmbBright > 0 {
		D = float32(math.Max(float64(D), float64(R)*(1.62*cfg.AmbSize+0.15)))
	}
	dirty := RECT{int32(math.Max(0, float64(cx-D))), int32(math.Max(0, float64(cy-D))), int32(math.Min(float64(v.w.w), float64(cx+D))), int32(math.Min(float64(v.w.h), float64(cy+D)))}
	full := v.needFull
	if !full {
		clipRect(g, float32(dirty.Left), float32(dirty.Top), float32(dirty.Right-dirty.Left), float32(dirty.Bottom-dirty.Top))
	}
	setCopyMode(g, true) // copy the cached layer over everything — no separate clear pass
	gImage(g, v.base.bmp, 0, 0, float32(v.w.w), float32(v.w.h))
	setCopyMode(g, false)
	if cfg.AmbVinyl && cfg.AmbBright > 0 {
		v.drawAmbient(g, full, dirty)
	}
	if cfg.DiscGlow > 0 && v.bass > 0.02 { // the glow breathes with the bass
		gc := accent()
		if cfg.DiscStyle != discPicture && cfg.DiscStyle != discBlack {
			gc = discColor()
		}
		a := math.Min(1, float64(v.bass)*1.4) * cfg.DiscGlow / 2
		for i, wd := range []float32{0.16, 0.1, 0.05} {
			strokeCircle(g, withAlpha(gc, a*[]float64{0.18, 0.3, 0.55}[i]), cx, cy, R*(1.0+wd/2), R*wd)
		}
	}
	if v.label != nil {
		L := float32(v.label.w)
		gTranslate(g, cx, cy)
		gRotate(g, float32(v.angle))
		gImage(g, v.label.bmp, -L/2, -L/2, L, L)
		gReset(g)
	}
	if cfg.GrooveLight {
		v.drawGrooveLight(g)
	}
	if cfg.Shine { // light reflections stay still while the record turns
		fillPie(g, 0x14FFFFFF, cx-R, cy-R, 2*R, 2*R, -62, 34)
		fillPie(g, 0x10FFFFFF, cx-R, cy-R, 2*R, 2*R, 118, 30)
	}

	if cfg.ShowArm {
		v.drawArm(g)
	}
	if full {
		v.w.present()
		v.needFull = false
	} else {
		clipReset(g)
		v.w.presentDirty(dirty)
	}
}

func snapArtist(s MediaSnap) string {
	if s.Track == nil {
		return ""
	}
	return s.Track.Artist
}

// ambColor: colour of the ambient light (and a second, slightly shifted one for the other blob).
func ambColor(t float64) (uint32, uint32) {
	var c uint32
	switch cfg.AmbColorMode {
	case 0:
		c = accent() | 0xFF000000
		if albumAccent != 0 {
			c = albumAccent | 0xFF000000
		}
	case 1:
		c = accent() | 0xFF000000
	case 2:
		c = parseHex(cfg.AmbColor)
	default:
		c = hueColor(t * 0.015)
	}
	shift := 0.05
	if cfg.AmbEffect == 4 { // drift: the colour wanders a little either way
		shift = 0.09 * math.Sin(t*0.35)
		c = hueShift(c, 0.06*math.Sin(t*0.21))
	}
	return c, hueShift(c, shift)
}

func hueShift(c uint32, d float64) uint32 {
	h, s, l := rgbToHsl(float64(c>>16&255), float64(c>>8&255), float64(c&255))
	r, g, b := hslToRgb(math.Mod(h+d+1, 1), s, l)
	return c&0xFF000000 | uint32(clamp255(r))<<16 | uint32(clamp255(g))<<8 | uint32(clamp255(b))
}

// ambientBlobs: two soft lights around (cx, cy) — never covering the middle more than a tint.
func ambientBlobs(g uintptr, cx, cy, r float32, t, bass float64, sx float32) {
	lv := ambLevel(cfg.AmbEffect, t, bass)
	a := cfg.AmbBright * lv * 0.55
	c1, c2 := ambColor(t)
	dx := float32(math.Sin(t*0.23)) * r * 0.06
	softGlow(g, cx+sx*r*0.12+dx, cy-r*0.1, r, withAlpha(c1, a))
	softGlow(g, cx-sx*r*0.18-dx, cy+r*0.06, r*0.78, withAlpha(c2, a*0.75))
}

func (v *VinylView) drawAmbient(g uintptr, full bool, dirty RECT) {
	R, cx, cy := float32(v.R), float32(v.cx), float32(v.cy)
	r := R * float32(1.62*cfg.AmbSize)
	// only around the record, not over it
	if full {
		clipRect(g, 0, 0, float32(v.w.w), float32(v.w.h))
	}
	disc := newPath()
	addEllipse(disc, cx-R, cy-R, 2*R, 2*R)
	clipExclude(g, disc)
	bass := float64(v.bass)
	if !v.playing {
		bass = 0
	}
	ambientBlobs(g, cx, cy, r, v.ambT, bass, float32(v.sx))
	freePath(disc)
	if full {
		clipReset(g)
	} else {
		clipRect(g, float32(dirty.Left), float32(dirty.Top), float32(dirty.Right-dirty.Left), float32(dirty.Bottom-dirty.Top))
	}
}

// drawCard: "now playing" beside the corner record — cover, title, artist, tilted a little.
func (v *VinylView) drawCard(g uintptr) {
	t := media.Snapshot().Track
	if t == nil {
		return
	}
	R := float32(v.R)
	ax, ay := float32(v.cx)+float32(v.sx)*R*1.14, float32(v.cy)-float32(v.sy)*R*0.6
	gTranslate(g, ax, ay)
	gRotate(g, float32(-13*v.sx*v.sy))
	defer gReset(g)
	cs := R * 0.27
	fN := font("Segoe UI", float64(R*0.062), true)
	fT := font("Segoe UI", float64(R*0.125), true)
	fA := font("Segoe UI", float64(R*0.09), false)
	maxW := R * 1.45
	tw, _ := measure(fT, t.Title)
	aw, _ := measure(fA, t.Artist)
	nw, _ := measure(fN, "NOW PLAYING")
	textW := float32(math.Min(float64(maxW), math.Max(float64(tw), math.Max(float64(aw), float64(nw)))))
	blockW := cs + R*0.08 + textW
	x0 := float32(0)
	if v.sx < 0 {
		x0 = -blockW
	}
	y0 := -cs / 2
	// soft shadow + cover thumb
	sh := roundRectPath(x0+2, y0+3, cs, cs, cs*0.16)
	fillPathColor(g, sh, 0x66000000)
	freePath(sh)
	if cover.img != 0 {
		p := roundRectPath(x0, y0, cs, cs, cs*0.16)
		clipPath(g, p)
		drawCoverSquare(g, cover.img, x0, y0, cs)
		clipReset(g)
		freePath(p)
	} else {
		p := roundRectPath(x0, y0, cs, cs, cs*0.16)
		fillPathColor(g, p, withAlpha(accent(), 0.5))
		freePath(p)
	}
	tx := x0 + cs + R*0.08
	acc := accent() | 0xFF000000
	txt := textColor()
	drawText(g, fN, "NOW PLAYING", tx, y0-R*0.005, maxW, acc)
	drawText(g, fT, t.Title, tx+1, y0+R*0.07+1.5, maxW, 0x66000000)
	drawText(g, fT, t.Title, tx, y0+R*0.07, maxW, txt)
	drawText(g, fA, t.Artist, tx, y0+R*0.07+R*0.16, maxW, withAlpha(txt, 0.65))
}

// drawGrooveLight: the music lights up the grooves — bass on the outside, highs near the label.
func (v *VinylView) drawGrooveLight(g uintptr) {
	R, cx, cy := float32(v.R), float32(v.cx), float32(v.cy)
	c := accent()
	if cfg.DiscStyle != discPicture && cfg.DiscStyle != discBlack {
		c = mixColor(discColor(), 0xFFFFFFFF, 0.35)
	}
	inner := float32(0.68)
	if cfg.DiscStyle == discPicture {
		inner = 0.4
	}
	const rings = 12
	for i := 0; i < rings; i++ {
		// ring 0 = outermost = lowest band
		lo, hi := i*40/rings, (i+1)*40/rings
		var lv float32
		for b := lo; b < hi; b++ {
			lv = float32(math.Max(float64(lv), float64(v.bands[b])))
		}
		lv = float32(math.Min(1, float64(lv)*cfg.HaloSens))
		if lv < 0.08 {
			continue
		}
		r := R * (0.975 - (0.975-inner)*float32(i)/float32(rings-1))
		strokeCircle(g, withAlpha(c, float64(lv)*0.55), cx, cy, r, R*0.012+lv*R*0.012)
	}
}

// drawArm: the pivot sits on the rim; the short arm reaches in over the record and slides
// toward the label as the song plays.
func (v *VinylView) drawArm(g uintptr) {
	R := v.R
	// local coordinates (bottom-left layout), centre at (0,0)
	pa := -57 * math.Pi / 180
	px, py := R*1.02*math.Cos(pa), R*1.02*math.Sin(pa)
	armLen := R * 0.74
	inner := 0.62
	if cfg.DiscStyle != discPicture {
		inner = 0.66
	}
	rt := R * (0.93 - (0.93-inner-0.02)*v.progress)
	if v.armT < 1 { // swing out to the rest beside the record
		t := v.armT * v.armT * (3 - 2*v.armT)
		rt = R*1.22 + (rt-R*1.22)*t
	}
	th := armAngle(px, py, 0, 0, armLen, rt)
	pt := func(lx, ly float64) (float32, float32) {
		return float32(v.cx + v.sx*lx), float32(v.cy + v.sy*ly)
	}
	Px, Py := pt(px, py)
	Sx, Sy := pt(px+armLen*math.Cos(th), py+armLen*math.Sin(th))
	Hx, Hy := pt(px+armLen*0.84*math.Cos(th), py+armLen*0.84*math.Sin(th))
	Bx, By := pt(px-R*0.16*math.Cos(th), py-R*0.16*math.Sin(th))
	r := float32(R)
	sh := r * float32(0.025+0.04*(1-v.armT)) // lifted needle → longer shadow
	line(g, 0x55000000, r*0.05, Px+sh, Py+sh*1.5, Hx+sh, Hy+sh*1.5)
	line(g, 0xFF8E9098, r*0.075, Px, Py, Bx, By)
	line(g, 0xFFD5D7DD, r*0.034, Px, Py, Hx, Hy)
	line(g, 0xFFB9BCC4, r*0.075, Hx, Hy, Sx, Sy)
	fillCircle(g, 0xFF1A1A1E, Sx, Sy, r*0.014)
	fillCircle(g, 0xFF2A2B30, Px, Py, r*0.11)
	fillCircle(g, 0xFFC9CBD2, Px, Py, r*0.078)
	fillCircle(g, 0x99FFFFFF, Px-r*0.022, Py-r*0.024, r*0.026)
}

/* ===================== Halo visualizer ===================== */

type HaloView struct {
	w       *Widget
	label   *Bitmap
	sig     string
	bars    []float32
	lin     [48]float32 // smoothed bands for the straight styles
	ambT    float64
	drawnAt time.Time
	idle    int
	spin    float64
	last    time.Time
}

func newHaloView() *HaloView {
	h := &HaloView{w: newWidget("halo", "Halo"), last: time.Now()}
	h.w.useSoft(0.5, false) // light and glow on the Mac's soft layer
	return h
}

func (h *HaloView) Update(now time.Time, snap MediaSnap, bands [nBands]float32, bass float32, active bool, ui float64) {
	dt := now.Sub(h.last).Seconds()
	h.last = now
	if !cfg.ShowHalo || snap.Track == nil {
		if h.w.canvas != nil {
			h.w.release()
			h.label.Free()
			h.label, h.sig = nil, ""
		}
		return
	}
	updateCover(snap.Track)
	S := haloSquare(cfg.widget("halo").Scale)
	HW, HH, _ := widgetSize("halo", cfg.widget("halo").Scale)
	p := cfg.widget("halo")
	wa := workArea()
	x, y := p.X, p.Y
	if !p.Set {
		x = int(wa.Left+wa.Right)/2 - S/2
		y = int(wa.Top+wa.Bottom)/2 - S/2
	}
	sizeChanged := h.w.w != HW || h.w.h != HH
	h.w.resize(HW, HH)
	if sizeChanged || layoutDirty || !h.w.visible {
		h.w.moveTo(x, y)
	}
	r := float32(S) * 0.235
	sig := fmt.Sprintf("%s|%d|%d|%x|%v|%d|%d|%d|%s|%s|%.2f|%v", cover.key, cover.ver, S, albumAccent, framed("halo"),
		cfg.HaloStyle, cfg.HaloPalette, cfg.HaloCard, cfg.HaloColorA, cfg.HaloColorB, cfg.HaloBackdrop, cfg.HaloArtGlow)
	if sig != h.sig || styleDirty {
		h.sig = sig
		L := int(r * 2)
		h.label.Free()
		h.label = newBitmap(L, L)
		if h.label != nil {
			g := h.label.g
			setInterp(g, interpHighQualityBicubic)
			half := float32(L) / 2
			fillCircle(g, 0xFF1C1C20, half, half, half)
			clip := clipCircle(g, half, half, half)
			drawCoverSquare(g, cover.img, 0, 0, float32(L))
			clipReset(g)
			freePath(clip)
		}
		needRedraw = true
	}
	n := cfg.HaloBars
	if len(h.bars) != n {
		h.bars = make([]float32, n)
	}
	animate := cfg.FPS > 0
	moving := false
	for i := 0; i < n; i++ {
		u := float64(i) / float64(n)
		m := u * 2
		if u >= 0.5 {
			m = (1 - u) * 2
		}
		t := float32(math.Min(1.2, float64(bands[int(m*46)])*cfg.HaloSens))
		if !snap.Playing || !animate {
			t = 0
		}
		h.bars[i] += (t - h.bars[i]) * 0.55
		if h.bars[i] > 0.01 {
			moving = true
		}
	}
	for k := range h.lin {
		t := float32(math.Min(1.2, float64(bands[k])*cfg.HaloSens))
		if !snap.Playing || !animate {
			t = 0
		}
		h.lin[k] += (t - h.lin[k]) * 0.55
		if h.lin[k] > 0.01 {
			moving = true
		}
	}
	if !animate {
		bass = 0
	}
	h.ambT += dt
	if cfg.AmbHalo && cfg.AmbBright > 0 && animate && snap.Playing && (cfg.AmbEffect != 0 || cfg.AmbColorMode == 3) {
		moving = true
	}
	if cfg.HaloSpin && snap.Playing && animate {
		h.spin = math.Mod(h.spin+dt*20, 360)
		moving = true
	}
	if !moving && !needRedraw && h.w.visible {
		h.idle++
		if h.idle > 2 {
			return
		}
	} else {
		h.idle = 0
	}
	h.drawnAt = now
	h.draw(S, bass)
}

// visColors: the visualizer gradient (quiet → loud).
func visColors() (uint32, uint32) {
	p := cfg.HaloPalette
	switch {
	case p == 0:
		a := accent() | 0xFF000000
		return a, mixColor(a, 0xFFFFFFFF, 0.45)
	case p == 1:
		a := accent() | 0xFF000000
		if albumAccent != 0 {
			a = albumAccent | 0xFF000000
		}
		return mixColor(a, 0xFF000000, 0.1), mixColor(a, 0xFFFFFFFF, 0.55)
	case p == len(visPalettes)-1:
		return parseHex(cfg.HaloColorA), parseHex(cfg.HaloColorB)
	}
	return visPalettes[p].A, visPalettes[p].B
}

// level of linear bar i of m (bass on the left)
func (h *HaloView) level(i, m int) float32 {
	k := int(float64(i) / float64(m) * 46)
	if k >= len(h.lin) {
		k = len(h.lin) - 1
	}
	return h.lin[k]
}

func (h *HaloView) draw(S int, bass float32) {
	if h.w.canvas == nil { // out of memory for the bitmap: skip the frame rather than crash
		return
	}
	g := h.w.g()
	W, H := float32(h.w.w), float32(h.w.h)
	sz := float32(S)
	c := sz / 2
	r := sz * 0.235
	ca, cb := visColors()
	gClear(g, 0)
	if cfg.HaloBackdrop > 0 {
		bg := roundRectPath(2, 2, W-4, H-4, sz*0.07)
		fillPathColor(g, bg, withAlpha(0xFF0E0E13, 0.9*cfg.HaloBackdrop))
		strokePath(g, bg, withAlpha(0xFFFFFFFF, 0.08*cfg.HaloBackdrop), 1)
		freePath(bg)
	}
	// the light behind it goes on the Mac's soft layer (not over a dark card: that would hide it)
	soft := cfg.HaloBackdrop <= 0 && softBegin(g)
	if cfg.AmbHalo && cfg.AmbBright > 0 {
		ambientBlobs(g, c, c, sz*float32(math.Min(0.42, 0.33*cfg.AmbSize)), h.ambT, float64(bass), 1)
	}
	if cfg.HaloArtGlow {
		ag := ca
		if albumAccent != 0 {
			ag = albumAccent | 0xFF000000
		}
		softGlow(g, c, c, sz*0.5, withAlpha(ag, 0.3+0.25*float64(bass)))
	}
	if cfg.HaloGlow > 0 {
		softGlow(g, c, c, r*1.55, withAlpha(ca, (0.16+0.3*float64(bass))*cfg.HaloGlow))
	}
	if soft {
		softEnd(g)
	}
	// the wide faint glow pass of the bars: on the soft layer too
	glowPass := func(pass int, draw func()) {
		if pass == 0 && cfg.HaloBackdrop <= 0 && softBegin(g) {
			draw()
			softEnd(g)
			return
		}
		draw()
	}
	// 8 pens from quiet to loud colour (+ wider, faint ones for the glow)
	const steps = 8
	style := cfg.HaloStyle
	colAt := func(t float32) uint32 {
		k := int(t * steps)
		if k >= steps {
			k = steps - 1
		}
		if k < 0 {
			k = 0
		}
		return mixColor(ca, cb, float64(k)/(steps-1))
	}
	switch style {
	case visHalo, visRing:
		n := len(h.bars)
		maxLen := sz * 0.2 * float32(cfg.HaloLength)
		if maxLen > sz*0.26 {
			maxLen = sz * 0.26
		}
		rb := r * 1.07
		if style == visHalo {
			bw := float32(math.Max(1.2, float64(2*math.Pi*float64(rb)/float64(n))*0.34))
			for pass := 0; pass < 2; pass++ {
				if pass == 0 && cfg.HaloGlow <= 0 {
					continue
				}
				glowPass(pass, func() {
					var pens [steps]uintptr
					for k := range pens {
						col := mixColor(ca, cb, float64(k)/(steps-1))
						if pass == 0 {
							pens[k] = newPen(withAlpha(col, 0.22*math.Min(1, cfg.HaloGlow)), bw*2.6)
						} else {
							pens[k] = newPen(withAlpha(col, 0.95), bw)
						}
					}
					for i := 0; i < n; i++ {
						a := -math.Pi/2 + float64(i)/float64(n)*2*math.Pi
						l := 2 + h.bars[i]*maxLen
						k := int(math.Min(steps-1, float64(h.bars[i])*steps))
						cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
						drawLinePen(g, pens[k], c+cs*rb, c+sn*rb, c+cs*(rb+l), c+sn*(rb+l))
					}
					for _, p := range pens {
						freePen(p)
					}
				})
			}
		} else { // Ring: a wobbling outline around the cover
			pt := func(i int) (float32, float32) {
				a := -math.Pi/2 + float64(i%n)/float64(n)*2*math.Pi
				l := rb + 4 + h.bars[i%n]*maxLen*0.75
				return c + l*float32(math.Cos(a)), c + l*float32(math.Sin(a))
			}
			for pass, wd := range []float32{sz * 0.022, sz * 0.006} {
				if pass == 0 && cfg.HaloGlow <= 0 {
					continue
				}
				glowPass(pass, func() {
					for i := 0; i < n; i++ {
						x1, y1 := pt(i)
						x2, y2 := pt(i + 1)
						col := colAt(h.bars[i])
						if pass == 0 {
							col = withAlpha(col, 0.2*math.Min(1, cfg.HaloGlow))
						}
						line(g, col, wd, x1, y1, x2, y2)
					}
				})
			}
			strokeCircle(g, withAlpha(ca, 0.35), c, c, rb+1, 1)
		}
		if h.label != nil {
			pulse := 1 + float32(cfg.HaloPulse)*bass
			L := float32(h.label.w) * pulse
			gTranslate(g, c, c)
			gRotate(g, float32(h.spin))
			gImage(g, h.label.bmp, -L/2, -L/2, L, L)
			gReset(g)
			strokeCircle(g, withAlpha(0xFFFFFFFF, 0.35), c, c, L/2, 1.5)
		}
	case visBars, visMirror:
		m := 56
		x0, w := sz*0.08, sz*0.84
		bw := w / float32(m)
		base := sz * 0.72
		amp := sz * 0.5 * float32(cfg.HaloLength)
		if style == visMirror {
			base, amp = c, sz*0.3*float32(cfg.HaloLength)
		}
		for i := 0; i < m; i++ {
			lv := h.level(i, m)
			hgt := 2 + lv*amp
			x := x0 + float32(i)*bw + bw*0.18
			col := colAt(lv)
			if cfg.HaloGlow > 0 {
				fillRect(g, withAlpha(col, 0.12*math.Min(1, cfg.HaloGlow)), x-bw*0.15, base-hgt-2, bw*0.94, hgt+2)
			}
			fillRect(g, col, x, base-hgt, bw*0.64, hgt)
			if style == visMirror {
				fillRect(g, withAlpha(col, 0.55), x, base+1, bw*0.64, hgt*0.8)
			}
		}
	case visBlocks:
		cols, rows := 22, 14
		x0, w := sz*0.1, sz*0.8
		y0, hh := sz*0.14, sz*0.68
		cw, ch := w/float32(cols), hh/float32(rows)
		for i := 0; i < cols; i++ {
			lv := h.level(i, cols) * float32(cfg.HaloLength)
			lit := int(lv*float32(rows) + 0.5)
			for j := 0; j < rows; j++ {
				x, y := x0+float32(i)*cw, y0+hh-float32(j+1)*ch
				col := uint32(0x14FFFFFF)
				if j < lit {
					col = mixColor(ca, cb, float64(j)/float64(rows-1))
				}
				fillRect(g, col, x+cw*0.12, y+ch*0.14, cw*0.76, ch*0.72)
			}
		}
	case visWave:
		m := 64
		x0, w := sz*0.06, sz*0.88
		amp := sz * 0.28 * float32(cfg.HaloLength)
		pts := make([][2]float32, m)
		for i := 0; i < m; i++ {
			// smooth: average with neighbours, taper at both ends
			lv := (h.level(maxI(i-1, 0), m) + 2*h.level(i, m) + h.level(minI(i+1, m-1), m)) / 4
			taper := float32(math.Sin(math.Pi * float64(i) / float64(m-1)))
			pts[i] = [2]float32{x0 + w*float32(i)/float32(m-1), lv * amp * taper}
		}
		for pass, wd := range []float32{sz * 0.03, sz * 0.008} {
			if pass == 0 && cfg.HaloGlow <= 0 {
				continue
			}
			glowPass(pass, func() {
				for side, sgn := range []float32{-1, 1} {
					for i := 0; i+1 < m; i++ {
						col := colAt(pts[i][1] / amp)
						a := 1.0
						if side == 1 {
							a = 0.5
						}
						if pass == 0 {
							a *= 0.18 * math.Min(1, cfg.HaloGlow)
						}
						line(g, withAlpha(col, a), wd, pts[i][0], c+sgn*pts[i][1], pts[i+1][0], c+sgn*pts[i+1][1])
					}
				}
			})
		}
	}
	if cfg.HaloCard > 0 {
		h.drawCard(g, sz, W, H)
	}
	if framed("halo") {
		editFrame(g, W, H, "halo", "Visualizer")
	}
	h.w.present()
}

// drawCard: now playing under the visualizer.
func (h *HaloView) drawCard(g uintptr, sz, W, H float32) {
	t := media.Snapshot().Track
	if t == nil {
		return
	}
	txt := textColor()
	fT := font("Segoe UI", float64(sz*0.042), true)
	fA := font("Segoe UI", float64(sz*0.032), false)
	tw, th := measure(fT, t.Title)
	aw, _ := measure(fA, t.Artist)
	maxW := W * 0.8
	tw, aw = float32(math.Min(float64(tw), float64(maxW))), float32(math.Min(float64(aw), float64(maxW)))
	y := sz*1.0 + sz*0.01
	if cfg.HaloCard == 1 && cover.img != 0 { // compact: cover thumb + text, centred as a block
		cs := sz * 0.1
		bw := cs + sz*0.03 + float32(math.Max(float64(tw), float64(aw)))
		x := (W - bw) / 2
		path := roundRectPath(x, y, cs, cs, cs*0.16)
		clipPath(g, path)
		drawCoverSquare(g, cover.img, x, y, cs)
		clipReset(g)
		freePath(path)
		tx := x + cs + sz*0.03
		drawText(g, fT, t.Title, tx, y+cs*0.02, maxW, txt)
		drawText(g, fA, t.Artist, tx, y+cs*0.02+th*1.05, maxW, withAlpha(txt, 0.6))
		return
	}
	drawText(g, fT, t.Title, (W-tw)/2, y, maxW, txt)
	drawText(g, fA, t.Artist, (W-aw)/2, y+th*1.05, maxW, withAlpha(txt, 0.6))
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

/* ===================== Lyrics ===================== */

type run struct {
	text   string
	x, y   float32
	f      *Font
	color  uint32
	rot    float32
	caret  bool
	cw, ch float32
	alpha  float32 // 0 = invisible, 1 = normal (multiplies the colour alpha)
	dy     float32 // animation offset
	dx     float32
	scale  float32 // animation scale (0 = 1)
	rotC   bool    // rotate around the run's centre (Orbit)
	bg     uint32  // highlighter box behind the text
	fill   float32 // >0: only this fraction (left→right) is bright, rest dim (Glass)
	chroma float32 // RGB-split offset in px (Glitch)
	haze   float32 // 0..1 soft blur (Scatter landing, Focus)
	hollow bool    // with fill: the unsung part is an outline (Ink)
	kind   int     // 0 text, 1 ball, 2 line to (x2,y2), 3 star symbol, 4 ring
	x2, y2 float32
	size   float32 // ball / star / ring radius, line width
	shape  int     // star symbol
}

const (
	rkText = iota
	rkBall
	rkLine
	rkStar
	rkRing
)

// drawShape draws the non-text runs (Constellation, Bouncing).
func drawShape(g uintptr, r run, ox, oy float32) {
	col := withAlpha(r.color, float64(r.alpha))
	x, y := ox+r.x+r.dx, oy+r.y+r.dy
	sz := r.size
	if r.scale != 0 {
		sz *= r.scale
	}
	a := float64(col>>24) / 255
	switch r.kind {
	case rkBall:
		softGlow(g, x, y, sz*2.6, withAlpha(col, 0.5*a))
		fillCircle(g, col, x, y, sz)
		fillCircle(g, withAlpha(0xFFFFFFFF, 0.55*a), x-sz*0.3, y-sz*0.32, sz*0.34)
	case rkLine:
		line(g, col, sz, x, y, ox+r.x2, oy+r.y2)
	case rkRing:
		strokeCircle(g, col, x, y, sz, float32(math.Max(1, float64(sz)*0.07)))
	case rkStar:
		softGlow(g, x, y, sz*2.4, withAlpha(col, 0.42*a))
		switch r.shape {
		case 3: // orb
			fillCircle(g, col, x, y, sz*0.42)
			fillCircle(g, withAlpha(0xFFFFFFFF, a), x, y, sz*0.2)
		case 4: // cross: a thin plus with a bright centre
			w := float32(math.Max(1, float64(sz)*0.12))
			line(g, col, w, x-sz, y, x+sz, y)
			line(g, col, w, x, y-sz, x, y+sz)
			fillCircle(g, col, x, y, sz*0.16)
		default:
			pts := starPoints(r.shape, float64(sz))
			pp := make([]PointF, len(pts))
			for i, q := range pts {
				pp[i] = PointF{x + float32(q[0]), y + float32(q[1])}
			}
			fillPoly(g, col, pp)
			fillCircle(g, withAlpha(0xFFFFFFFF, 0.8*a), x, y, sz*0.12)
		}
	}
}

// lyricsVer: the song version whose lyrics were last asked for.
var lyricsVer = -1

type LyricsView struct {
	lines     []Line // the song's lines (Constellation reaches back a few lines)
	w         *Widget
	version   int
	sig       string
	idx       int
	fadeFrom  time.Time
	rng       *rand.Rand
	last      [2]int
	shown     bool
	lineStart time.Time
}

func newLyricsView() *LyricsView {
	lv := &LyricsView{w: newWidget("lyrics", "Lyrics"), version: -1, idx: -2, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	lv.w.useSoft(0.5, false) // the glows on the Mac's soft layer
	return lv
}

func (lv *LyricsView) fontSize(ui float64) float64 {
	return cfg.TextSize * ui * cfg.widget("lyrics").Scale
}

func upper(s string) string {
	switch cfg.TextCase {
	case 1:
		return strings.ToUpper(s)
	case 2:
		return strings.ToLower(s)
	}
	return s
}

func easeOut(t float64) float64 { return 1 - math.Pow(1-t, 3) }
func easeOutBack(t float64) float64 {
	c1, c3 := 1.70158, 2.70158
	return 1 + c3*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}

func (lv *LyricsView) Update(now time.Time, snap MediaSnap, bass float32, ui float64) {
	if snap.Version != lv.version {
		lv.version = snap.Version
		if snap.Version != lyricsVer { // the desktop and the full-screen lyrics share one download
			lyricsVer = snap.Version
			lyrics.Load(snap.Track)
		}
		lv.idx = -2
	}
	showFrame := framed("lyrics")
	if !cfg.ShowLyrics || (snap.Track == nil && !showFrame) {
		lv.fadeOut()
		return
	}
	_, status, lines := lyrics.Get()
	lv.lines = lines
	p := snap.Pos
	idx := -1
	if status == lyrSynced || status == lyrPlain {
		idx = lineAt(lines, p)
	}
	var L *Line
	if idx >= 0 {
		L = &lines[idx]
		// hide during long instrumental gaps
		if L.Text == "" || (p-L.T > L.Sing+6 && L.End-L.T > L.Sing+9) {
			L = nil
		}
	}
	if L == nil && !showFrame {
		lv.fadeOut()
		return
	}
	F := lv.fontSize(ui)
	style := cfg.LyricsStyle % len(styleNames)
	animate := cfg.FPS > 0

	sample := Line{Text: "Lyrics show up here", T: 0, Sing: 1, End: 5}
	if L == nil {
		sample.Words = []Word{{"Lyrics", 0}, {"show", 0.2}, {"up", 0.4}, {"here", 0.6}}
		L = &sample
		p = 99
		idx = -1
	}
	// styles that bring each word in with a motion start it a moment early, so the word is
	// readable on the beat instead of still flying in; the rest only make up for drawing time
	if idx >= 0 {
		p += visualLead(style)
	}
	wi := wordAt(L, p)
	ws := make([]string, len(L.Words))
	for i, w := range L.Words {
		ws[i] = upper(w.W)
	}
	chars := len([]rune(strings.Join(ws, " ")))
	typed := typedChars(L, ws, p) // Typewriter follows the words' own times, not an even pace
	newLine := idx != lv.idx
	if newLine {
		lv.idx = idx
		lv.lineStart = now
	}
	caret := style == stTypewriter && animate && (typed < chars || now.Sub(lv.lineStart) < 2500*time.Millisecond) && (now.UnixMilli()/450)%2 == 0

	// time-based animations (Rise, Pop, Neon flicker) need a redraw every frame while running
	animKey := int64(0)
	if animate {
		switch style {
		case stRise:
			for i := 0; i <= wi && i < len(L.Words); i++ {
				if p-L.Words[i].T < 0.45 {
					animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
				}
			}
		case stPop, stWordReel:
			if wi >= 0 && p-L.Words[wi].T < 0.3 {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stNeon:
			if now.Sub(lv.lineStart) < 700*time.Millisecond {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stBounce, stSlide:
			for i := 0; i <= wi && i < len(L.Words); i++ {
				if p-L.Words[i].T < 0.6 {
					animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
				}
			}
		case stGlass:
			if wi >= 0 && p-L.Words[wi].T < wordDur(L, wi)+0.05 {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stWave: // always moving; 30 fps is plenty for a slow wave and halves the cost
			fps := cfg.FPS
			if fps > 30 {
				fps = 30
			}
			animKey = now.UnixMilli() / int64(1000/fps+1)
		case stGlitch:
			if (wi >= 0 && p-L.Words[wi].T < 0.45) || now.UnixMilli()%2300 < 160 {
				animKey = now.UnixMilli() / 45
			}
		case stDecode:
			if wi >= 0 && p-L.Words[wi].T < 0.5 {
				animKey = now.UnixMilli() / 55
			}
		case stEcho:
			if wi >= 0 && p-L.Words[wi].T < 0.95 {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stConstellation: // a new star pops in, or the finished sky fades in
			if wi >= 0 && (p-L.Words[wi].T < 0.45 || (wi == len(L.Words)-1 && p-L.Words[wi].T < 1.5)) {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stBouncing: // the ball is always on its way somewhere, except resting on the last word
			if !(wi >= len(L.Words)-1 && wi >= 0 && p-L.Words[wi].T > 0.7) {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stScatter, stSwing, stStack:
			for i := 0; i <= wi && i < len(L.Words); i++ {
				if p-L.Words[i].T < 0.65 {
					animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
				}
			}
		case stRain, stInk:
			if wi >= 0 && p-L.Words[wi].T < wordDur(L, wi)+0.4 {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stSplit, stFocus:
			if now.Sub(lv.lineStart) < 650*time.Millisecond {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stSubtitle:
			animKey = int64(math.Min(1, math.Max(0, (p-L.T)/math.Max(0.4, L.Sing))) * 60) // the bar moves in 60 steps
		case stOrbit, stSpotlight:
			if wi >= 0 && p-L.Words[wi].T < 0.35 {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		}
	}
	var prev []string
	for k := 1; k <= 3; k++ {
		if idx-k >= 0 && style == stFlow {
			prev = append(prev, lines[idx-k].Text)
		} else {
			prev = append(prev, "")
		}
	}
	next := ""
	if idx >= 0 && idx+1 < len(lines) {
		next = lines[idx+1].Text
	}
	progressKey := wi
	if style == stTypewriter {
		progressKey = typed
	}
	if style == stSubtitle {
		progressKey = 0
	}
	sig := fmt.Sprintf("%d|%d|%d|%v|%d|%s|%.1f|%v|%v|%v|%d|%v|%x|%s|%d|%.2f|%.0f|%s|%s", style, idx, progressKey, caret, animKey,
		cfg.FontName, F, cfg.Bold, cfg.TextCase, cfg.ShowPrevLine, cfg.Theme, showFrame, albumAccent, L.Text, cfg.Effect, cfg.GlowAmount, cfg.LyricsWidth, cfg.TextColor, cfg.AccentColor)
	sig += fmt.Sprint("|", cfg.StarShape)
	// music-driven motion (Pulse / Stretch / Glow) and the Dissolve haze, quantised so we only
	// redraw when it visibly changes
	lyrMotion, lyrHaze = 0, 0
	if animate && L != &sample {
		switch cfg.Motion {
		case moPulse, moStretch, moGlow:
			lyrMotion = float32(math.Round(math.Min(1, float64(bass)*1.3)*cfg.MotionAmount*16) / 16)
		case moDissolve:
			hz := 0.0
			if t := now.Sub(lv.lineStart).Seconds(); t < 0.5 {
				hz = 1 - t/0.5
			}
			if e := L.End - p; L.End > L.T && e < 0.55 {
				hz = math.Max(hz, 1-math.Max(0, e)/0.55)
			}
			lyrHaze = float32(math.Round(math.Min(1, hz*math.Max(0.3, cfg.MotionAmount))*12) / 12)
		}
	}
	sig += fmt.Sprintf("|%d|%.3f|%.3f|%.2f", cfg.Motion, lyrMotion, lyrHaze, cfg.MotionAmount)
	if sig != lv.sig || styleDirty || layoutDirty {
		lv.sig = sig
		runs, bw, bh := lv.layout(style, L, idx, wi, typed, caret, prev, next, F, p, now)
		lv.render(runs, bw, bh, F, style)
		lv.place(newLine || layoutDirty || !lv.shown, ui)
		if (newLine || !lv.shown) && animate {
			lv.fadeFrom = now
			lv.w.alpha = 0
		} else if lv.fadeFrom.IsZero() {
			lv.w.alpha = 255
			lv.w.present()
		}
	}
	if !lv.fadeFrom.IsZero() {
		t := now.Sub(lv.fadeFrom).Seconds() / 0.22
		if t < 1 {
			lv.w.alpha = byte(255 * t)
		} else {
			lv.w.alpha = 255
			lv.fadeFrom = time.Time{}
		}
		lv.w.present()
	}
	lv.shown = true
}

func (lv *LyricsView) fadeOut() {
	if !lv.shown {
		return
	}
	if lv.w.alpha > 30 && cfg.FPS > 0 {
		lv.w.alpha -= 30
		lv.w.present()
		return
	}
	lv.w.alpha = 0
	lv.w.hide()
	lv.shown = false
	lv.sig = ""
	lv.idx = -2
}

// layout turns the current state into positioned text runs (centre-relative x, top-relative y).
func (lv *LyricsView) layout(style int, L *Line, idx, wi, typed int, caret bool, prev []string, next string, F float64, p float64, now time.Time) ([]run, float32, float32) {
	txt := textColor()
	acc := accent()
	fontName := cfg.FontName
	big := font(fontName, F, cfg.Bold)
	small := font(fontName, F*0.42, false)
	spaceW, _ := measure(big, " ")
	maxW := float64(workWidth()) * cfg.LyricsWidth / 100
	if maxW > F*28 {
		maxW = F * 28
	}
	words := make([]string, len(L.Words))
	widths := make([]float64, len(L.Words))
	for i, w := range L.Words {
		words[i] = upper(w.W)
		ww, _ := measure(big, words[i])
		widths[i] = float64(ww)
	}
	animate := cfg.FPS > 0
	var runs []run
	y := float32(0)
	lineH := float32(F * 1.18)
	boxW := float32(0)

	addCentered := func(rowRuns []run, rowW float32) {
		for i := range rowRuns {
			rowRuns[i].x -= rowW / 2
		}
		runs = append(runs, rowRuns...)
		if rowW > boxW {
			boxW = rowW
		}
	}
	textLine := func(s string, f *Font, alpha float64, gapAfter float32) {
		if s == "" {
			return
		}
		s = upper(s)
		sw, _ := measure(f, s)
		if float64(sw) > maxW*1.25 {
			rs := []rune(s)
			s = string(rs[:int(float64(len(rs))*maxW*1.25/float64(sw))]) + "…"
			sw, _ = measure(f, s)
		}
		addCentered([]run{{text: s, y: y, f: f, color: withAlpha(txt, alpha), alpha: 1}}, sw)
		y += gapAfter
	}
	smallLine := func(s string, alpha float64) { textLine(s, small, alpha, float32(F*0.62)) }
	type wstate struct {
		show                bool
		color               uint32
		f                   *Font
		alpha, dy, dx       float32
		scale, fill, chroma float32
		bg                  uint32
		text                string // drawn instead of the word (Decode)
		rot, haze           float32
		rotC, hollow        bool
	}
	wordRows := func(state func(i int) wstate) {
		rows := balanceRows(widths, float64(spaceW), maxW)
		for _, row := range rows {
			x := float32(0)
			var rr []run
			for k, i := range row {
				st := state(i)
				w := float32(widths[i])
				if st.f != big {
					w2, _ := measure(st.f, words[i])
					w = w2
				}
				if st.show {
					fy := y
					if st.f != big {
						fy += (float32(F) - st.f.size) * 0.55
					}
					a := st.alpha
					if a == 0 {
						a = 1
					}
					t := words[i]
					if st.text != "" {
						t = st.text
					}
					rr = append(rr, run{text: t, x: x, y: fy, f: st.f, color: st.color, alpha: a, dy: st.dy, dx: st.dx,
						scale: st.scale, fill: st.fill, chroma: st.chroma, bg: st.bg, rot: st.rot, rotC: st.rotC, haze: st.haze, hollow: st.hollow})
				}
				x += w
				if k < len(row)-1 {
					x += spaceW
				}
			}
			addCentered(rr, x)
			y += lineH
		}
	}
	since := func(i int) float64 { // seconds since word i started
		if i < 0 || i >= len(L.Words) {
			return 99
		}
		return p - L.Words[i].T
	}

	switch style {
	case stTypewriter: // the line is typed out letter by letter
		rows := balanceRows(widths, float64(spaceW), maxW)
		left := typed
		caretX, caretY := float32(0), y
		for ri, row := range rows {
			parts := make([]string, len(row))
			for k, i := range row {
				parts[k] = words[i]
			}
			rowText := strings.Join(parts, " ")
			rr := []rune(rowText)
			fullW, _ := measure(big, rowText)
			if fullW > boxW {
				boxW = fullW // the box keeps its final size while typing → no jumping
			}
			if ri == 0 {
				caretX = -fullW / 2
			}
			n := left
			if n > len(rr) {
				n = len(rr)
			}
			if n > 0 {
				shown := string(rr[:n])
				sw, _ := measure(big, shown)
				runs = append(runs, run{text: shown, x: -fullW / 2, y: y, f: big, color: txt, alpha: 1})
				caretX, caretY = -fullW/2+sw, y
			}
			left -= len(rr) + 1
			y += lineH
		}
		if caret {
			runs = append(runs, run{caret: true, x: caretX + float32(F)*0.03, y: caretY + float32(F)*0.14, cw: float32(F) * 0.075, ch: float32(F) * 0.92, color: withAlpha(txt, 0.9), alpha: 1})
		}

	case stFlow: // previous / current / next lines, current one bright
		smallLine(prev[0], 0.35)
		wordRows(func(i int) wstate { return wstate{show: true, color: txt, f: big} })
		smallLine(next, 0.35)

	case stWordReel: // one big word at a time; the last one flies up and vanishes
		cur := "♪"
		if wi >= 0 && wi < len(words) {
			cur = words[wi]
		}
		hf := font(fontName, F*1.35, cfg.Bold)
		cw, ch := measure(hf, cur)
		y = float32(F * 0.9) // room for the word flying out
		t := 1.0
		if animate {
			t = easeOut(math.Min(1, since(wi)/0.3))
		}
		if wi > 0 && t < 1 {
			pw := words[wi-1]
			pwW, _ := measure(hf, pw)
			addCentered([]run{{text: pw, y: y, f: hf, color: txt, alpha: float32(1 - t), dy: -float32(t * F * 0.9), scale: float32(1 - 0.3*t)}}, pwW)
		}
		addCentered([]run{{text: cur, y: y, f: hf, color: txt, alpha: float32(math.Max(0.02, t)), dy: float32((1 - t) * F * 0.45)}}, cw)
		y += ch + float32(F*0.3)

	case stCinematic: // words light up as they are sung
		wordRows(func(i int) wstate {
			if i <= wi {
				return wstate{show: true, color: txt, f: big}
			}
			return wstate{show: true, color: withAlpha(txt, 0.2), f: big}
		})

	case stKinetic: // small words small, the longest word in the accent colour, words appear as sung
		longest, lw := -1, 0
		for i, w := range words {
			if n := len([]rune(w)); n > lw && len(words) > 3 {
				longest, lw = i, n
			}
		}
		smallF := font(fontName, F*0.45, cfg.Bold)
		for i, w := range words {
			words[i] = strings.ToUpper(w)
			f := big
			if isSmallWord(w) {
				f = smallF
			}
			ww, _ := measure(f, words[i])
			widths[i] = float64(ww)
		}
		wordRows(func(i int) wstate {
			f := big
			if isSmallWord(words[i]) {
				f = smallF
			}
			col := txt
			if i == longest {
				col = acc
			}
			return wstate{show: i <= wi, color: col, f: f}
		})

	case stNeon: // glowing tube letters; flickers on when the line starts
		a := float32(1)
		if animate {
			t := now.Sub(lv.lineStart).Seconds()
			switch {
			case t < 0.08:
				a = 0.2
			case t < 0.14:
				a = 1
			case t < 0.2:
				a = 0.35
			case t < 0.5:
				a = 0.9
			}
		}
		wordRows(func(i int) wstate {
			c := mixColor(txt, acc, 0.25)
			if i > wi {
				return wstate{show: true, color: withAlpha(c, 0.35), f: big, alpha: a}
			}
			return wstate{show: true, color: c, f: big, alpha: a}
		})

	case stSubtitle: // the whole line, with a thin bar showing how far it has been sung
		wordRows(func(i int) wstate { return wstate{show: true, color: txt, f: big} })
		bw := boxW * 0.6
		pr := float32(1)
		if animate {
			pr = float32(math.Min(1, math.Max(0, (p-L.T)/math.Max(0.4, L.Sing))))
		}
		bh := float32(math.Max(2, F*0.05))
		runs = append(runs, run{caret: true, x: -bw / 2, y: y + float32(F*0.12), cw: bw, ch: bh, color: withAlpha(txt, 0.18), alpha: 1},
			run{caret: true, x: -bw / 2, y: y + float32(F*0.12), cw: bw * pr, ch: bh, color: acc, alpha: 1})
		y += float32(F * 0.45)

	case stStack: // a tower: one word per row, every new word drops onto the pile
		sf := font(fontName, F*0.82, cfg.Bold)
		rowH := float32(F * 0.92)
		n := 0
		for i := 0; i <= wi && i < len(words); i++ {
			n++
		}
		if n == 0 {
			n = 1
		}
		for i := 0; i <= wi && i < len(words); i++ {
			ww, _ := measure(sf, words[i])
			t := 1.0
			if animate {
				t = math.Min(1, since(i)/0.45)
			}
			col := txt
			if i == wi {
				col = acc
			}
			addCentered([]run{{text: words[i], y: y + float32(i)*rowH, f: sf, color: col, alpha: float32(math.Min(1, t*3) + 0.01),
				dy: -float32((1 - easeOutBounce(t)) * F * 1.4)}}, ww)
		}
		y += float32(n)*rowH + float32(F*0.25)

	case stRise: // each word rises into place as it is sung
		wordRows(func(i int) wstate {
			if i > wi {
				return wstate{show: false, f: big}
			}
			t := 1.0
			if animate {
				t = easeOut(math.Min(1, since(i)/0.4))
			}
			return wstate{show: true, color: txt, f: big, alpha: float32(math.Max(0.02, t)), dy: float32((1 - t) * F * 0.7)}
		})

	case stPop: // one word at a time, popping in
		cur := "♪"
		if wi >= 0 && wi < len(words) {
			cur = words[wi]
		}
		hf := font(fontName, F*1.5, cfg.Bold)
		cw, _ := measure(hf, cur)
		sc := float32(1)
		if animate {
			sc = float32(0.45 + 0.55*easeOutBack(math.Min(1, since(wi)/0.28)))
		}
		addCentered([]run{{text: cur, y: y, f: hf, color: txt, alpha: 1, scale: sc}}, cw)
		y += float32(F * 1.7)

	case stKaraoke:
		wordRows(func(i int) wstate {
			if i <= wi {
				return wstate{show: true, color: acc, f: big}
			}
			return wstate{show: true, color: withAlpha(txt, 0.55), f: big}
		})

	case stScatter: // words land in random spots of a box: they glide in, shrink into place and settle
		W := maxW * 0.95
		H := F * 4.2
		spots := scatterLayout(len(words), int64((idx+2)*7919+len(L.Text)), W, H)
		for i := range words {
			if i > wi {
				continue
			}
			sp := spots[i]
			f := font(fontName, F*sp.Scale, cfg.Bold)
			ww, hh := measure(f, words[i])
			t := 1.0
			if animate {
				t = math.Min(1, since(i)/0.6)
			}
			e := easeOut(t)
			col := txt
			if i == wi {
				col = mixColor(acc, 0xFFFFFFFF, 0.35*(1-e)) // a little flash as it lands
			}
			// drift in from towards the middle of the box
			dx := float32((W/2 - sp.X) * 0.18 * (1 - e))
			r := run{text: words[i], x: float32(sp.X) - ww/2 - float32(W)/2, y: float32(sp.Y) - hh/2, f: f, color: col,
				rot: float32(sp.Rot * (0.4 + 0.6*e)), rotC: true, alpha: float32(math.Max(0.02, math.Min(1, t*2.2))),
				scale: float32(1 + 0.55*(1-easeOutBack(t))), dx: dx, dy: float32((1 - e) * F * 0.25)}
			if t < 1 {
				r.haze = float32(1 - e) // comes out of a soft blur
			}
			runs = append(runs, r)
		}
		boxW = float32(W)
		y = float32(H)

	case stGlass: // Apple-Music-like: the whole line is dim, light sweeps through each word as it is sung
		wordRows(func(i int) wstate {
			switch {
			case i < wi:
				return wstate{show: true, color: txt, f: big}
			case i == wi:
				f := 1.0
				lift := float32(0)
				if animate {
					f = math.Min(1, since(i)/wordDur(L, i))
					lift = -float32(easeOut(math.Min(1, since(i)/0.35)) * F * 0.06)
				}
				return wstate{show: true, color: txt, f: big, fill: float32(math.Max(0.02, f)), dy: lift}
			}
			return wstate{show: true, color: withAlpha(txt, 0.35), f: big}
		})

	case stBounce: // words drop in and bounce as they are sung
		wordRows(func(i int) wstate {
			if i > wi {
				return wstate{show: false, f: big}
			}
			t := 1.0
			if animate {
				t = math.Min(1, since(i)/0.55)
			}
			col := txt
			if i == wi {
				col = acc
			}
			return wstate{show: true, color: col, f: big, alpha: float32(math.Min(1, t*4) + 0.01), dy: -float32((1 - easeOutBounce(t)) * F * 0.9)}
		})

	case stWave: // the line floats on a slow wave
		ph := float64(now.UnixMilli()%100000) / 1000
		wordRows(func(i int) wstate {
			dy := float32(0)
			if animate {
				dy = float32(math.Sin(ph*3.2-float64(i)*0.75) * F * 0.11)
			}
			col := withAlpha(txt, 0.4)
			if i < wi {
				col = txt
			} else if i == wi {
				col = acc
			}
			return wstate{show: true, color: col, f: big, dy: dy}
		})

	case stGlitch: // RGB split + jitter whenever a word lands, and random little glitches
		burst := animate && now.UnixMilli()%2300 < 140
		slot := now.UnixMilli() / 45
		wordRows(func(i int) wstate {
			col := withAlpha(txt, 0.28)
			if i <= wi {
				col = txt
			}
			st := wstate{show: true, color: col, f: big}
			if animate && ((i == wi && since(i) < 0.4) || (burst && i <= wi)) {
				h := hash32(uint32(slot)*31 + uint32(i)*977)
				st.chroma = float32(F) * (0.03 + float32(h%5)*0.012)
				st.dx = float32(int(h>>8%7)-3) * float32(F) * 0.025
				st.dy = float32(int(h>>12%5)-2) * float32(F) * 0.015
			}
			return st
		})

	case stDecode: // hacker-style: words scramble and resolve as they are sung
		slot := now.UnixMilli() / 55
		wordRows(func(i int) wstate {
			rs := []rune(words[i])
			switch {
			case i < wi || (i == wi && (!animate || since(i) >= 0.42)):
				return wstate{show: true, color: txt, f: big}
			case i == wi:
				k := int(float64(len(rs)) * since(i) / 0.42)
				return wstate{show: true, color: acc, f: big, text: scramble(rs, k, uint32(slot)+uint32(i)*131)}
			}
			return wstate{show: true, color: withAlpha(txt, 0.22), f: big, text: scramble(rs, 0, uint32(i)*7919+uint32(idx+2)*31)}
		})

	case stHighlighter: // a marker sweeps across the sung words
		dark := uint32(0xFF101010)
		if lum(acc) < 0.45 {
			dark = 0xFFFFFFFF
		}
		wordRows(func(i int) wstate {
			if i <= wi {
				return wstate{show: true, color: dark, f: big, bg: acc}
			}
			return wstate{show: true, color: withAlpha(txt, 0.6), f: big}
		})

	case stEcho: // the current word leaves fading echoes above it
		wordRows(func(i int) wstate {
			if i > wi {
				return wstate{show: true, color: withAlpha(txt, 0.18), f: big}
			}
			if i == wi {
				return wstate{show: true, color: acc, f: big}
			}
			return wstate{show: true, color: txt, f: big}
		})
		fade := 1.0
		if animate && wi >= 0 {
			fade = math.Max(0, 1-since(wi)/0.9)
		}
		if fade > 0 && wi >= 0 && wi < len(words) {
			var echoes []run
			for _, r := range runs {
				if r.color == acc && r.text == words[wi] {
					for k := 3; k >= 1; k-- {
						e := r
						e.dy = -float32(k) * float32(F) * 0.22 * float32(0.4+0.6*fade)
						e.alpha = float32(0.32 / float64(k) * fade)
						e.scale = 1 + float32(k)*0.06
						echoes = append(echoes, e)
					}
				}
			}
			runs = append(echoes, runs...)
		}

	case stOrbit: // the line bends over an arc, words land on it as they are sung
		total := 0.0
		for i := range widths {
			total += widths[i]
			if i > 0 {
				total += float64(spaceW)
			}
		}
		R := math.Max(maxW*0.62, total*0.75)
		start := -total / R / 2
		pos := 0.0
		minY, maxY, maxX := math.MaxFloat64, -math.MaxFloat64, 0.0
		type placed struct {
			r  run
			cy float64
		}
		var ps []placed
		for i := range words {
			mid := start + (pos+widths[i]/2)/R
			pos += widths[i] + float64(spaceW)
			cx, cy := R*math.Sin(mid), R*(1-math.Cos(mid))
			minY, maxY = math.Min(minY, cy), math.Max(maxY, cy)
			maxX = math.Max(maxX, math.Abs(cx)+widths[i]/2)
			if i > wi {
				continue
			}
			col := txt
			if i == wi {
				col = acc
			}
			sc := float32(1)
			if animate && i == wi {
				sc = float32(0.6 + 0.4*easeOutBack(math.Min(1, since(i)/0.3)))
			}
			ps = append(ps, placed{run{text: words[i], x: float32(cx - widths[i]/2), f: big, color: col, alpha: 1, rot: float32(mid * 180 / math.Pi), rotC: true, scale: sc}, cy})
		}
		if len(words) == 0 {
			minY, maxY = 0, 0
		}
		top := minY - F*0.15
		for _, pl := range ps {
			pl.r.y = float32(pl.cy - top)
			runs = append(runs, pl.r)
		}
		boxW = float32(maxX*2 + F*0.6)
		y = float32(maxY-top) + float32(F*1.35)

	case stSlide: // words glide in from alternating sides
		wordRows(func(i int) wstate {
			if i > wi {
				return wstate{show: false, f: big}
			}
			t := 1.0
			if animate {
				t = easeOut(math.Min(1, since(i)/0.38))
			}
			dir := float32(1)
			if i%2 == 1 {
				dir = -1
			}
			col := txt
			if i == wi {
				col = mixColor(txt, acc, 0.6)
			}
			return wstate{show: true, color: col, f: big, alpha: float32(math.Max(0.02, t)), dx: dir * float32((1-t)*F*0.9)}
		})

	case stSpotlight: // only the sung word is lit, its neighbours fall into shadow
		wordRows(func(i int) wstate {
			d := math.Abs(float64(i - wi))
			a := math.Max(0.1, 1-d*0.3)
			if i == wi {
				sc := 1.12
				if animate {
					sc = 1 + 0.12*easeOutBack(math.Min(1, since(i)/0.25))
				}
				return wstate{show: true, color: acc, f: big, scale: float32(sc)}
			}
			return wstate{show: true, color: withAlpha(txt, a), f: big}
		})

	case stRain: // letters fall into place one by one while the word is sung
		rows := balanceRows(widths, float64(spaceW), maxW)
		for _, row := range rows {
			x := float32(0)
			var rr []run
			for k, i := range row {
				rs := []rune(words[i])
				for j := range rs {
					var at float64 = 99
					if i < wi || (i == wi && !animate) {
						at = 99
					} else if i == wi {
						at = since(i) - wordDur(L, i)*float64(j)/float64(len(rs))
					} else {
						continue // not sung yet
					}
					pre, _ := measure(big, string(rs[:j]))
					t := math.Min(1, math.Max(0, at/0.32))
					if at < 0 {
						continue
					}
					col := txt
					if i == wi {
						col = mixColor(acc, txt, easeOut(t))
					}
					rr = append(rr, run{text: string(rs[j]), x: x + pre, y: y, f: big, color: col, alpha: float32(math.Max(0.02, t)),
						dy: -float32((1 - easeOutBounce(t)) * F * 0.8)})
				}
				x += float32(widths[i])
				if k < len(row)-1 {
					x += spaceW
				}
			}
			addCentered(rr, x)
			y += lineH
		}

	case stSwing: // each word swings down into place on a hinge
		wordRows(func(i int) wstate {
			if i > wi {
				return wstate{show: false, f: big}
			}
			t := 1.0
			if animate {
				t = math.Min(1, since(i)/0.5)
			}
			col := txt
			if i == wi {
				col = acc
			}
			return wstate{show: true, color: col, f: big, alpha: float32(math.Max(0.02, math.Min(1, t*2.5))), rot: -float32(70 * (1 - easeOutBack(t))), rotC: true}
		})

	case stSplit: // the line comes together from both sides
		e := 1.0
		if animate {
			e = easeOut(math.Min(1, now.Sub(lv.lineStart).Seconds()/0.55))
		}
		half := len(words) / 2
		wordRows(func(i int) wstate {
			dir := float32(-1)
			if i >= half {
				dir = 1
			}
			col := txt
			if i == wi {
				col = acc
			}
			return wstate{show: true, color: col, f: big, alpha: float32(math.Max(0.02, e)), dx: dir * float32((1-e)*F*2.2)}
		})

	case stFocus: // the line starts big and blurry and pulls into focus
		e := 1.0
		if animate {
			e = easeOut(math.Min(1, now.Sub(lv.lineStart).Seconds()/0.6))
		}
		wordRows(func(i int) wstate {
			col := txt
			if i == wi {
				col = acc
			}
			return wstate{show: true, color: col, f: big, alpha: float32(math.Max(0.05, e)), scale: float32(1 + 0.3*(1-e)), haze: float32(1 - e)}
		})

	case stConstellation: // a real constellation per line: its stars carry the words, its lines grow as they're sung
		ci := constellationFor(max(idx, 0), len(words))
		sky := constellations[ci]
		order := skyOrder(sky)
		chunks := skyChunks(len(words), len(sky.Stars))
		W, H := maxW, F*4.8
		m := F * 0.8
		pos := make([][2]float32, len(sky.Stars))
		for i, st := range sky.Stars {
			pos[i] = [2]float32{float32(m + st[0]*(W-2*m) - W/2), float32(m*0.6 + st[1]*(H-2*m))}
		}
		// which star is being sung, and which are lit
		cur := -1
		for k, c := range chunks {
			if wi >= c[0] && wi < c[1] {
				cur = k
			}
		}
		done := wi >= len(words)-1 && wi >= 0 && since(wi) > 0.6 // the last word has rung: the whole sky lights
		lit := make([]bool, len(sky.Stars))
		rank := make([]int, len(sky.Stars)) // place in the lighting order
		for k, st := range order {
			rank[st] = k
			lit[st] = k <= cur || done
		}
		pop := 1.0
		if animate && cur >= 0 && wi >= 0 {
			pop = math.Min(1, since(chunks[cur][0])/0.4)
		}
		fade := 1.0
		if done {
			fade = math.Min(1, (since(wi)-0.6)/0.8) // the unworded stars and lines join in
		}
		lw := float32(math.Max(1, F*0.02))
		lineCol := withAlpha(txt, 0.55)
		for _, e := range sky.Edges { // lines first, stars on top
			a, b := e[0], e[1]
			if !lit[a] || !lit[b] {
				continue
			}
			if rank[a] > rank[b] {
				a, b = b, a // draw from the earlier star toward the later one
			}
			x0, y0, x1, y1 := pos[a][0], pos[a][1], pos[b][0], pos[b][1]
			al := float32(1)
			if rank[b] > cur && done { // a line of the finished shape
				al = float32(fade)
			} else if rank[b] == cur && pop < 1 { // the newest line draws itself toward the new star
				e := float32(easeOut(pop))
				x1, y1 = x0+(x1-x0)*e, y0+(y1-y0)*e
			}
			runs = append(runs, run{kind: rkLine, x: x0, y: y0, x2: x1, y2: y1, size: lw, color: lineCol, alpha: al})
		}
		smallF := font(fontName, F*0.36, cfg.Bold)
		curF := font(fontName, F*0.72, true)
		labelOf := func(st int) string {
			k := rank[st]
			if k >= len(chunks) {
				return ""
			}
			return strings.Join(words[chunks[k][0]:chunks[k][1]], " ")
		}
		// a label goes above its star when an earlier star sits just below it
		above := func(st int) bool {
			for o := range sky.Stars {
				if o != st && rank[o] < rank[st] {
					dx, dy := sky.Stars[o][0]-sky.Stars[st][0], sky.Stars[o][1]-sky.Stars[st][1]
					if dy > 0 && dy < 0.22 && math.Abs(dx) < 0.16 {
						return true
					}
				}
			}
			return false
		}
		for st := range sky.Stars {
			x, yy := pos[st][0], pos[st][1]
			sh := shapeFor(cfg.StarShape, st)
			label := labelOf(st)
			if !lit[st] { // not reached yet: a faint point, the sky waiting to be drawn
				runs = append(runs, run{kind: rkStar, x: x, y: yy, size: float32(F * 0.06), shape: sh, color: txt, alpha: 0.22})
				continue
			}
			ly := yy + float32(F*0.16)
			if above(st) {
				ly = yy - float32(F*0.16) - float32(F*0.42)
			}
			if rank[st] == cur && !done { // the words being sung: a big star with a ring, bold under it
				sc := float32(0.4 + 0.6*easeOutBack(pop))
				runs = append(runs, run{kind: rkRing, x: x, y: yy, size: float32(F * 0.3 * (0.6 + 0.4*easeOut(pop))), color: acc, alpha: float32(math.Min(1, pop*2) + 0.01)},
					run{kind: rkStar, x: x, y: yy, size: float32(F * 0.24), scale: sc, shape: sh, color: txt, alpha: 1})
				tw, _ := measure(curF, label)
				if above(st) {
					ly = yy - float32(F*0.3) - float32(F*0.8)
				} else {
					ly = yy + float32(F*0.28)
				}
				runs = append(runs, run{text: label, x: x - tw/2, y: ly, f: curF, color: txt, alpha: float32(math.Max(0.05, pop)), dy: float32((1 - easeOut(pop)) * F * 0.2)})
				continue
			}
			al := 0.9
			if label == "" {
				al = 0.7 * fade
			}
			runs = append(runs, run{kind: rkStar, x: x, y: yy, size: float32(F * 0.1), shape: sh, color: txt, alpha: float32(al)})
			if label != "" {
				tw, _ := measure(smallF, label)
				runs = append(runs, run{text: label, x: x - tw/2, y: ly, f: smallF, color: withAlpha(txt, 0.85), alpha: 1})
			}
		}
		if done { // the constellation's name, small, once it is complete
			nf := font(fontName, F*0.24, false)
			name := strings.ToUpper(sky.Name)
			tw, _ := measure(nf, name)
			runs = append(runs, run{text: name, x: float32(W/2-m*0.5) - tw, y: float32(H - m*0.55), f: nf, color: withAlpha(acc, 0.7), alpha: float32(fade)})
		}
		boxW = float32(W)
		y = float32(H)

	case stBouncing: // the sing-along ball hops from word to word; the word it lands on springs up
		y += float32(F * 0.9) // room for the ball's arc
		base := len(runs)
		wordRows(func(i int) wstate {
			st := wstate{show: true, f: big, color: withAlpha(txt, 0.45)}
			if i < wi {
				st.color = txt
			}
			if i == wi {
				st.color = acc
				if animate {
					if t := since(i) / 0.34; t < 1 {
						st.dy = -float32(math.Sin(t*math.Pi) * F * 0.2)
						st.scale = float32(1 + 0.1*math.Sin(t*math.Pi))
					}
				}
			}
			return st
		})
		if n := len(words); n > 0 && len(runs)-base == n {
			top := func(i int) (float64, float64) {
				r := runs[base+i]
				return float64(r.x) + widths[i]/2, float64(r.y) - F*0.12
			}
			ball := float32(F * 0.12)
			var bx, by float64
			secs := float64(now.UnixMilli()%100000) / 1000
			switch {
			case wi < 0: // waiting for the first word: bounce in place above it
				bx, by = top(0)
				if animate {
					by -= math.Abs(math.Sin(secs*4.5)) * F * 0.4
				}
			case wi >= n-1 || !animate:
				bx, by = top(minI(wi, n-1))
				if animate {
					by -= math.Abs(math.Sin(secs*5)) * F * 0.12 * math.Max(0, 1-since(wi)/0.7)
				}
			default:
				x0, y0 := top(wi)
				x1, y1 := top(wi + 1)
				t := (p - L.Words[wi].T) / math.Max(0.12, L.Words[wi+1].T-L.Words[wi].T)
				h := F*0.5 + math.Abs(x1-x0)*0.12
				if math.Abs(y1-y0) > 1 {
					h = F * 0.9 // over to the next row
				}
				bx, by = ballHop(x0, y0, x1, y1, t, h)
			}
			runs = append(runs, run{kind: rkBall, x: float32(bx), y: float32(by) - ball, size: ball, color: acc, alpha: 1})
		}

	case stInk: // hollow letters that fill with ink as they are sung
		wordRows(func(i int) wstate {
			switch {
			case i < wi:
				return wstate{show: true, color: txt, f: big}
			case i == wi:
				f := 1.0
				if animate {
					f = math.Min(1, since(i)/wordDur(L, i))
				}
				if f >= 1 {
					return wstate{show: true, color: txt, f: big}
				}
				return wstate{show: true, color: txt, f: big, fill: float32(math.Max(0.001, f)), hollow: true}
			}
			return wstate{show: true, color: txt, f: big, fill: 0.0001, hollow: true}
		})
	}
	if boxW < float32(F)*3 {
		boxW = float32(F) * 3
	}
	return runs, boxW, y
}

func mixColor(a, b uint32, t float64) uint32 {
	m := func(sh uint) uint32 {
		x, y := float64(a>>sh&255), float64(b>>sh&255)
		return uint32(x+(y-x)*t) & 255
	}
	return a&0xFF000000 | m(16)<<16 | m(8)<<8 | m(0)
}

func workWidth() int32 {
	wa := workArea()
	return wa.Right - wa.Left
}

// drawRun draws one piece of text with the chosen effect.
func drawRun(g uintptr, r run, ox, oy float32, F float64, effect int) {
	if r.alpha <= 0.01 {
		return
	}
	if r.kind != rkText {
		drawShape(g, r, ox, oy)
		return
	}
	col := withAlpha(r.color, float64(r.alpha))
	if r.caret {
		fillRect(g, col, ox+r.x+r.dx, oy+r.y, r.cw, r.ch)
		return
	}
	path := newPath()
	defer freePath(path)
	x, y := ox+r.x+r.dx, oy+r.y+r.dy
	var tw, th float32
	needSize := r.rotC || (r.scale != 0 && r.scale != 1) || r.bg != 0 || r.fill > 0
	if needSize {
		tw, th = measure(r.f, r.text)
	}
	switch {
	case r.rotC:
		gTranslate(g, x+tw/2, y+th/2)
		gRotate(g, r.rot)
		if r.scale != 0 && r.scale != 1 {
			gScale(g, r.scale, r.scale)
		}
		x, y = -tw/2, -th/2
		addTextPath(path, r.f, r.text, x, y)
	case r.rot != 0:
		gTranslate(g, x, y)
		gRotate(g, r.rot)
		x, y = 0, 0
		addTextPath(path, r.f, r.text, 0, 0)
	case r.scale != 0 && r.scale != 1:
		gTranslate(g, x+tw/2, y+th/2)
		gScale(g, r.scale, r.scale)
		x, y = -tw/2, -th/2
		addTextPath(path, r.f, r.text, x, y)
	default:
		addTextPath(path, r.f, r.text, x, y)
	}
	defer gBase(g)
	size := r.f.size
	haze := lyrHaze
	if r.haze > haze {
		haze = r.haze
	}
	if haze > 0 && softBegin(g) { // (the Mac: one blurred fill on the soft layer)
		fillPathColor(g, path, withAlpha(col, 0.5*float64(haze)))
		softEnd(g)
		softBlur(g, size*0.16*haze)
		col = withAlpha(col, 1-0.8*float64(haze))
	} else if haze > 0 { // the words sit in a haze that clears as they arrive (Dissolve, Scatter, Focus)
		rr := size * 0.16 * haze
		for i := 0; i < 6; i++ {
			ang := float64(i) * math.Pi / 3
			dx, dy := rr*float32(math.Cos(ang)), rr*float32(math.Sin(ang))
			gTranslate(g, dx, dy)
			fillPathColor(g, path, withAlpha(col, 0.13*float64(haze)))
			gTranslate(g, -dx, -dy)
		}
		col = withAlpha(col, 1-0.8*float64(haze))
	}
	a := float64(col>>24) / 255
	if r.bg != 0 { // highlighter
		pad := size * 0.12
		box := roundRectPath(x-pad, y+size*0.06, tw+2*pad, th*0.92, size*0.18)
		fillPathColor(g, box, withAlpha(r.bg, a))
		freePath(box)
	}
	if r.chroma != 0 {
		gTranslate(g, -r.chroma, 0)
		fillPathColor(g, path, withAlpha(0xFFFF2A55, a*0.75))
		gTranslate(g, 2*r.chroma, 0)
		fillPathColor(g, path, withAlpha(0xFF22E5FF, a*0.75))
		gTranslate(g, -r.chroma, 0)
	}
	if r.fill > 0 && r.fill < 1 { // dim word, bright part sweeps left → right
		if r.hollow {
			strokePath(g, path, withAlpha(col, 0.85), float32(math.Max(1.2, float64(size)*0.035)))
		} else {
			fillPathColor(g, path, withAlpha(col, 0.35))
		}
		clipRect(g, x-size, y-size, tw*r.fill+size, th+2*size)
		drawEffect(g, path, col, size, effect)
		clipReset(g)
		return
	}
	drawEffect(g, path, col, size, effect)
}

// lyrics motion state for the frame being drawn (UI thread only)
var (
	lyrMotion, lyrHaze float32
	lyrBase            struct {
		on             bool
		cx, cy, sx, sy float32
	}
)

// gBase resets the transform to the lyrics' whole-box transform (Pulse / Stretch).
func gBase(g uintptr) {
	gReset(g)
	if lyrBase.on {
		gTranslate(g, lyrBase.cx, lyrBase.cy)
		gScale(g, lyrBase.sx, lyrBase.sy)
		gTranslate(g, -lyrBase.cx, -lyrBase.cy)
	}
}

func drawEffect(g, path uintptr, col uint32, size float32, effect int) {
	amt := float32(cfg.GlowAmount)
	if cfg.Motion == moGlow {
		amt = amt*(1+lyrMotion*1.4) + lyrMotion
	}
	a := float64(col>>24) / 255
	switch effect {
	case fxNone:
		fillPathColor(g, path, col)
	case fxSoftGlow:
		drawGlowPath(g, path, col, col, size*0.22*amt, true)
	case fxStrongGlow:
		if softBegin(g) { // the Mac: one blurred fill on the soft layer instead of four outlines
			fillPathColor(g, path, withAlpha(col|0xFF000000, math.Min(1, a*0.8*math.Min(1.5, float64(amt)))))
			softEnd(g)
			softBlur(g, size*0.25*amt+1)
		} else {
			for i, wm := range []float32{1.0, 0.65, 0.35, 0.15} {
				strokePath(g, path, withAlpha(col|0xFF000000, a*[]float64{0.07, 0.12, 0.2, 0.35}[i]*math.Min(1.5, float64(amt))), size*0.5*amt*wm+1)
			}
		}
		fillPathColor(g, path, col)
	case fxShadow:
		dx, dy := size*0.05*amt+1, size*0.07*amt+1
		gTranslate(g, dx, dy)
		fillPathColor(g, path, withAlpha(0xFF000000, a*0.7))
		gTranslate(g, -dx, -dy)
		fillPathColor(g, path, col)
	case fxOutline:
		strokePath(g, path, withAlpha(0xFF000000, a*0.9), size*0.09*amt+1.5)
		fillPathColor(g, path, col)
	case fxNeon:
		acc := accent()
		if softBegin(g) {
			fillPathColor(g, path, withAlpha(acc, a*0.8))
			softEnd(g)
			softBlur(g, size*0.21*amt+1)
			strokePath(g, path, withAlpha(acc, a*0.45), size*0.42*amt*0.3+1) // the tube's crisp rim
		} else {
			for i, wm := range []float32{1.0, 0.6, 0.3} {
				strokePath(g, path, withAlpha(acc, a*[]float64{0.12, 0.22, 0.45}[i]), size*0.42*amt*wm+1)
			}
		}
		fillPathColor(g, path, withAlpha(mixColor(0xFFFFFFFF, acc, 0.25), a))
	case fxGradient:
		b := pathBounds(path)
		drawGlowPath(g, path, 0, col, size*0.12*amt, true)
		fillPathGradient(g, path, col, withAlpha(accent(), a), b.Y, b.Y+b.H)
	case fxChromatic: // RGB split
		d := size*0.035*amt + 1
		gTranslate(g, -d, 0)
		fillPathColor(g, path, withAlpha(0xFFFF2A55, a*0.8))
		gTranslate(g, 2*d, 0)
		fillPathColor(g, path, withAlpha(0xFF22E5FF, a*0.8))
		gTranslate(g, -d, 0)
		fillPathColor(g, path, col)
	case fxLongShadow: // extruded 3D shadow
		n := int(size*0.12*amt) + 2
		shadow := withAlpha(mixColor(col|0xFF000000, 0xFF000000, 0.82), a)
		for k := n; k >= 1; k-- {
			gTranslate(g, 1, 1)
			fillPathColor(g, path, shadow)
		}
		gTranslate(g, -float32(n), -float32(n))
		fillPathColor(g, path, col)
	case fxHollow: // outline only
		strokePath(g, path, col, size*0.045*amt+1.2)
	case fxSoftBlur: // soft, slightly out-of-focus edges
		r := size*0.035*amt + 1
		if softBegin(g) {
			fillPathColor(g, path, withAlpha(col, 0.6))
			softEnd(g)
			softBlur(g, r*1.3)
			fillPathColor(g, path, withAlpha(col, 0.85))
			return
		}
		for i := 0; i < 8; i++ {
			ang := float64(i) * math.Pi / 4
			dx, dy := r*float32(math.Cos(ang)), r*float32(math.Sin(ang))
			gTranslate(g, dx, dy)
			fillPathColor(g, path, withAlpha(col, 0.14))
			gTranslate(g, -dx, -dy)
		}
		fillPathColor(g, path, withAlpha(col, 0.85))
	case fxGlass: // frosted: light gradient, thin bright rim, soft halo
		b := pathBounds(path)
		drawGlowPath(g, path, 0, col, size*0.1*amt, false)
		fillPathGradient(g, path, withAlpha(0xFFFFFFFF, a*0.95), withAlpha(col, a*0.45), b.Y, b.Y+b.H)
		strokePath(g, path, withAlpha(0xFFFFFFFF, a*0.55), 1)
	}
}

func (lv *LyricsView) render(runs []run, bw, bh float32, F float64, style int) {
	editMode := framed("lyrics")
	pad := float32(F * 0.6)
	if cfg.Motion == moPulse || cfg.Motion == moStretch { // room to grow on the beat
		pad += bw * 0.1 * float32(cfg.MotionAmount)
	}
	W := int(bw + 2*pad)
	H := int(bh + 2*pad)
	if editMode {
		H += 34
	}
	lv.w.resize(W, H)
	if lv.w.canvas == nil {
		return
	}
	g := lv.w.g()
	gClear(g, 0)
	ox := pad + bw/2
	oy := pad
	if editMode {
		oy += 34
	}
	effect := cfg.Effect
	if style == stNeon {
		effect = fxNeon
	}
	if cfg.Motion == moGlow && effect == fxNone {
		effect = fxSoftGlow
	}
	lyrBase.on = false
	if lyrMotion > 0 && (cfg.Motion == moPulse || cfg.Motion == moStretch) {
		lyrBase.on = true
		lyrBase.cx, lyrBase.cy = ox, oy+bh/2
		if cfg.Motion == moPulse {
			lyrBase.sx = 1 + 0.09*lyrMotion
			lyrBase.sy = lyrBase.sx
		} else {
			lyrBase.sx, lyrBase.sy = 1+0.16*lyrMotion, 1-0.05*lyrMotion
		}
		gBase(g)
	}
	for _, r := range runs {
		fx := effect
		if r.f != nil && r.f.size < float32(F)*0.6 && fx == fxStrongGlow {
			fx = fxSoftGlow // keep the small lines readable
		}
		if r.bg != 0 && (fx == fxSoftGlow || fx == fxStrongGlow || fx == fxNeon || fx == fxSoftBlur || fx == fxGlass || fx == fxGradient) {
			fx = fxNone // dark text on the marker stays crisp
		}
		drawRun(g, r, ox, oy, F, fx)
	}
	lyrBase.on = false
	gReset(g)
	if editMode {
		label := "Lyrics (random spot — drag to pin)"
		if cfg.LyricsPlace == 1 {
			label = "Lyrics"
		}
		editFrame(g, float32(W), float32(H), "lyrics", label)
	}
}

// place moves the lyrics window: a fresh random spot per line, or the pinned spot.
func (lv *LyricsView) place(newLine bool, ui float64) {
	W, H := lv.w.w, lv.w.h
	wa := workArea()
	p := cfg.widget("lyrics")
	if cfg.LyricsPlace == 1 || editMode {
		cx, cy := int(wa.Left)+int(float64(wa.Right-wa.Left)*0.24), int(wa.Top)+int(float64(wa.Bottom-wa.Top)*0.22)
		if p.Set {
			cx, cy = p.X, p.Y
		}
		lv.w.moveTo(cx-W/2, cy-H/2)
		return
	}
	if !newLine {
		// same line re-rendered (typing / animation): keep the centre still
		r := lv.w.rect()
		if r.Right > r.Left {
			ocx, ocy := int(r.Left+r.Right)/2, int(r.Top+r.Bottom)/2
			lv.w.moveTo(ocx-W/2, ocy-H/2)
		}
		return
	}
	mx, my := int(float64(wa.Right-wa.Left)*0.04), int(float64(wa.Bottom-wa.Top)*0.06)
	area := rectI{int(wa.Left) + mx, int(wa.Top) + my, int(wa.Right) - mx, int(wa.Bottom) - my}
	var avoid []rectI
	for _, w := range []*Widget{vinyl.w, halo.w, widgetByName("clock"), widgetByName("weather")} {
		if w != nil && w.visible {
			r := w.rect()
			avoid = append(avoid, rectI{int(r.Left) - 20, int(r.Top) - 20, int(r.Right) + 20, int(r.Bottom) + 20})
		}
	}
	for _, r := range otherDesktopWidgets() { // the Mac's own desktop widgets (calendar, photos …)
		avoid = append(avoid, rectI{int(r.Left) - 12, int(r.Top) - 12, int(r.Right) + 12, int(r.Bottom) + 12})
	}
	x, y := randomSpot(lv.rng, area, W, H+int(lv.fontSize(ui)*1.2), avoid, lv.last)
	lv.last = [2]int{x, y}
	lv.w.moveTo(x, y)
}
