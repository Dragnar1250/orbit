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
		// the small copy for the song list is written a little later, not in the middle of
		// the record change (encoding + disk on the UI thread made it stutter)
		key, ver := t.Key, t.CoverVer
		time.AfterFunc(2800*time.Millisecond, func() {
			runOnUI(func() {
				if cover.key == key && cover.ver == ver && cover.img != 0 {
					saveHistoryCover(key, cover.img)
				}
			})
		})
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
	gImageHQ(g, img, x+(s-dw)/2, y+(s-dh)/2, dw, dh)
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
	label = T(label)
	if widgetLocked(name) {
		label += "  ·  " + T("locked")
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
	w         *Widget
	base      *Bitmap // glow, rings, (grooves), track list
	label     *Bitmap // rotating part (cover)
	sig       string
	needFull  bool // next frame must push the whole window (layers rebuilt / resized)
	angle     float64
	last      time.Time
	version   int
	R         float64
	sx, sy    float64 // +1/-1: mirror for the right / top corners
	cx, cy    float64 // disc centre in window pixels
	progress  float64
	drawnAt   time.Time
	armT      float64 // 0 = needle up on its rest, 1 = on the record
	ambT      float64 // ambient light clock
	playing   bool
	bands     [nBands]float32
	lastDirty RECT     // this frame's redrawn part (for drawing in Go)
	np        nowLabel // the "NOW PLAYING" caption in the song wheel, which comes alive each frame
	bass      float32

	// 1.19
	holo      *Bitmap // CD: the rainbow, drawn over the turning disc (it stays with the light)
	oldLabel  *Bitmap // sleeve swap: the record that is being put away
	oldSleeve *Bitmap // sleeve swap: the cover of the song that ended
	swapT     float64 // seconds into the record change, -1 = none
	swapWait  float64 // seconds spent waiting for the next song's cover
	swapStyle int     // the style this change started with
	newAt     float64 // lift & drop: when the new record started coming down (-1 = not yet)
	recSig    string  // what the record bitmap was made for
	qAsk      time.Time
	qSig      string
	lastKey   string
	noSwap    bool // full screen's record: no sleeve swap, no scratching
	tipX      float32
	tipY      float32
	sparks    []spark
	scr       scratchState
}

type spark struct{ x, y, t, life, size float32 }

type scratchState struct {
	on       bool
	ang      float64 // last mouse angle around the centre, degrees
	pos, dur float64 // where the song is while the record is held
	turned   float64 // degrees turned since the press
	lastSeek time.Time
	sentPos  float64
}

// sleeve swap timeline (seconds)
const (
	swLift     = 0.35 // needle lifts
	swCover    = 0.80 // the sleeve slides over the old record
	swAway     = 1.20 // … and out, with the record in it
	swNewIn    = 1.65 // the next record's sleeve slides in
	swReveal   = 2.10 // the sleeve slides off: the new record stays
	swDone     = 2.50 // needle back on
	swRecordIn = swCover
	swRecordUp = swNewIn
)

// lift & drop timeline (seconds). The new record starts coming down while the old one is
// still fading out (as soon as its cover is there), so there is no empty pause.
const (
	lfOldFrom = 0.10 // the old record lifts off (the needle is already rising) …
	lfOldTo   = 1.00 // … drifts toward the corner and is gone
	lfNew     = 0.55 // earliest start of the new record
	lfNewDur  = 0.95 // it comes down, fades in and settles
	lfNeedle  = 0.75 // after it started: the needle swings back on
	lfTail    = 1.35 // after it started: the change is over
)

func (v *VinylView) swapHoldAt() float64 { return swAway } // (sleeve only)

func (v *VinylView) swapEnd() float64 {
	if v.swapStyle == 1 {
		return swDone
	}
	if v.newAt < 0 {
		return math.Inf(1)
	}
	return v.newAt + lfTail
}

// coverReady: the cover of the song now playing is loaded.
func (v *VinylView) coverReady() bool { return cover.key == v.lastKey && cover.img != 0 }

func newVinylView() *VinylView {
	return &VinylView{w: newWidget("vinyl", "Turntable"), last: time.Now(), version: -1, sx: 1, sy: 1, armT: 1, swapT: -1}
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
			v.endSwap()
			v.holo = nil // (shared: holoFor keeps it)
			v.recSig = ""
		}
		return
	}
	if snap.Version != v.version {
		v.version = snap.Version
		if snap.Track != nil && !v.w.offscreen { // (full screen keeps the list itself — and holds the settings lock here)
			pushHistory(snap.Track)
		}
		needRedraw = true
		key := ""
		if snap.Track != nil {
			key = snap.Track.Key
		}
		if key != v.lastKey {
			if v.lastKey != "" && key != "" {
				v.startSwap() // (before the cover changes: the old one goes on the old sleeve)
			}
			v.lastKey = key
		}
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
	if v.w.offscreen { // full screen: the record in the middle of its picture, with room all round for its glow
		m := int(R * 1.5)
		W, H = 2*m, 2*m
	}
	sizeChanged := v.w.w != W || v.w.h != H
	v.w.resize(W, H)
	if sizeChanged || layoutDirty || !v.w.visible {
		v.w.moveTo(x, y)
		v.needFull = true
	}
	v.R = R
	v.cx, v.cy = float64(v.tx(R*1.08)), float64(v.ty(R*1.12))
	if v.w.offscreen {
		v.cx, v.cy = float64(W)/2, float64(H)/2
	}

	key, title := "", ""
	if snap.Track != nil {
		key, title = snap.Track.Key, snap.Track.Title
	}
	sig := fmt.Sprintf("%s|%d|%d|%.1f|%v|%v|%d|%x|%s|%d|%d|%d|%v|%s|%s|%v", key, cover.ver, cfg.Theme, R, cfg.ShowList, editMode,
		len(cfg.History), albumAccent, title, cfg.DiscStyle, corner, cfg.ListCount, cfg.ShowArm, cfg.TextColor, cfg.AccentColor, framed("vinyl"))
	sig += fmt.Sprintf("|%d|%s|%.2f|%v|%d|%s|%d|%v", cfg.DiscColorMode, cfg.DiscColor, cfg.DiscGlow, widgetLocked("vinyl"), cfg.VinylLayout, snapArtist(snap), hoverBtn, cfg.Worn)
	upNext := v.upNext(snap)
	qs := ""
	for _, u := range upNext {
		qs += u.Key + "\x00" + u.CoverFile + "\x00"
	}
	sig += "|" + qs + fmt.Sprint(cfg.VinylNext)
	if sig != v.sig || styleDirty {
		v.sig = sig
		v.buildBase(upNext)
		needRedraw = true
	}
	// the record itself: made once the new song's cover is there (during a change it waits for
	// it, so the new record never shows a wrong cover — and it is made only once)
	recSig := fmt.Sprintf("%s|%s|%d|%d|%.1f|%x|%v|%d|%s|%x|%d|%s", key, cover.key, cover.ver, cfg.DiscStyle, R, discColor(), cfg.Worn,
		cfg.Theme, cfg.AccentColor, albumAccent, cfg.DiscColorMode, cfg.DiscColor)
	waiting := v.swapT >= 0 && !v.coverReady() && v.newAt < 0 && v.swapWait < 2.2 && !(v.swapStyle == 0 && v.swapT >= lfNew+2.2)
	if (recSig != v.recSig || coverChanged || styleDirty || v.label == nil) && !waiting {
		v.recSig = recSig
		v.buildRecord()
		needRedraw = true
	}
	v.askQueue(snap)
	if v.scr.on { // the hand on the record decides where the song is
		if v.scr.dur > 0 {
			v.progress = math.Max(0, math.Min(1, v.scr.pos/v.scr.dur))
		}
	} else if snap.Track != nil && snap.Track.Duration > 0 {
		pr := math.Max(0, math.Min(1, snap.Pos/snap.Track.Duration))
		if math.Abs(pr-v.progress) > 0.002 {
			v.progress = pr
			needRedraw = true
		}
	}
	spinning := snap.Playing && snap.Track != nil && cfg.FPS > 0 && cfg.SpinRPM > 0 && !v.scr.on
	if spinning {
		v.angle = math.Mod(v.angle+dt*cfg.SpinRPM*6*v.sx*v.sy*v.spinFactor(), 360)
	}
	if v.swapT >= 0 {
		step := math.Min(dt, 1.0/24) // a slow frame doesn't skip half the motion
		if v.swapStyle == 1 {
			hold := v.swapHoldAt()
			if v.swapT+step >= hold && v.swapT <= hold && !v.coverReady() && v.swapWait < 2.2 {
				v.swapT = hold // the empty sleeve waits for the next cover
				v.swapWait += step
			} else {
				v.swapT += step
			}
		} else {
			v.swapT += step
			if v.newAt < 0 && v.swapT >= lfNew && v.label != nil && (v.coverReady() || v.swapT >= lfNew+2.2) {
				v.newAt = v.swapT // the new record is made: bring it down
			}
		}
		needRedraw = true
		if v.swapT >= v.swapEnd() || cfg.FPS == 0 {
			v.endSwap()
			v.needFull = true
		}
	}
	v.updateSparks(dt, snap.Playing && snap.Track != nil)
	// needle: lowers onto the record when the music starts, lifts off when it stops
	target := 1.0
	if cfg.ArmLift && !(snap.Playing && snap.Track != nil) {
		target = 0
	}
	if v.swapT >= 0 && ((v.swapStyle == 1 && v.swapT < swReveal) || (v.swapStyle == 0 && (v.newAt < 0 || v.swapT < v.newAt+lfNeedle))) {
		target = 0 // off the record while it is changed
	}
	if cfg.FPS == 0 {
		v.armT = target
	} else if v.armT != target {
		step := math.Min(1, dt) * 1.6
		if v.swapT >= 0 {
			step *= 2.6
		}
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
	if len(v.sparks) > 0 {
		needRedraw = true
	}
	if !spinning && !needRedraw && v.w.visible {
		return // nothing moves → nothing to draw (no CPU while paused)
	}
	v.drawnAt = now
	v.draw()
}

/* ---------- 1.19: sleeve swap ---------- */

// startSwap: a new song — keep the old record and cover for the sleeve animation.
func (v *VinylView) startSwap() {
	if v.swapT >= 0 && v.label == nil && v.swapStyle == 0 {
		// skipped again before the next record was made: keep the old one leaving, wait for
		// the newest song's cover instead
		v.newAt, v.recSig, v.swapWait = -1, "", 0
		if v.swapT > lfNew {
			v.swapT = lfNew
		}
		return
	}
	if !cfg.SwapAnim || cfg.FPS == 0 || v.noSwap || v.label == nil || !v.w.visible || v.scr.on {
		return
	}
	v.endSwap()
	v.oldLabel, v.label = v.label, nil // buildLayers makes the new one
	v.swapStyle = cfg.SwapStyle
	if v.swapStyle == 1 {
		v.oldSleeve = renderSleeve(cover.img, float32(v.R))
	}
	v.sig, v.recSig = "", ""
	v.swapT, v.swapWait, v.newAt = 0, 0, -1
}

// spinFactor: the record slows as it is lifted and the new one speeds up once it is on.
func (v *VinylView) spinFactor() float64 {
	if v.swapT < 0 || v.swapStyle != 0 {
		return 1
	}
	if v.newAt < 0 || v.swapT < v.newAt {
		return 1 - 0.65*smooth(v.swapT/0.5)
	}
	return 0.25 + 0.75*smooth((v.swapT-v.newAt)/lfTail)
}

// recPose: where and how the record is drawn this frame.
type recPose struct {
	lbl                      *Bitmap
	x, y, scale, alpha, lift float64
	show, rest               bool // rest: on the platter, turning normally (effects on)
}

func easeOutCubic(t float64) float64 { t = math.Max(0, math.Min(1, t)); return 1 - math.Pow(1-t, 3) }
func easeInCubic(t float64) float64  { t = math.Max(0, math.Min(1, t)); return t * t * t }

// settle: 0 → 1 with a small, soft dip below 1 before it rests (a record landing).
func settle(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	const c1 = 0.9
	return 1 + (c1+1)*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}

// recordPoses: the record(s) to draw this frame, back to front. During a lift & drop change the
// old record and the new one can both be there for a moment (one fading out, one in).
func (v *VinylView) recordPoses() []recPose {
	rest := recPose{lbl: v.label, x: v.cx, y: v.cy, scale: 1, alpha: 1, show: v.label != nil, rest: true}
	if v.swapT < 0 {
		return []recPose{rest}
	}
	t := v.swapT
	if v.swapStyle == 1 { // sleeve: the record stays, the sleeve moves over it
		_, oldOne, record := v.swapPose()
		p := rest
		p.rest = false
		if oldOne {
			p.lbl = v.oldLabel
		}
		p.show = record && p.lbl != nil
		return []recPose{p}
	}
	dx, dy := -1.08*v.sx, 1.18*v.sy // toward the screen corner the record sits in
	n := math.Hypot(dx, dy)
	dx, dy = dx/n, dy/n
	var out []recPose
	// the old record: lifts (bigger, its shadow falls away), drifts to the corner, fades
	if u := (t - lfOldFrom) / (lfOldTo - lfOldFrom); u < 1 && v.oldLabel != nil {
		u = math.Max(0, u)
		p := recPose{lbl: v.oldLabel, scale: 1, alpha: 1, show: true}
		p.lift = smooth(u * 2.2)
		p.scale = 1 + 0.05*p.lift
		d := easeInCubic(u) * v.R * 1.35
		p.x, p.y = v.cx+dx*d, v.cy+dy*d
		p.alpha = 1 - smooth((u-0.2)/0.8)
		if p.alpha > 0.004 {
			out = append(out, p)
		}
	}
	// the new record: comes down from just above the platter, fades in and settles
	if v.newAt >= 0 && v.label != nil {
		u := (t - v.newAt) / lfNewDur
		e := easeOutCubic(u)
		p := recPose{lbl: v.label, show: true}
		d := (1 - e) * v.R * 0.45
		p.x, p.y = v.cx+dx*d, v.cy+dy*d-(1-e)*v.R*0.1
		p.scale = 1 + 0.1*(1-settle(u))
		p.lift = 1 - e
		p.alpha = smooth(u / 0.4)
		p.rest = u >= 1
		out = append(out, p)
	}
	return out
}

func (v *VinylView) endSwap() {
	v.oldLabel.Free()
	v.oldSleeve.Free()
	v.oldLabel, v.oldSleeve = nil, nil
	v.swapT, v.newAt = -1, -1
}

// renderSleeve: the album jacket — the cover across it, a soft sheen and an edge.
func renderSleeve(img uintptr, R float32) *Bitmap {
	S := int(R * 2.14)
	b := newBitmap(S, S)
	if b == nil {
		return nil
	}
	g := b.g
	setInterp(g, interpHighQualityBicubic)
	s := float32(S)
	fillRect(g, 0xFF202024, 0, 0, s, s)
	if img != 0 {
		drawCoverSquare(g, img, 0, 0, s)
	} else {
		fillRectGrad(g, 0, 0, s, s, 0, 0, s, s, withAlpha(accent(), 0.8)|0xFF000000, 0xFF151518)
		f := font("Segoe UI Symbol", float64(s*0.3), false)
		tw, th := measure(f, "♪")
		drawText(g, f, "♪", s/2-tw/2, s/2-th/2, tw+4, 0xCCFFFFFF)
	}
	fillRectGrad(g, 0, 0, s, s, 0, 0, s, s, 0x30FFFFFF, 0x00FFFFFF) // card sheen
	fillRect(g, 0x18000000, 0, s*0.5, s, s*0.5)
	e := s - 0.6
	for _, l := range [][4]float32{{0.6, 0.6, e, 0.6}, {e, 0.6, e, e}, {e, e, 0.6, e}, {0.6, e, 0.6, 0.6}} {
		line(g, 0x50FFFFFF, 1.2, l[0], l[1], l[2], l[3])
	}
	return b
}

func smooth(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// swapPose: where the sleeve is (0 = right over the record, 1 = far out past the corner),
// which sleeve / record shows, and whether there is a record on the platter at all.
func (v *VinylView) swapPose() (sleeve float64, oldOne, record bool) {
	t := v.swapT
	switch {
	case t < swLift:
		return 1, true, true
	case t < swCover:
		return 1 - smooth((t-swLift)/(swCover-swLift)), true, true
	case t < swAway:
		return smooth((t - swCover) / (swAway - swCover)), true, false
	case t < swNewIn:
		return 1 - smooth((t-swAway)/(swNewIn-swAway)), false, false
	case t < swReveal:
		return smooth((t - swNewIn) / (swReveal - swNewIn)), false, true
	}
	return 1, false, true
}

// drawSleeve: the jacket slides in from (and out to) the screen corner the record sits in.
func (v *VinylView) drawSleeve(g uintptr, out float64, oldOne bool) {
	if out >= 0.999 {
		return
	}
	R := float32(v.R)
	dx, dy := -1.08*v.sx, 1.18*v.sy // toward the corner
	n := math.Hypot(dx, dy)
	far := v.R * 3.4
	x := float32(v.cx + dx/n*far*out)
	y := float32(v.cy + dy/n*far*out)
	S := R * 2.14
	gTranslate(g, x, y)
	gRotate(g, float32((out*14-3)*v.sx*v.sy))
	fillRect(g, 0x50000000, -S/2+R*0.04, -S/2+R*0.06, S, S) // shadow
	if oldOne && v.oldSleeve != nil {
		gImage(g, v.oldSleeve.bmp, -S/2, -S/2, S, S)
	} else if !oldOne {
		if b := renderSleeveCached(v); b != nil {
			gImage(g, b.bmp, -S/2, -S/2, S, S)
		}
	}
	gReset(g)
}

var newSleeve struct {
	b   *Bitmap
	key string
	ver int
	R   float32
}

// renderSleeveCached: the next song's sleeve, made once per cover (the cover can arrive late).
func renderSleeveCached(v *VinylView) *Bitmap {
	R := float32(v.R)
	if newSleeve.b == nil || newSleeve.key != cover.key || newSleeve.ver != cover.ver || newSleeve.R != R {
		newSleeve.b.Free()
		img := cover.img
		if cover.key != v.lastKey { // (never another song's cover)
			img = 0
		}
		newSleeve.b = renderSleeve(img, R)
		newSleeve.key, newSleeve.ver, newSleeve.R = cover.key, cover.ver, R
	}
	return newSleeve.b
}

/* ---------- 1.19: sparkles where the needle runs over dust ---------- */

func (v *VinylView) updateSparks(dt float64, playing bool) {
	live := v.sparks[:0]
	for _, s := range v.sparks {
		s.t += float32(dt)
		if s.t < s.life {
			live = append(live, s)
		}
	}
	v.sparks = live
	if !cfg.Worn || !cfg.ShowArm || !playing || cfg.FPS == 0 || v.armT < 1 || v.swapT >= 0 || v.scr.on || v.tipX == 0 {
		return
	}
	rate := 5 + 14*float64(v.bass) // sparkles a second
	for n := rand.Float64() * 1; n < rate*dt; n++ {
		if len(v.sparks) >= 24 {
			break
		}
		R := float32(v.R)
		a := rand.Float64() * 2 * math.Pi
		d := R * 0.035 * float32(rand.Float64())
		v.sparks = append(v.sparks, spark{
			x: v.tipX + d*float32(math.Cos(a)), y: v.tipY + d*float32(math.Sin(a)),
			life: 0.25 + float32(rand.Float64())*0.35, size: R * (0.012 + 0.022*float32(rand.Float64())),
		})
	}
}

func (v *VinylView) drawSparks(g uintptr) {
	for _, s := range v.sparks {
		k := s.t / s.life
		a := float64(1 - k)
		sz := s.size * (0.6 + 0.6*float32(math.Sin(float64(k)*math.Pi)))
		c := withAlpha(0xFFFFF6E0, a)
		line(g, c, 1, s.x-sz, s.y, s.x+sz, s.y)
		line(g, c, 1, s.x, s.y-sz, s.x, s.y+sz)
		fillCircle(g, withAlpha(0xFFFFFFFF, a*0.9), s.x, s.y, 1.1)
		if gl := sparkGlow(a * 0.35); gl != nil { // a ready-made glow: a new gradient brush per spark per frame was slow
			r := sz * 1.6
			gImage(g, gl.bmp, s.x-r, s.y-r, 2*r, 2*r)
		} else {
			softGlow(g, s.x, s.y, sz*1.6, withAlpha(0xFFFFE2A8, a*0.35))
		}
	}
}

var sparkGlows [9]*Bitmap

// sparkGlow: the sparkle's soft light at one of 9 strengths (made once each).
func sparkGlow(a float64) *Bitmap {
	i := int(math.Round(math.Max(0, math.Min(1, a/0.35)) * 8))
	if i == 0 {
		return nil
	}
	if sparkGlows[i] == nil {
		b := newBitmap(48, 48)
		if b == nil {
			return nil
		}
		gClear(b.g, 0)
		softGlow(b.g, 24, 24, 24, withAlpha(0xFFFFE2A8, 0.35*float64(i)/8))
		sparkGlows[i] = b
	}
	return sparkGlows[i]
}

/* ---------- 1.19: scratching ---------- */

// scratchHit: is the point (screen pixels) on the record?
func (v *VinylView) scratchHit(pt POINT) bool {
	if !cfg.Scratch || v.noSwap || v.w.canvas == nil || v.swapT >= 0 {
		return false
	}
	r := v.w.rect()
	dx, dy := float64(pt.X-r.Left)-v.cx, float64(pt.Y-r.Top)-v.cy
	return math.Hypot(dx, dy) < v.R*0.97
}

func (v *VinylView) mouseAngle(pt POINT) float64 {
	r := v.w.rect()
	return math.Atan2(float64(pt.Y-r.Top)-v.cy, float64(pt.X-r.Left)-v.cx) * 180 / math.Pi
}

func (v *VinylView) beginScratch(pt POINT) {
	snap := media.Snapshot()
	v.scr = scratchState{on: true, ang: v.mouseAngle(pt), pos: snap.Pos, sentPos: snap.Pos}
	if snap.Track != nil {
		v.scr.dur = snap.Track.Duration
	}
	v.w.capture(true)
	requestRedraw()
}

// scratchMove: the record follows the hand; one full turn = cfg.ScratchSec seconds of song.
func (v *VinylView) scratchMove(pt POINT) {
	if !v.scr.on {
		return
	}
	a := v.mouseAngle(pt)
	d := scratchDelta(v.scr.ang, a)
	v.scr.ang = a
	v.angle = math.Mod(v.angle+d, 360)
	v.scr.turned += math.Abs(d)
	v.scr.pos = scratchPos(v.scr.pos, d*v.sx*v.sy, cfg.ScratchSec, v.scr.dur)
	if time.Since(v.scr.lastSeek) > 220*time.Millisecond && math.Abs(v.scr.pos-v.scr.sentPos) > 0.25 {
		v.scr.lastSeek, v.scr.sentPos = time.Now(), v.scr.pos
		requestSeek(v.scr.pos - media.Offset())
	}
	needRedraw = true
	requestRedraw()
}

// endScratch: let go — the song carries on from where the record was left. A press that
// didn't turn the record is a plain click (click to play / pause).
func (v *VinylView) endScratch() {
	if !v.scr.on {
		return
	}
	s := v.scr
	v.scr.on = false
	releaseCapture()
	if s.turned < 4 {
		lastClick.up = true
		return
	}
	lastClick.id++ // a scratch is not a click: no play / pause after it
	if math.Abs(s.pos-s.sentPos) > 0.05 || s.lastSeek.IsZero() {
		if !requestSeek(s.pos - media.Offset()) {
			logf("scratch: this player can't be told to jump (Windows media controls)")
		}
	}
	logf("scratch: turned %.0f° → %s", s.turned, fmtTime(s.pos))
	requestRedraw()
}

// drawScratchTime: a small pill with the time while the record is held.
func (v *VinylView) drawScratchTime(g uintptr) {
	R := float32(v.R)
	txt := fmtTime(v.scr.pos)
	if v.scr.dur > 0 {
		txt += " / " + fmtTime(v.scr.dur)
	}
	f := font("Segoe UI", float64(R*0.085), true)
	tw, th := measure(f, txt)
	x, y := float32(v.cx)-tw/2, float32(v.cy)-R*0.36-th/2
	path := roundRectPath(x-R*0.05, y-R*0.012, tw+R*0.1, th+R*0.024, (th+R*0.024)/2)
	fillPathColor(g, path, 0xC0101014)
	freePath(path)
	drawText(g, f, txt, x, y, tw+4, 0xFFFFFFFF)
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
// (Since 1.19 every record turns as one image — wear, the sleeve swap and scratching
// need the whole record to move — this only says whether its pattern shows it.)
func rotatesWhole(style int) bool {
	return style == discPicture || style == discSplatter || style == discMarble || style == discTwoTone
}

// discGlowColor: the colour of the glow around the record and of the groove light.
func discGlowColor() uint32 {
	switch cfg.DiscStyle {
	case discPicture, discBlack:
		return accent()
	case discCD:
		return 0xFFBFD8FF
	case discGlowDark:
		return 0xFF7CFFB2
	case discGold:
		return 0xFFFFD27A
	}
	return discColor()
}

// buildRecord makes the turning record for the song playing (and the CD's rainbow).
func (v *VinylView) buildRecord() {
	key := ""
	if t := media.Snapshot().Track; t != nil {
		key = t.Key
	}
	img := cover.img
	if cover.key != key {
		img = 0
	}
	v.label.Free()
	v.label = renderRecord(cfg.DiscStyle, float32(v.R), key, img)
	v.holo = nil
	if cfg.DiscStyle == discCD {
		v.holo = holoFor(float32(v.R))
	}
	v.needFull = true
}

var holoCache = map[float32]*Bitmap{}

// holoFor: the CD rainbow depends only on the size — made once per size, not on every song.
// (The desktop record and full screen's have different sizes: both stay; old sizes left over
// from resizing are freed once no record uses them.)
func holoFor(R float32) *Bitmap {
	if b, ok := holoCache[R]; ok && b != nil {
		return b
	}
	if len(holoCache) >= 4 {
		for k, b := range holoCache {
			inUse := (vinyl != nil && vinyl.holo == b) || (fs != nil && fs.vin != nil && fs.vin.holo == b)
			if !inUse {
				b.Free()
				delete(holoCache, k)
			}
		}
	}
	b := renderHolo(R)
	holoCache[R] = b
	return b
}

// buildBase: everything that stands still — shadow, glow, rings, the song wheel or card.
func (v *VinylView) buildBase(upNext []HistItem) {
	R, cx, cy := float32(v.R), float32(v.cx), float32(v.cy)
	acc := accent()
	v.needFull = true
	v.np = nowLabel{}
	v.base.Free()
	v.base = newBitmap(v.w.w, v.w.h)
	if v.base == nil {
		return
	}
	g := v.base.g
	gClear(g, 0)
	setTextHint(g, 3) // grid-fitted: crisp small text on the wheel / card
	softGlow(g, cx, cy+R*0.04*float32(v.sy), R*1.2, 0xAA000000)
	if cfg.DiscGlow > 0 {
		softGlow(g, cx, cy, R*1.16, withAlpha(discGlowColor(), 0.2*cfg.DiscGlow))
	}
	strokeCircle(g, withAlpha(acc, 0.55), cx, cy, R*1.035, 1.4)
	strokeCircle(g, withAlpha(acc, 0.22), cx, cy, R*1.075, 1)
	if cfg.VinylLayout == 1 {
		v.drawCard(g)
	} else if cfg.ShowList {
		v.drawList(g, upNext)
	}
	if framed("vinyl") {
		editFrame(g, float32(v.w.w), float32(v.w.h), "vinyl", "Turntable")
	}
}

/* ---------- 1.19.1: what plays next ---------- */

// upNext: the songs after the playing one, from the Spotify queue (empty when not connected,
// not known yet, or the wheel is set to what played).
func (v *VinylView) upNext(snap MediaSnap) []HistItem {
	if cfg.VinylNext != 0 || !cfg.ShowList || cfg.VinylLayout == 1 || snap.Track == nil || cfg.SpotifyClientID == "" || cfg.SpotifyRefresh == "" {
		return nil
	}
	q, playing := spotify.snapshot()
	q = queueAfter(q, playing, snap.Track.Title, snap.Track.Key)
	var out []HistItem
	for _, it := range q {
		k := keyOf(it.Title, it.Artist)
		if k == snap.Track.Key && len(out) == 0 { // Spotify sometimes repeats the playing song first
			continue
		}
		out = append(out, HistItem{Key: k, Title: it.Title, Artist: it.Artist, CoverFile: it.File})
		if len(out) >= cfg.ListCount {
			break
		}
	}
	return out
}

// askQueue keeps the queue fresh: at once when the song changes, then every 20 s.
func (v *VinylView) askQueue(snap MediaSnap) {
	if cfg.VinylNext != 0 || !cfg.ShowList || cfg.VinylLayout == 1 || snap.Track == nil || cfg.SpotifyClientID == "" || cfg.SpotifyRefresh == "" {
		return
	}
	if v.qSig != snap.Track.Key || time.Since(v.qAsk) > 20*time.Second {
		v.qSig, v.qAsk = snap.Track.Key, time.Now()
		spotify.refreshQueue()
	}
}

// renderRecord paints a whole record (body, grooves, label, hole, wear) into a square bitmap
// of about 2R, centred. img is the album cover (0 = none).
func renderRecord(style int, R float32, key string, img uintptr) *Bitmap {
	L := int(R * 1.98)
	b := newBitmap(L, L)
	if b == nil {
		return nil
	}
	g := b.g
	setInterp(g, interpHighQualityBicubic)
	half := float32(L) / 2
	k := half / (R * 0.99) // local px per record px
	acc := accent()
	dc := discColor()
	picture := style == discPicture
	step := float32(math.Max(2.2, float64(R)*0.012)) * k
	edge := func(dark, light uint32) {
		strokeCircle(g, dark, half, half, half-1.5, 3)
		strokeCircle(g, light, half, half, half-0.5, 1)
	}
	labelR := R * 0.64 * k
	switch style {
	case discPicture:
		labelR = half
	case discSplatter, discMarble:
		paintPattern(g, style, dc, half, key)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x10FFFFFF, 0x1EFFFFFF)
		edge(0x60000000, 0x40FFFFFF)
	case discBlack:
		fillCircle(g, 0xFF0A0A0C, half, half, half)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x12FFFFFF, 0x22FFFFFF)
		edge(0x60000000, 0x50FFFFFF)
	case discColoured:
		fillCircle(g, mixColor(dc, 0xFF000000, 0.25)|0xFF000000, half, half, half)
		softGlow(g, half, half, half*0.95, withAlpha(mixColor(dc, 0xFFFFFFFF, 0.45), 0.3))
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x14000000, 0x24FFFFFF)
		edge(0x50000000, 0x60FFFFFF)
	case discClear: // see-through: tinted glass with grooves
		fillCircle(g, withAlpha(dc, 0.28), half, half, half)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x18FFFFFF, 0x30FFFFFF)
		strokeCircle(g, withAlpha(mixColor(dc, 0xFFFFFFFF, 0.6), 0.8), half, half, half-1, 2)
		strokeCircle(g, 0x30FFFFFF, half, half, R*0.66*k, 1)
	case discGold:
		fillCircle(g, 0xFF8A6418, half, half, half)
		softGlow(g, half, half, half*1.05, 0x80FFE7A0)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x22000000, 0x40FFF3C8)
		edge(0x40000000, 0xC0FFE9A8)
	case discNeon: // black vinyl, neon tubes at the rim and around the label
		fillCircle(g, 0xFF08080A, half, half, half)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x10FFFFFF, 0x1CFFFFFF)
		n := mixColor(dc, 0xFFFFFFFF, 0.15) | 0xFF000000
		for _, rr := range []float32{0.97, 0.81, 0.68} {
			strokeCircle(g, withAlpha(n, 0.18), half, half, R*rr*k, R*0.05*k)
			strokeCircle(g, withAlpha(n, 0.45), half, half, R*rr*k, R*0.016*k)
			strokeCircle(g, withAlpha(mixColor(n, 0xFFFFFFFF, 0.6), 0.95), half, half, R*rr*k, R*0.006*k+0.6)
		}
	case discTwoTone: // half and half, split straight through the middle
		clip := clipCircle(g, half, half, half)
		fillPie(g, mixColor(dc, 0xFF000000, 0.2)|0xFF000000, 0, 0, float32(L), float32(L), 0, 180)
		fillPie(g, mixColor(dc, 0xFFFFFFFF, 0.62)|0xFF000000, 0, 0, float32(L), float32(L), 180, 180)
		clipReset(g)
		freePath(clip)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x16000000, 0x26FFFFFF)
		edge(0x50000000, 0x60FFFFFF)
	case discGlowDark: // phosphor green, brighter bands where the songs are
		fillCircle(g, 0xFF0D1A12, half, half, half)
		softGlow(g, half, half, half, 0x5A5CFF9A)
		grooves(g, half, half, R*0.66*k, half*0.995, step, 0x225CFF9A, 0x447CFFB2)
		for _, rr := range []float32{0.9, 0.78} { // the gaps between tracks
			strokeCircle(g, 0x50061008, half, half, R*rr*k, R*0.012*k)
		}
		edge(0x50000000, 0x907CFFB2)
	case discCD: // silver disc; the rainbow is drawn on top each frame (it stays with the light)
		fillCircle(g, 0xFFB7BDC7, half, half, half)
		softGlow(g, half, half, half, 0x70F4F7FF)
		for r := R * 0.6 * k; r < half*0.99; r += step * 2.2 {
			strokeCircle(g, 0x0C000000, half, half, r, 0.8)
		}
		edge(0x50404650, 0xC0FFFFFF)
		labelR = R * 0.56 * k
	}

	// the label (the whole record for a picture disc): the album cover
	if !picture {
		fillCircle(g, 0xFF1C1C20, half, half, labelR)
	}
	if img != 0 {
		disc := newPath() // (a smooth round edge, not a stair-stepped clip)
		addEllipse(disc, half-labelR, half-labelR, 2*labelR, 2*labelR)
		fillPathCover(g, img, half-labelR, half-labelR, 2*labelR, disc)
		freePath(disc)
	} else {
		fillCircle(g, withAlpha(acc, 0.35), half, half, labelR*0.98)
		f := font("Segoe UI Symbol", float64(labelR*0.5), false)
		tw, th := measure(f, "♪")
		drawText(g, f, "♪", half-tw/2, half-th/2, tw+4, 0xCCFFFFFF)
	}
	switch {
	case picture:
		// grooves, white rings and the spindle hole over the printed cover
		pstep := float32(math.Max(2.6, float64(R)*0.014)) * k
		i := 0
		for r := R * 0.36 * k; r < half*0.995; r += pstep {
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
		edge(0x70000000, 0x40FFFFFF)
		fillCircle(g, 0xFF0B0B0D, half, half, R*0.03*k+2)
	case style == discCD:
		// printed art ends in a thin silver ring; clear plastic hub around the hole
		strokeCircle(g, 0xB0E8ECF2, half, half, labelR, R*0.012*k)
		fillCircle(g, 0x70DCE4EE, half, half, R*0.2*k)
		strokeCircle(g, 0x90FFFFFF, half, half, R*0.2*k, 1.2)
		strokeCircle(g, 0x50FFFFFF, half, half, R*0.13*k, 1)
		fillCircle(g, 0xFF0B0B0D, half, half, R*0.07*k)
		strokeCircle(g, 0x80FFFFFF, half, half, R*0.07*k, 1)
	default:
		strokeCircle(g, 0x66000000, half, half, labelR-1.5, 3)
		strokeCircle(g, 0x22FFFFFF, half, half, labelR*0.16, 1.2)
		fillCircle(g, 0xFF0B0B0D, half, half, labelR*0.045+2)
		fillCircle(g, 0x55FFFFFF, half-labelR*0.012, half-labelR*0.012, labelR*0.015+0.8)
	}
	if cfg.Worn {
		paintWear(g, half, R*k, style == discCD, key)
	}
	return b
}

// paintWear: dust specks, fine scratches and a little warmth — an old, much-played record.
// Same song = same wear. rr = record radius in bitmap px.
func paintWear(g uintptr, half, rr float32, cd bool, key string) {
	seed := uint32(0x5EEDBEEF)
	for _, b := range []byte(key) {
		seed = (seed ^ uint32(b)) * 16777619
	}
	rnd := func() float32 { seed = hash32(seed + 0x9E3779B9); return float32(seed%10000) / 10000 }
	clip := clipCircle(g, half, half, half*0.995)
	defer func() { clipReset(g); freePath(clip) }()
	fillCircle(g, 0x0E7A5A30, half, half, half) // a warm, faded tint
	speck := uint32(0xFFFFFFFF)
	scratch := uint32(0x30FFFFFF)
	if cd {
		speck, scratch = 0xFF505860, 0x40FFFFFF
	}
	for i := 0; i < 160; i++ { // dust
		a, d := rnd()*2*math.Pi, rr*float32(math.Sqrt(float64(rnd())))
		x, y := half+d*float32(math.Cos(float64(a))), half+d*float32(math.Sin(float64(a)))
		r := (0.35 + rnd()*rnd()*1.4) * float32(math.Max(1, float64(rr)/190))
		fillCircle(g, withAlpha(speck, 0.18+float64(rnd())*0.35), x, y, r)
	}
	for i := 0; i < 5; i++ { // a few hairs / fibres
		a, d := rnd()*2*math.Pi, rr*(0.5+rnd()*0.45)
		x, y := half+d*float32(math.Cos(float64(a))), half+d*float32(math.Sin(float64(a)))
		p := newPen(withAlpha(speck, 0.35), 0.8)
		drawArc(g, p, x-rr*0.04, y-rr*0.03, rr*0.08, rr*0.06, rnd()*360, 90+rnd()*120)
		freePen(p)
	}
	for i := 0; i < 16; i++ { // short scratches across the grooves
		a, d := rnd()*2*math.Pi, rr*(0.4+rnd()*0.58)
		x, y := half+d*float32(math.Cos(float64(a))), half+d*float32(math.Sin(float64(a)))
		l := rr * (0.04 + rnd()*0.18)
		ta := float64(a) + math.Pi/2 + float64(rnd()-0.5)*1.2
		dx, dy := l*float32(math.Cos(ta))/2, l*float32(math.Sin(ta))/2
		line(g, scratch, 0.7, x-dx, y-dy, x+dx, y+dy)
	}
	for i := 0; i < 3; i++ { // long, curved ones that follow the grooves a while
		r := rr * (0.55 + rnd()*0.4)
		p := newPen(withAlpha(scratch, 0.8), 0.6)
		drawArc(g, p, half-r, half-r, 2*r, 2*r, rnd()*360, 25+rnd()*60)
		freePen(p)
	}
}

// renderHolo: the CD's rainbow — two rainbows around the disc with two bright streaks
// where the light hits, a clear middle (the printed label shows through).
func renderHolo(R float32) *Bitmap {
	S := int(2 * R)
	b := newBitmap(S, S)
	if b == nil {
		return nil
	}
	g := b.g
	gClear(g, 0)
	sz := float32(S)
	for i := 0; i < 120; i++ {
		a0 := float64(i) * 3
		th := a0 + 1.5
		hue := math.Mod(th*2/360+0.62, 1)
		streak := math.Pow(math.Abs(math.Cos((th-38)*math.Pi/180)), 6)
		fillPie(g, withAlpha(hueColor(hue), 0.07+0.36*streak), 0, 0, sz, sz, float32(a0), 3.4)
	}
	for _, a := range []float32{38, 218} { // the bright cores of the light
		fillPie(g, 0x38FFFFFF, 0, 0, sz, sz, a-5, 10)
		fillPie(g, 0x30FFFFFF, 0, 0, sz, sz, a-1.5, 3)
	}
	setCopyMode(g, true) // punch out the label and the rim
	fillCircle(g, 0, sz/2, sz/2, R*0.575)
	setCopyMode(g, false)
	return b
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

// drawList: the song wheel on an arc around the open side of the record.
// Up next (1.19.2): the song playing in the middle, highlighted; the next songs above it, the
// ones that played below. Recently played: the playing song first, what played after it.
func (v *VinylView) drawList(g uintptr, upNext []HistItem) {
	snap := media.Snapshot()
	setTextHint(g, 3) // grid-fitted: small text stays crisp
	type item struct {
		title, artist string
		img           uintptr
		slot          int    // 0 = playing; -1, -2 … next (above); 1, 2 … played (below)
		label         string // small caption above the title
		hint          bool   // "connect Spotify" stand-in: no cover
	}
	var items []item
	cur := item{title: T("Nothing playing"), artist: T("Play something on Spotify"), label: T("NOW PLAYING")}
	if snap.Track != nil {
		cur.title, cur.artist = snap.Track.Title, snap.Track.Artist
		if cover.key == snap.Track.Key {
			cur.img = cover.img
		}
	}
	var past []HistItem
	for _, h := range cfg.History {
		if snap.Track == nil || h.Key != snap.Track.Key {
			past = append(past, h)
		}
	}
	if cfg.VinylNext == 0 {
		nPrev := cfg.ListCount / 2
		nNext := cfg.ListCount - nPrev
		for i, h := range upNext {
			if i >= nNext {
				break
			}
			it := item{title: h.Title, artist: h.Artist, img: historyImage(h), slot: -(i + 1)}
			if i == 0 {
				it.label = T("UP NEXT")
			}
			items = append(items, it)
		}
		if len(upNext) == 0 && nNext > 0 && (cfg.SpotifyClientID == "" || cfg.SpotifyRefresh == "") {
			items = append(items, item{title: T("Up next"), artist: T("Connect Spotify in the settings"), slot: -1, hint: true})
		}
		items = append(items, cur)
		for i, h := range past {
			if i >= nPrev {
				break
			}
			it := item{title: h.Title, artist: h.Artist, img: historyImage(h), slot: i + 1}
			if i == 0 {
				it.label = T("PLAYED")
			}
			items = append(items, it)
		}
	} else { // the playing song, then what played (the 1.0 – 1.19.1 wheel)
		cur.slot = 0
		items = append(items, cur)
		for i, h := range past {
			if i >= cfg.ListCount {
				break
			}
			items = append(items, item{title: h.Title, artist: h.Artist, img: historyImage(h), slot: i + 1})
		}
	}

	txt := textColor()
	tint := accent() | 0xFF000000 // labels take the album's colour, softened to read on glass
	if albumAccent != 0 {
		tint = albumAccent | 0xFF000000
	}
	tint = softTint(tint)
	R := float32(v.R)
	ui := float32(uiScale())
	floor := func(x, min float32) float32 {
		if x < min*ui {
			return min * ui
		}
		return x
	}
	small := v.R < 150*float64(ui) // small wheel: no tilt (tilted small text goes soft)
	// spacing: 21° — more when the minimum sizes make the rows taller than that (small wheels)
	step := 21.0
	if need := float64(floor(R*0.22, 30)*1.44/2+floor(R*0.16, 22)/2+6*ui) / (v.R * 1.16) * 180 / math.Pi; need > step {
		step = need
	}
	for _, it := range items {
		var deg float64
		if cfg.VinylNext == 0 {
			// screen-up is negative local angles for bottom corners, positive for top corners
			deg = float64(it.slot) * step * v.sy
		} else {
			deg = -30 + float64(it.slot)*18
		}
		now := it.slot == 0 && !it.hint
		ra := v.R * 1.15
		if now && cfg.VinylNext == 0 {
			ra = v.R * 1.17
		}
		a := deg * math.Pi / 180
		ax := float32(math.Round(v.cx + v.sx*ra*math.Cos(a)))
		ay := float32(math.Round(v.cy + v.sy*ra*math.Sin(a)))
		gTranslate(g, ax, ay)
		if !small {
			gRotate(g, float32(deg*0.4*v.sx*v.sy))
		}
		cs := floor(R*0.16, 22)
		ts, as := floor(R*0.078, 11.5), floor(R*0.06, 9.5)
		alpha := 0.5
		if it.slot < 0 {
			alpha = 0.58 // what's coming is a little brighter than what's gone
		}
		if now {
			cs, ts, as = floor(R*0.22, 30), floor(R*0.105, 14), floor(R*0.068, 10.5)
			alpha = 1
		}
		if cfg.VinylNext != 0 && !now {
			alpha = 0.78
		}
		gap := floor(R*0.06, 7)
		maxW := float32(math.Max(float64(R)*1.3, 150*float64(ui)))
		fT := font("Segoe UI", float64(ts), false)
		if now {
			fT = font("Segoe UI Semibold", float64(ts), false)
		}
		fA := font("Segoe UI", float64(as), false)
		x0 := float32(0) // cover position (right side: grows to the left)
		if v.sx < 0 {
			x0 = -cs
		}
		if now && cfg.VinylNext == 0 { // no box: the cover itself steps forward
			softGlow(g, x0+cs/2, cs*0.04, cs*1.25, withAlpha(tint, 0.26)) // the album's own light
			for i := 0; i < 6; i++ {                                      // a soft, deep shadow
				e := cs * (0.02 + 0.035*float32(i))
				sh := roundRectPath(x0-e*0.5, -cs/2+cs*0.07-e*0.2, cs+e, cs+e, cs*0.16+e*0.5)
				fillPathColor(g, sh, 0x12000000)
				freePath(sh)
			}
		}
		path := roundRectPath(x0, -cs/2, cs, cs, cs*0.16)
		if it.hint {
			strokePath(g, path, withAlpha(txt, 0.3), 1)
		} else {
			fillPathColor(g, path, 0xFF26262B)
			fillPathCover(g, it.img, x0, -cs/2, cs, path)
			if now {
				strokePath(g, path, 0x30FFFFFF, 1)
			} else if alpha < 1 { // dim the cover with the text
				fillPathColor(g, path, withAlpha(0xFF000000, (1-alpha)*0.6))
			}
		}
		freePath(path)
		text := func(f *Font, s string, yy float32, c uint32) {
			yy = float32(math.Round(float64(yy)))
			if v.sx > 0 {
				drawText(g, f, s, cs+gap, yy, maxW, c)
			} else {
				drawTextRight(g, f, s, -cs-gap, yy, maxW, c)
			}
		}
		y := -ts * 0.95
		if it.label != "" {
			lf := font("Segoe UI Semibold", float64(floor(R*0.042, 8)), false)
			la := 1.0
			if !now {
				la = 0.75
			}
			ly := -cs/2 - floor(R*0.012, 1) - lf.size*0.35
			if now && cfg.VinylNext == 0 { // three little bars, like a playing indicator (they dance: see drawNowLabel)
				bw := lf.size * 0.16
				tw, _ := measure(lf, it.label)
				bx := cs + gap
				tx := cs + gap + bw*7
				if v.sx < 0 {
					bx = -cs - gap - tw - bw*7
					tx = -cs - gap - tw
				}
				rly := float32(math.Round(float64(ly)))
				drawText(g, lf, it.label, tx, rly, maxW, withAlpha(tint, la))
				v.np = nowLabel{ok: true, m: getTransform(g), bx: bx, bw: bw, tx: tx, y: rly, tw: tw, size: lf.size, text: it.label, tint: tint}
			} else {
				text(lf, it.label, ly, withAlpha(tint, la))
			}
			if now {
				y = -ts * 0.55
			} else {
				y = -ts * 0.7
			}
		}
		text(fT, it.title, y, withAlpha(txt, alpha))
		artistC := withAlpha(txt, alpha*0.62)
		if now {
			artistC = withAlpha(mixColor(txt, tint, 0.45), 0.9) // the artist line carries the tint
		}
		text(fA, it.artist, y+ts*1.2, artistC)
		gReset(g)
	}
}

func (v *VinylView) draw() {
	if v.w.canvas == nil || v.base == nil {
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
	swapping := v.swapT >= 0
	poses := v.recordPoses()
	if swapping && v.swapStyle == 0 { // only the part the records move over is pushed
		for _, p := range poses {
			e := v.R * 1.22 * p.scale
			dirty.Left = int32(math.Max(0, math.Min(float64(dirty.Left), p.x-e)))
			dirty.Top = int32(math.Max(0, math.Min(float64(dirty.Top), p.y-e)))
			dirty.Right = int32(math.Min(float64(v.w.w), math.Max(float64(dirty.Right), p.x+e)))
			dirty.Bottom = int32(math.Min(float64(v.w.h), math.Max(float64(dirty.Bottom), p.y+e)))
		}
	}
	if v.np.ok && !v.w.offscreen { // the "NOW PLAYING" caption comes alive each frame too
		b := v.np.box()
		dirty.Left = int32(math.Max(0, float64(min(dirty.Left, b.Left))))
		dirty.Top = int32(math.Max(0, float64(min(dirty.Top, b.Top))))
		dirty.Right = int32(math.Min(float64(v.w.w), float64(max(dirty.Right, b.Right))))
		dirty.Bottom = int32(math.Min(float64(v.w.h), float64(max(dirty.Bottom, b.Bottom))))
	}
	full := v.needFull || (swapping && v.swapStyle == 1) // the sleeve travels across the whole window
	v.lastDirty = dirty
	if full {
		v.lastDirty = RECT{0, 0, int32(v.w.w), int32(v.w.h)}
	}
	if !full {
		clipRect(g, float32(dirty.Left), float32(dirty.Top), float32(dirty.Right-dirty.Left), float32(dirty.Bottom-dirty.Top))
	}
	secProf("")
	if bp := v.base.pixels(); bp != nil && v.base.w == v.w.w && v.base.h == v.w.h {
		// full screen: the cached layer back with a plain memory copy (big at 4K, slow through GDI+)
		x0, y0, x1, y1 := 0, 0, v.w.w, v.w.h
		if !full {
			x0, y0, x1, y1 = int(dirty.Left), int(dirty.Top), int(dirty.Right), int(dirty.Bottom)
		}
		gFlush(g)
		dst, W := pix(v.w.canvas), v.w.w
		for y := y0; y < y1; y++ {
			copy(dst[y*W+x0:y*W+x1], bp[y*W+x0:y*W+x1])
		}
	} else {
		setCopyMode(g, true) // copy the cached layer over everything — no separate clear pass
		gImage(g, v.base.bmp, 0, 0, float32(v.w.w), float32(v.w.h))
		setCopyMode(g, false)
	}
	secProf("v.base")
	if cfg.AmbVinyl && cfg.AmbBright > 0 {
		v.drawAmbient(g, full, dirty)
	}
	secProf("v.amb")
	settled := len(poses) == 1 && poses[0].rest && poses[0].show
	if swapping && !settled { // the bare platter while the record is off it
		fillCircle(g, 0xAA0C0C0F, cx, cy, R*0.99)
		strokeCircle(g, 0x22FFFFFF, cx, cy, R*0.6, 1)
		fillCircle(g, 0xFFB9BCC4, cx, cy, R*0.025)
	}
	if settled && cfg.DiscGlow > 0 && v.bass > 0.02 { // the glow breathes with the bass
		gc := discGlowColor()
		a := math.Min(1, float64(v.bass)*1.4) * cfg.DiscGlow / 2
		if true { // the rings in Go (GDI+ wide strokes are slow)
			rs := make([]ring, 0, 3)
			for i, wd := range []float64{0.16, 0.1, 0.05} {
				rs = append(rs, ring{float64(R) * (1.0 + wd/2), float64(R) * wd, a * []float64{0.18, 0.3, 0.55}[i] * float64(gc>>24) / 255})
			}
			v.goRings(g, rs, gc, dirty, full)
		} else {
			for i, wd := range []float32{0.16, 0.1, 0.05} {
				strokeCircle(g, withAlpha(gc, a*[]float64{0.18, 0.3, 0.55}[i]), cx, cy, R*(1.0+wd/2), R*wd)
			}
		}
	}
	secProf("v.rings")
	for _, pose := range poses {
		if !pose.show || pose.lbl == nil {
			continue
		}
		px, py, sc := float32(pose.x), float32(pose.y), float32(pose.scale)
		if pose.lift > 0 { // lifted: its shadow falls further away and softer
			softGlow(g, px+R*0.05*float32(pose.lift), py+R*0.09*float32(pose.lift), R*1.12*sc, withAlpha(0xFF000000, 0.5*pose.lift*pose.alpha))
		}
		L := float32(pose.lbl.w) * sc
		if R >= 160 { // a big record turns in Go (GDI+ is slow at this size); a small one keeps GDI+'s sharper filter
			if src := pose.lbl.pixels(); src != nil {
				clip := [4]int{int(dirty.Left), int(dirty.Top), int(dirty.Right), int(dirty.Bottom)}
				if full {
					clip = [4]int{0, 0, v.w.w, v.w.h}
				}
				gFlush(g)
				rotateOver(pix(v.w.canvas), v.w.w, v.w.h, src, pose.lbl.w, pose.lbl.h, float64(px), float64(py), float64(L), v.angle, pose.alpha, clip)
				if cfg.DiscStyle == discCD && v.holo != nil {
					v.drawHolo(g, px, py, sc, pose.alpha, pose.rest)
				}
				continue
			}
		}
		if R < 160 { // a small record turns with the better filter (cheap at that size, much sharper)
			setInterp(g, interpHighQualityBicubic)
		}
		gTranslate(g, px, py)
		gRotate(g, float32(v.angle))
		gImageAlpha(g, pose.lbl.bmp, pose.lbl.w, pose.lbl.h, -L/2, -L/2, L, L, pose.alpha)
		gReset(g)
		if R < 160 {
			setInterp(g, interpBilinear)
		}
		if cfg.DiscStyle == discCD && v.holo != nil {
			v.drawHolo(g, px, py, sc, pose.alpha, pose.rest)
		}
	}
	secProf("v.rotate")
	pose := recPose{rest: settled}
	if pose.rest && cfg.GrooveLight {
		v.drawGrooveLight(g)
	}
	if pose.rest && cfg.Shine { // light reflections stay still while the record turns
		fillPie(g, 0x14FFFFFF, cx-R, cy-R, 2*R, 2*R, -62, 34)
		fillPie(g, 0x10FFFFFF, cx-R, cy-R, 2*R, 2*R, 118, 30)
	}
	secProf("v.groove")
	if swapping && v.swapStyle == 1 {
		sleeve, oldOne, _ := v.swapPose()
		v.drawSleeve(g, sleeve, oldOne)
	}
	secProf("v.sleeve")
	if cfg.ShowArm {
		v.drawArm(g)
	}
	secProf("v.arm")
	v.drawSparks(g)
	secProf("v.sparks")
	if v.scr.on {
		v.drawScratchTime(g)
	}
	secProf("v.scr")
	if v.np.ok && !v.w.offscreen {
		snap := media.Snapshot()
		v.drawNowLabel(g, snap.Playing && snap.Track != nil && cfg.FPS > 0, time.Now())
	}
	secProf("v.label")
	if full {
		v.w.present()
		v.needFull = false
	} else {
		clipReset(g)
		v.w.presentDirty(dirty)
	}
	secProf("v.present")
}

// drawHolo: the CD's rainbow turns a little with the disc (so it drifts across it) and
// flashes on the bass.
func (v *VinylView) drawHolo(g uintptr, cx, cy, sc float32, alpha float64, flash bool) {
	R := float32(v.R) * sc
	a := float32(v.angle*0.16 + 9*math.Sin(v.ambT*0.45))
	gTranslate(g, cx, cy)
	gRotate(g, a)
	gImageAlpha(g, v.holo.bmp, v.holo.w, v.holo.h, -R, -R, 2*R, 2*R, alpha)
	gReset(g)
	if b := float64(v.bass); flash && b > 0.06 {
		al := math.Min(0.55, b*0.75)
		p := newPen(withAlpha(0xFFFFFFFF, al*0.5), R*0.4)
		for _, s := range []float32{38, 218} {
			drawArc(g, p, cx-R*0.78, cy-R*0.78, R*1.56, R*1.56, s+a-7, 14)
		}
		freePen(p)
		p = newPen(withAlpha(hueColor(math.Mod(v.ambT*0.1, 1)), al*0.6), R*0.4)
		for _, s := range []float32{38, 218} {
			drawArc(g, p, cx-R*0.78, cy-R*0.78, R*1.56, R*1.56, s+a+8, 18)
		}
		freePen(p)
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
	R, cx, cy := float64(v.R), float64(v.cx), float64(v.cy)
	r := R * 1.62 * cfg.AmbSize
	bass := float64(v.bass)
	if !v.playing {
		bass = 0
	}
	// two soft lights around the record (not over it), drawn in Go: GDI+'s gradients at
	// this size took most of the frame
	t := v.ambT
	lv := ambLevel(cfg.AmbEffect, t, bass)
	a := cfg.AmbBright * lv * 0.55
	c1, c2 := ambColor(t)
	dx := math.Sin(t*0.23) * r * 0.06
	clip := [4]int{int(dirty.Left), int(dirty.Top), int(dirty.Right), int(dirty.Bottom)}
	if full {
		clip = [4]int{0, 0, v.w.w, v.w.h}
	}
	gFlush(g)
	dst := pix(v.w.canvas)
	glowOver(dst, v.w.w, v.w.h, cx+v.sx*r*0.12+dx, cy-r*0.1, r, withAlpha(c1, a), clip, cx, cy, R)
	glowOver(dst, v.w.w, v.w.h, cx-v.sx*r*0.18-dx, cy+r*0.06, r*0.78, withAlpha(c2, a*0.75), clip, cx, cy, R)
}

// nowLabel: where the song wheel's "NOW PLAYING" caption was drawn (in the wheel's own tilted
// coordinates, kept as the drawing transform of that moment).
type nowLabel struct {
	ok                      bool
	m                       [6]float32
	bx, bw, tx, y, tw, size float32
	text                    string
	tint                    uint32
}

// box: the caption and its bars on the window (for the part redrawn each frame).
func (n nowLabel) box() RECT {
	pts := [][2]float32{{n.bx - n.size*0.3, n.y - n.size*0.3}, {n.tx + n.tw + n.size*0.3, n.y - n.size*0.3},
		{n.bx - n.size*0.3, n.y + n.size*1.5}, {n.tx + n.tw + n.size*0.3, n.y + n.size*1.5}}
	r := RECT{1 << 30, 1 << 30, -1 << 30, -1 << 30}
	for _, p := range pts {
		x := n.m[0]*p[0] + n.m[2]*p[1] + n.m[4]
		y := n.m[1]*p[0] + n.m[3]*p[1] + n.m[5]
		r.Left, r.Top = min(r.Left, int32(x)-1), min(r.Top, int32(y)-1)
		r.Right, r.Bottom = max(r.Right, int32(x)+2), max(r.Bottom, int32(y)+2)
	}
	return r
}

// drawNowLabel: the bars beside "NOW PLAYING" move with the music, and now and then a soft
// light runs across the words.
func (v *VinylView) drawNowLabel(g uintptr, playing bool, now time.Time) {
	n := v.np
	if !n.ok {
		return
	}
	setTransform(g, n.m)
	defer gReset(g)
	for k, base := range []float32{0.55, 1, 0.75} {
		h := base
		if playing {
			b := float64(v.bands[[3]int{2, 6, 11}[k]%nBands]) * 1.6
			t := float64(now.UnixMilli()) / 1000
			idle := 0.5 + 0.45*math.Sin(t*(5.1+float64(k)*1.7)+float64(k)*2.1)*math.Sin(t*(2.3+float64(k)*0.9)) // its own sway when there's no sound to follow
			h = float32(0.22 + 0.78*math.Min(1, math.Max(b, idle*0.8)))
		}
		hh := n.size * 0.7 * float32(math.Max(0.2, float64(h)))
		fillRect(g, withAlpha(n.tint, 0.95), n.bx+float32(k)*n.bw*1.9, n.y+n.size*0.98-hh, n.bw, hh)
	}
	if !playing {
		return
	}
	// the sheen: 1.1 s across, then a pause (every 3.6 s)
	ph := math.Mod(float64(now.UnixMilli())/1000, 3.6) / 1.1
	if ph >= 1 {
		return
	}
	bandW := n.size * 2.2
	cx := n.tx - bandW + float32(ph)*(n.tw+2*bandW)
	for i, k := range []float32{1, 0.6, 0.3} { // soft edges: three bands, narrower and brighter inside
		w := bandW * k
		clipRect(g, cx-w/2, n.y-n.size*0.2, w, n.size*1.6)
		drawText(g, font("Segoe UI Semibold", float64(n.size), false), n.text, n.tx, n.y, n.tw+n.size, withAlpha(0xFFFFFFFF, []float64{0.35, 0.45, 0.6}[i]))
		clipReset(g)
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
	nw, _ := measure(fN, T("NOW PLAYING"))
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
		fillPathCover(g, cover.img, x0, y0, cs, p)
		freePath(p)
	} else {
		p := roundRectPath(x0, y0, cs, cs, cs*0.16)
		fillPathColor(g, p, withAlpha(accent(), 0.5))
		freePath(p)
	}
	tx := x0 + cs + R*0.08
	acc := accent() | 0xFF000000
	txt := textColor()
	drawText(g, fN, T("NOW PLAYING"), tx, y0-R*0.005, maxW, acc)
	drawText(g, fT, t.Title, tx+1, y0+R*0.07+1.5, maxW, 0x66000000)
	drawText(g, fT, t.Title, tx, y0+R*0.07, maxW, txt)
	drawText(g, fA, t.Artist, tx, y0+R*0.07+R*0.16, maxW, withAlpha(txt, 0.65))
}

// drawGrooveLight: the music lights up the grooves — bass on the outside, highs near the label.
func (v *VinylView) drawGrooveLight(g uintptr) {
	R := float32(v.R)
	c := accent()
	if cfg.DiscStyle != discPicture && cfg.DiscStyle != discBlack {
		c = mixColor(discGlowColor(), 0xFFFFFFFF, 0.35)
	}
	inner := float32(0.68)
	if cfg.DiscStyle == discPicture {
		inner = 0.4
	}
	const rings = 12
	var rs []ring
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
		rs = append(rs, ring{float64(r), float64(R*0.012 + lv*R*0.012), float64(lv) * 0.55 * float64(c>>24) / 255})
	}
	if len(rs) > 0 {
		v.goRings(g, rs, c, v.lastDirty, false)
	}
}

// goRings: rings drawn straight into the canvas (full screen's record) — see ringsOver.
func (v *VinylView) goRings(g uintptr, rs []ring, col uint32, dirty RECT, full bool) {
	clip := [4]int{int(dirty.Left), int(dirty.Top), int(dirty.Right), int(dirty.Bottom)}
	if full {
		clip = [4]int{0, 0, v.w.w, v.w.h}
	}
	gFlush(g)
	ringsOver(pix(v.w.canvas), v.w.w, v.w.h, v.cx, v.cy, rs, col&0xFFFFFF, clip)
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
	v.tipX, v.tipY = Sx, Sy
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

	// 1.19
	parts    []particle
	pal      []uint32 // colours picked from the cover (Particles)
	palVer   int
	prevBass float32
	kickT    float64 // time since the last kick
	wave     [384]float32
	trails   [3][]PointF // the phosphor's afterglow: the last traces
	blobT    float64
	flick    float32
}

type particle struct {
	ang, home, off, vel, spin, size float32
	col                             int
}

func newHaloView() *HaloView {
	return &HaloView{w: newWidget("halo", "Halo"), last: time.Now(), palVer: -1}
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
	switch cfg.HaloStyle {
	case visParticles:
		if h.updateParticles(dt, bass, snap.Playing && animate) {
			moving = true
		}
	case visScope:
		if snap.Playing && animate && spectrum.Wave(h.wave[:]) {
			moving = true
		} else {
			for i := range h.wave {
				h.wave[i] *= 0.6
			}
		}
		h.flick = 0.86 + 0.14*rand.Float32()
	case visLiquid:
		if snap.Playing && animate {
			h.blobT += dt * (0.6 + 1.4*float64(bass))
			moving = true
		}
	}
	h.prevBass = bass
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
	secProf("")
	h.w.clearCanvas()
	if cfg.HaloBackdrop > 0 {
		bg := roundRectPath(2, 2, W-4, H-4, sz*0.07)
		fillPathColor(g, bg, withAlpha(0xFF0E0E13, 0.9*cfg.HaloBackdrop))
		strokePath(g, bg, withAlpha(0xFFFFFFFF, 0.08*cfg.HaloBackdrop), 1)
		freePath(bg)
	}
	secProf("h.clear")
	// the soft lights in Go: GDI+'s gradients at this size took most of the frame
	gFlush(g)
	dst, whole := pix(h.w.canvas), [4]int{0, 0, h.w.w, h.w.h}
	C := float64(c)
	if cfg.AmbHalo && cfg.AmbBright > 0 {
		t, ar := h.ambT, float64(sz)*math.Min(0.42, 0.33*cfg.AmbSize)
		a := cfg.AmbBright * ambLevel(cfg.AmbEffect, t, float64(bass)) * 0.55
		c1, c2 := ambColor(t)
		dx := math.Sin(t*0.23) * ar * 0.06
		glowOver(dst, h.w.w, h.w.h, C+ar*0.12+dx, C-ar*0.1, ar, withAlpha(c1, a), whole, 0, 0, 0)
		glowOver(dst, h.w.w, h.w.h, C-ar*0.18-dx, C+ar*0.06, ar*0.78, withAlpha(c2, a*0.75), whole, 0, 0, 0)
	}
	if cfg.HaloArtGlow {
		ag := ca
		if albumAccent != 0 {
			ag = albumAccent | 0xFF000000
		}
		glowOver(dst, h.w.w, h.w.h, C, C, float64(sz)*0.5, withAlpha(ag, 0.3+0.25*float64(bass)), whole, 0, 0, 0)
	}
	if cfg.HaloGlow > 0 {
		glowOver(dst, h.w.w, h.w.h, C, C, float64(r)*1.55, withAlpha(ca, (0.16+0.3*float64(bass))*cfg.HaloGlow), whole, 0, 0, 0)
	}
	secProf("h.glow")
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
			// the spokes in Go (GDI+ wide anti-aliased lines are slow, and there are hundreds)
			gFlush(g)
			for pass := 0; pass < 2; pass++ {
				if pass == 0 && cfg.HaloGlow <= 0 {
					continue
				}
				for i := 0; i < n; i++ {
					a := -math.Pi/2 + float64(i)/float64(n)*2*math.Pi
					l := float64(2 + h.bars[i]*maxLen)
					k := int(math.Min(steps-1, float64(h.bars[i])*steps))
					col := mixColor(ca, cb, float64(k)/(steps-1))
					w := float64(bw)
					if pass == 0 {
						col, w = withAlpha(col, 0.22*math.Min(1, cfg.HaloGlow)), w*2.6
					} else {
						col = withAlpha(col, 0.95)
					}
					cs, sn, R0 := math.Cos(a), math.Sin(a), float64(rb)
					lineOver(dst, h.w.w, h.w.h, C+cs*R0, C+sn*R0, C+cs*(R0+l), C+sn*(R0+l), w, col, whole)
				}
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
				for i := 0; i < n; i++ {
					x1, y1 := pt(i)
					x2, y2 := pt(i + 1)
					col := colAt(h.bars[i])
					if pass == 0 {
						col = withAlpha(col, 0.2*math.Min(1, cfg.HaloGlow))
					}
					line(g, col, wd, x1, y1, x2, y2)
				}
			}
			strokeCircle(g, withAlpha(ca, 0.35), c, c, rb+1, 1)
		}
		secProf("h.bars")
		if h.label != nil {
			pulse := 1 + float32(cfg.HaloPulse)*bass
			L := float32(h.label.w) * pulse
			if src := h.label.pixels(); src != nil && L >= 200 { // a big cover turns in Go (GDI+ is slow at this size)
				gFlush(g)
				rotateOver(pix(h.w.canvas), h.w.w, h.w.h, src, h.label.w, h.label.h, float64(c), float64(c), float64(L), h.spin, 1, [4]int{0, 0, h.w.w, h.w.h})
			} else {
				gTranslate(g, c, c)
				gRotate(g, float32(h.spin))
				gImage(g, h.label.bmp, -L/2, -L/2, L, L)
				gReset(g)
			}
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
	case visParticles:
		h.drawParticles(g, c, r, sz)
		h.drawLabel(g, c, bass)
	case visScope:
		h.drawScope(g, c, sz)
	case visLiquid:
		h.drawLiquid(g, c, r, sz, bass, ca, cb)
		h.drawLabel(g, c, bass)
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
		}
	}
	if cfg.HaloCard > 0 {
		h.drawCard(g, sz, W, H)
	}
	if framed("halo") {
		editFrame(g, W, H, "halo", "Visualizer")
	}
	secProf("h.label")
	h.w.present()
	secProf("h.present")
}

// drawLabel: the turning, bass-pulsing cover in the middle.
func (h *HaloView) drawLabel(g uintptr, c float32, bass float32) {
	if h.label == nil {
		return
	}
	pulse := 1 + float32(cfg.HaloPulse)*bass
	L := float32(h.label.w) * pulse
	gTranslate(g, c, c)
	gRotate(g, float32(h.spin))
	gImage(g, h.label.bmp, -L/2, -L/2, L, L)
	gReset(g)
	strokeCircle(g, withAlpha(0xFFFFFFFF, 0.35), c, c, L/2, 1.5)
}

/* ---------- 1.19: Particles ---------- */

const nParticles = 150

// updateParticles: a kick (the bass jumping up) throws every particle outward; a spring pulls
// it back to its place on the ring. true while anything moves.
func (h *HaloView) updateParticles(dt float64, bass float32, playing bool) bool {
	if cover.ver != h.palVer {
		h.palVer = cover.ver
		h.pal = nil
		if cover.img != 0 {
			if b := newBitmap(14, 14); b != nil {
				gImage(b.g, cover.img, 0, 0, 14, 14)
				px := make([]uint32, 0, 196)
				for y := 0; y < 14; y++ {
					for x := 0; x < 14; x++ {
						px = append(px, pixel(b.bmp, x, y))
					}
				}
				b.Free()
				h.pal = paletteFromPixels(px, 6)
			}
		}
	}
	if len(h.parts) != nParticles {
		h.parts = make([]particle, nParticles)
		for i := range h.parts {
			h.parts[i] = particle{ang: rand.Float32() * 2 * math.Pi, home: rand.Float32(),
				spin: (rand.Float32() - 0.5) * 0.25, size: 0.4 + rand.Float32()*rand.Float32()*1.6, col: rand.Intn(64)}
		}
	}
	h.kickT += dt
	sens := float32(cfg.HaloSens)
	if playing && bass*sens > 0.28 && bass-h.prevBass > 0.06 && h.kickT > 0.14 {
		h.kickT = 0
		hit := float32(math.Min(1.6, float64((bass-h.prevBass)*6*sens)))
		for i := range h.parts {
			h.parts[i].vel += hit * (0.5 + rand.Float32()) * (1.3 - 0.6*h.parts[i].home)
		}
	}
	moving := false
	d := float32(math.Min(dt, 0.05))
	for i := range h.parts {
		p := &h.parts[i]
		acc := -38*p.off - 7*p.vel // spring home, with damping
		p.vel += acc * d
		p.off += p.vel * d
		if p.off < -0.05 {
			p.off, p.vel = -0.05, 0
		}
		if playing {
			p.ang += p.spin * d * (1 + 3*p.off)
			moving = true
		}
		if math.Abs(float64(p.vel)) > 0.002 || p.off > 0.002 {
			moving = true
		}
	}
	return moving
}

func (h *HaloView) drawParticles(g uintptr, c, r, sz float32) {
	ca, cb := visColors()
	pal := h.pal
	if cfg.HaloPalette != 1 || len(pal) == 0 { // not "Album colours": the visualizer palette
		pal = []uint32{ca, mixColor(ca, cb, 0.5), cb, mixColor(cb, 0xFFFFFFFF, 0.4)}
	}
	reach := sz * 0.22 * float32(cfg.HaloLength)
	for _, p := range h.parts {
		rad := r*1.12 + p.home*sz*0.06 + p.off*reach
		if lim := sz * 0.49; rad > lim {
			rad = lim
		}
		x := c + rad*float32(math.Cos(float64(p.ang)))
		y := c + rad*float32(math.Sin(float64(p.ang)))
		col := pal[p.col%len(pal)]
		size := sz * 0.0045 * p.size * (1 + 0.8*p.off)
		a := math.Max(0.35, 1-float64(p.off)*0.35)
		if cfg.HaloGlow > 0 && p.size > 1 {
			softGlow(g, x, y, size*4, withAlpha(col, 0.35*a*math.Min(1, cfg.HaloGlow)))
		}
		if p.off > 0.08 { // a short streak while it flies
			back := float32(math.Min(float64(p.vel), 2)) * reach * 0.05
			bx, by := c+(rad-back)*float32(math.Cos(float64(p.ang))), c+(rad-back)*float32(math.Sin(float64(p.ang)))
			line(g, withAlpha(col, a*0.45), size*1.2, bx, by, x, y)
		}
		fillCircle(g, withAlpha(col, a), x, y, size)
	}
}

/* ---------- 1.19: Oscilloscope ---------- */

func (h *HaloView) drawScope(g uintptr, c, sz float32) {
	const green = 0xFF39FF7A
	x0, w := sz*0.07, sz*0.86
	y0, hh := c-sz*0.3, sz*0.6
	// the CRT: dark glass, a fine grid, a soft bloom in the middle
	screen := roundRectPath(x0, y0, w, hh, sz*0.04)
	fillPathColor(g, screen, 0xD6020A05)
	strokePath(g, screen, 0x5539FF7A, 1.2)
	clipPath(g, screen)
	softGlow(g, c, c, sz*0.42, 0x1C39FF7A)
	for i := 1; i < 10; i++ {
		x := x0 + w*float32(i)/10
		col := uint32(0x1439FF7A)
		if i == 5 {
			col = 0x2C39FF7A
		}
		line(g, col, 1, x, y0, x, y0+hh)
	}
	for j := 1; j < 6; j++ {
		y := y0 + hh*float32(j)/6
		col := uint32(0x1439FF7A)
		if j == 3 {
			col = 0x2C39FF7A
		}
		line(g, col, 1, x0, y, x0+w, y)
	}
	// the trace
	n := len(h.wave)
	pts := make([]PointF, n)
	amp := hh * 0.42 * float32(cfg.HaloLength)
	for i := 0; i < n; i++ {
		pts[i] = PointF{x0 + w*float32(i)/float32(n-1), c - h.wave[i]*amp}
	}
	// afterglow: the older traces fade like phosphor
	for k := len(h.trails) - 1; k >= 0; k-- {
		if t := h.trails[k]; len(t) > 1 {
			path := polylinePath(t)
			strokePath(g, path, withAlpha(green, 0.10/float64(k+1)), sz*0.006)
			freePath(path)
		}
	}
	copy(h.trails[1:], h.trails[:len(h.trails)-1])
	h.trails[0] = pts
	path := polylinePath(pts)
	fl := float64(h.flick)
	if cfg.HaloGlow > 0 {
		strokePath(g, path, withAlpha(green, 0.10*fl*math.Min(1, cfg.HaloGlow)), sz*0.04)
		strokePath(g, path, withAlpha(green, 0.22*fl*math.Min(1, cfg.HaloGlow)), sz*0.016)
	}
	strokePath(g, path, withAlpha(green, 0.95*fl), sz*0.0055)
	strokePath(g, path, withAlpha(0xFFE6FFEE, 0.7*fl), sz*0.0018)
	freePath(path)
	// scanlines
	for y := y0; y < y0+hh; y += 3 {
		line(g, 0x14000000, 1, x0, y, x0+w, y)
	}
	clipReset(g)
	freePath(screen)
}

/* ---------- 1.19: Liquid ---------- */

// drawLiquid: a soft blob around the cover. Its bumps come from the music — bass makes
// the big slow ones, the highs the small quick ones — and it swells on the kick.
func (h *HaloView) drawLiquid(g uintptr, c, r, sz float32, bass float32, ca, cb uint32) {
	const n = 72
	amp := sz * 0.075 * float32(cfg.HaloLength)
	shape := func(phase float64, grow float32) []PointF {
		pts := make([]PointF, n)
		for i := 0; i < n; i++ {
			th := float64(i) / n * 2 * math.Pi
			d := float32(0)
			for k := 2; k <= 7; k++ {
				lv := h.level((k-2)*8, 48) // bass → few big bumps, highs → many small ones
				sp := 0.5 + float64(k)*0.25
				d += lv * float32(math.Sin(float64(k)*th+h.blobT*sp+phase+float64(k)*1.7)) / float32(k-1)
			}
			rad := r*1.12 + grow + sz*0.02 + bass*sz*0.035 + d*amp
			pts[i] = PointF{c + rad*float32(math.Cos(th)), c + rad*float32(math.Sin(th))}
		}
		return pts
	}
	if cfg.HaloGlow > 0 {
		softGlow(g, c, c, r*2.1, withAlpha(ca, (0.22+0.3*float64(bass))*math.Min(1, cfg.HaloGlow)))
	}
	outer := closedCurvePath(shape(0, sz*0.035), 0.5)
	fillPathGradient(g, outer, withAlpha(ca, 0.55), withAlpha(cb, 0.55), c-r*2, c+r*2)
	freePath(outer)
	inner := closedCurvePath(shape(2.1, 0), 0.5)
	fillPathGradient(g, inner, withAlpha(cb, 0.75), withAlpha(ca, 0.75), c-r*2, c+r*2)
	strokePath(g, inner, withAlpha(0xFFFFFFFF, 0.35), 1.2)
	freePath(inner)
	// a glossy highlight on the top-left of the blob
	softGlow(g, c-r*0.9, c-r*0.95, r*0.45, 0x30FFFFFF)
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
		fillPathCover(g, cover.img, x, y, cs, path)
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
	// 1.19 LED board: the line as columns of dots (bit r = row r lit), scrolled by ledOff
	// columns; [hi0, hi1) is the word being sung, columns before sung are already sung
	led            []uint16
	ledOff         float32
	hi0, hi1, sung int
}

const (
	rkText = iota
	rkBall
	rkLine
	rkStar
	rkRing
	rkTile // 1.19 split-flap: the tile behind a letter
	rkFlap // 1.19 split-flap: the hinge line and the falling flap (size = 0..1 through a flip)
	rkLED  // 1.19: the LED panel
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
	case rkTile:
		drawTile(g, r, x, y)
	case rkFlap:
		drawFlap(g, r, x, y)
	case rkLED:
		drawLED(g, r, x, y)
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

// lyrBoxH: a taller box for the styles that spread words out (Cinema's lyrics stage); 0 = normal.
var lyrBoxH float64

// lyrFitW/H: Cinema's stage box. When set, every style that shows the line as a block makes each
// line as big as fits it (worked out once per line, so it never changes size while it is sung).
// The desktop widget never sets them.
var lyrFitW, lyrFitH float64

func fitsStage(style int) bool {
	switch style {
	case stScatter, stConstellation: // these fill the stage their own way
		return false
	}
	return true
}

// fitFont: the font size that makes this line fill the stage.
func (lv *LyricsView) fitFont(L *Line, style int, F float64) float64 {
	key := fmt.Sprintf("%s|%d|%.1f|%s|%v|%.0f|%.0f", L.Text, style, F, cfg.FontName, cfg.Bold, lyrFitW, lyrFitH)
	if key == lv.fitKey {
		return lv.fitF
	}
	lv.fitKey, lv.fitF = key, fitFor(L, style, F)
	return lv.fitF
}

// warm gets the next line ready while this one is sung: its size, its font and its words'
// outlines — so the frame where it starts has nothing slow left to do (Cinema).
func (lv *LyricsView) warm(L *Line, style int, ui float64) {
	if L == nil || len(L.Words) == 0 || lyrFitW <= 0 {
		return
	}
	F := lv.fontSize(ui)
	if fitsStage(style) {
		F = fitFor(L, style, F)
	}
	f := font(cfg.FontName, F, cfg.Bold)
	for _, w := range L.Words {
		cachedTextPath(f, upper(w.W)).size(f, upper(w.W))
	}
}

// fitFor: the font size that makes line L fill the stage.
func fitFor(L *Line, style int, F float64) float64 {
	f := font(cfg.FontName, F, cfg.Bold)
	sp, _ := measure(f, " ")
	widths := make([]float64, 0, len(L.Words))
	widest := 0.0
	for _, w := range L.Words {
		ww, _ := measure(f, upper(w.W))
		widths = append(widths, float64(ww))
		widest = math.Max(widest, float64(ww))
	}
	maxW, maxH, hi := lyrFitW, lyrFitH*0.56, 2.3
	var s float64
	switch style {
	case stStack: // one word on each row
		s = math.Min(hi, math.Min(maxW/math.Max(1, widest), maxH/(float64(len(widths))*F*1.18)))
	case stFlow: // the old lines above stay: just bigger
		s = 1.45
	case stOrbit: // the arc is about as wide as the line is long: keep it inside the stage
		total := float64(sp) * float64(len(widths)-1)
		for _, w := range widths {
			total += w
		}
		return F * math.Max(0.55, math.Min(1.7, maxW*0.92/math.Max(1, total)))
	case stLED: // bigger dots; the panel scrolls a long line anyway
		s = math.Max(1, math.Min(2.2, maxW/6/F))
	case stFlap: // letter tiles, at most four rows of them
		cw := make([]float64, len(L.Words))
		for i, w := range L.Words {
			cw[i] = float64(len([]rune(upper(w.W)))) * F * 0.73
		}
		s = fitScale(cw, F*0.73, F*1.13, maxW, math.Min(maxH, 4*F*1.13*2.2), 1, 2.2)
	case stSubtitle, stHighlighter: // a single band: wider than tall
		s = fitScale(widths, float64(sp), F*1.18, maxW*0.94, maxH*0.7, 1, 1.9)
	default:
		s = fitScale(widths, float64(sp), F*1.18, maxW, maxH, 1, hi)
	}
	return F * math.Max(1, s)
}

// lyricsVer: the song version whose lyrics were last asked for.
var lyricsVer = -1

type LyricsView struct {
	sigBase   string    // the signature without the beat-driven motion
	drawnAt   time.Time // when the words were last drawn
	lines     []Line    // the song's lines (Constellation reaches back a few lines)
	w         *Widget
	version   int
	sig       string
	idx       int
	fadeFrom  time.Time
	rng       *rand.Rand
	last      [2]int
	shown     bool
	lineStart time.Time
	fitKey    string // Cinema: the line the size below was worked out for
	fitF      float64
	scatKey   string // Scatter: the line its spots were worked out for
	scatSpots []scatterSpot
	scatIdx   int
	scatBoxes [][4]float64 // where this line's words are (the next one keeps clear)
}

func newLyricsView() *LyricsView {
	return &LyricsView{w: newWidget("lyrics", "Lyrics"), version: -1, idx: -2, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
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
	if lyrFitW > 0 && L != nil && len(L.Words) > 0 && fitsStage(style) {
		F = lv.fitFont(L, style, F)
	}

	sample := Line{Text: T("Lyrics show up here"), T: 0, Sing: 1, End: 5}
	if L == nil {
		for i, w := range strings.Fields(sample.Text) {
			sample.Words = append(sample.Words, Word{w, float64(i) * 0.2})
		}
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
	stage := lyrFitW > 0 // full screen: big words, every frame of the screen counts
	steps := 16.0
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
		case stFlap:
			if p-L.T < flapSettle(ws)+0.05 {
				animKey = now.UnixMilli() / int64(1000/cfg.FPS+1)
			}
		case stLED: // the panel scrolls all the time a line is sung (30 fps is smooth enough)
			fps := cfg.FPS
			if fps > 30 {
				fps = 30
			}
			animKey = now.UnixMilli() / int64(1000/fps+1)
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
			animKey = int64(math.Min(1, math.Max(0, (p-L.T)/math.Max(0.4, L.Sing))) * 60 * (1 + b2f(stage))) // the bar moves in 60 steps (120 in full screen)
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
			lyrMotion = float32(math.Round(math.Min(1, float64(bass)*1.3)*cfg.MotionAmount*steps) / steps)
		case moDissolve:
			hz := 0.0
			if t := now.Sub(lv.lineStart).Seconds(); t < 0.5 {
				hz = 1 - t/0.5
			}
			if e := L.End - p; L.End > L.T && e < 0.55 {
				hz = math.Max(hz, 1-math.Max(0, e)/0.55)
			}
			lyrHaze = float32(math.Round(math.Min(1, hz*math.Max(0.3, cfg.MotionAmount))*steps*0.75) / (steps * 0.75))
		}
	}
	base := sig
	sig += fmt.Sprintf("|%d|%.3f|%.3f|%.2f", cfg.Motion, lyrMotion, lyrHaze, cfg.MotionAmount)
	if stage && sig != lv.sig && base == lv.sigBase && !styleDirty && !layoutDirty && now.Sub(lv.drawnAt) < 33*time.Millisecond {
		sig = lv.sig // only the beat moved: the big stage follows it at most 30 times a second
	}
	if sig != lv.sig || styleDirty || layoutDirty {
		lv.sigBase, lv.drawnAt = base, now
		lv.sig = sig
		secProf("")
		runs, bw, bh := lv.layout(style, L, idx, wi, typed, caret, prev, next, F, p, now)
		secProf("r.layout")
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
		if lyrBoxH > H { // Cinema's stage: the words spread over the whole of it
			H = lyrBoxH
		}
		// where each word goes: worked out once per line, from the words' real sizes, so that
		// no two ever overlap
		key := fmt.Sprintf("%d|%s|%.1f|%.0f|%.0f|%s|%v", idx, L.Text, F, W, H, fontName, cfg.Bold)
		if key != lv.scatKey {
			base := font(fontName, F, cfg.Bold)
			sizes := make([][2]float64, len(words))
			for i, w := range words {
				ww, hh := cachedTextPath(base, w).size(base, w)
				sizes[i] = [2]float64{float64(ww), float64(hh)}
			}
			var avoid [][4]float64
			if lv.scatIdx == idx-1 || lv.scatIdx == idx+1 { // the line before is still fading out: keep clear of it
				avoid = lv.scatBoxes
			}
			lv.scatKey, lv.scatSpots = key, scatterPlaceAvoid(sizes, int64((idx+2)*7919+len(L.Text)), W, H, avoid)
			for i := range lv.scatSpots { // steps of 5 %: the same few fonts, and never bigger than placed
				lv.scatSpots[i].Scale = math.Max(0.05, math.Floor(lv.scatSpots[i].Scale*20)/20)
			}
			lv.scatIdx, lv.scatBoxes = idx, scatterBoxes(sizes, lv.scatSpots)
		}
		spots := lv.scatSpots
		for i := range words {
			if i > wi || i >= len(spots) {
				continue
			}
			sp := spots[i]
			f := font(fontName, F*sp.Scale, cfg.Bold)
			ww, hh := cachedTextPath(f, words[i]).size(f, words[i])
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
		labK, curK, starK := 0.36, 0.72, 1.0
		if lyrBoxH > H { // Cinema's stage: the sky fills it, its words and stars a size up
			H = lyrBoxH
			labK, curK, starK = 0.5, 0.95, 1.35
		} else { // the desktop widget: a compact sky, and words and stars big enough to see
			W, H = math.Min(maxW, F*15), F*6.2
			labK, curK, starK = 0.74, 1.18, 1.7
		}
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
		lw := float32(math.Max(1, F*0.02*starK))
		lineCol := withAlpha(txt, 0.5+0.12*(starK-1))
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
		smallF := font(fontName, F*labK, cfg.Bold)
		curF := font(fontName, F*curK, true)
		// keep a label inside the sky (a star near the edge would push its words out of it)
		inX := func(x, tw float32) float32 {
			return float32(math.Max(-W/2, math.Min(W/2-float64(tw), float64(x-tw/2))))
		}
		inY := func(y float32, f *Font) float32 {
			return float32(math.Max(0, math.Min(H-float64(f.size)*1.25, float64(y))))
		}
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
				runs = append(runs, run{kind: rkStar, x: x, y: yy, size: float32(F * 0.06 * starK), shape: sh, color: txt, alpha: 0.22})
				continue
			}
			ly := yy + float32(F*0.16*starK)
			if above(st) {
				ly = yy - float32(F*0.16*starK) - float32(F*labK*1.17)
			}
			if rank[st] == cur && !done { // the words being sung: a big star with a ring, bold under it
				sc := float32(0.4 + 0.6*easeOutBack(pop))
				runs = append(runs, run{kind: rkRing, x: x, y: yy, size: float32(F * 0.3 * starK * (0.6 + 0.4*easeOut(pop))), color: acc, alpha: float32(math.Min(1, pop*2) + 0.01)},
					run{kind: rkStar, x: x, y: yy, size: float32(F * 0.24 * starK), scale: sc, shape: sh, color: txt, alpha: 1})
				tw, _ := measure(curF, label)
				if above(st) {
					ly = yy - float32(F*0.3*starK) - float32(F*curK*1.11)
				} else {
					ly = yy + float32(F*0.28*starK)
				}
				runs = append(runs, run{text: label, x: inX(x, tw), y: inY(ly, curF), f: curF, color: txt, alpha: float32(math.Max(0.05, pop)), dy: float32((1 - easeOut(pop)) * F * 0.2)})
				continue
			}
			al := 0.9
			if label == "" {
				al = 0.7 * fade
			}
			runs = append(runs, run{kind: rkStar, x: x, y: yy, size: float32(F * 0.1 * starK), shape: sh, color: txt, alpha: float32(al)})
			if label != "" {
				tw, _ := measure(smallF, label)
				runs = append(runs, run{text: label, x: inX(x, tw), y: inY(ly, smallF), f: smallF, color: withAlpha(txt, 0.85), alpha: 1})
			}
		}
		if done { // the constellation's name, small, once it is complete
			nf := font(fontName, F*0.24*math.Sqrt(starK), false)
			name := upperLang(T(sky.Name))
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

	case stFlap:
		runs, boxW, y = flapLayout(L, words, wi, p, F, maxW, txt, acc, animate)
	case stLED:
		runs, boxW, y = ledLayout(L, words, wi, p, F, maxW, txt, acc, animate)
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
	// Cinema: a word that just stands there (no turn, size change, sweep, haze or split) is drawn
	// once into a small picture with its effect, and that picture is copied every frame after —
	// filling big letters' outlines each frame is the slowest part of the lyrics stage.
	if lyrBoxH > 0 && r.rot == 0 && !r.rotC && (r.scale == 0 || r.scale == 1) && r.bg == 0 && r.chroma == 0 &&
		!(r.fill > 0 && r.fill < 1) && r.haze <= 0 && lyrHaze <= 0 && cfg.Motion != moGlow {
		if sp := wordSprite(r.f, r.text, col, effect); sp != nil {
			x, y := ox+r.x+r.dx-sp.m, oy+r.y+r.dy-sp.m
			if !lyrBase.on {
				x, y = float32(math.Round(float64(x))), float32(math.Round(float64(y)))
				setInterp(g, 5) // NearestNeighbor: a 1:1 copy
				gImage(g, sp.b.bmp, x, y, float32(sp.b.w), float32(sp.b.h))
				setInterp(g, interpBilinear)
			} else {
				gImage(g, sp.b.bmp, x, y, float32(sp.b.w), float32(sp.b.h))
			}
			return
		}
	}
	tp := cachedTextPath(r.f, r.text) // (the outline is made once; the transform puts it in place)
	path := tp.path
	x, y := ox+r.x+r.dx, oy+r.y+r.dy
	var tw, th float32
	needSize := r.rotC || (r.scale != 0 && r.scale != 1) || r.bg != 0 || r.fill > 0
	if needSize {
		tw, th = tp.size(r.f, r.text)
	}
	switch {
	case r.rotC:
		gTranslate(g, x+tw/2, y+th/2)
		gRotate(g, r.rot)
		if r.scale != 0 && r.scale != 1 {
			gScale(g, r.scale, r.scale)
		}
		gTranslate(g, -tw/2, -th/2)
	case r.rot != 0:
		gTranslate(g, x, y)
		gRotate(g, r.rot)
	case r.scale != 0 && r.scale != 1:
		gTranslate(g, x+tw/2, y+th/2)
		gScale(g, r.scale, r.scale)
		gTranslate(g, -tw/2, -th/2)
	default:
		gTranslate(g, x, y)
	}
	x, y = 0, 0
	defer gBase(g)
	size := r.f.size
	haze := lyrHaze
	if r.haze > haze {
		haze = r.haze
	}
	if haze > 0 { // the words sit in a haze that clears as they arrive (Dissolve, Scatter, Focus)
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

// word pictures for Cinema (see drawRun): made once a word has been asked for a few frames
type spriteKey struct {
	font     string // the font, its size and style (fontID)
	text     string
	col, acc uint32
	effect   int
	amt      float64
}

type sprite struct {
	hits int
	b    *Bitmap
	m    float32 // the room around the letters for the effect
}

var wordSprites = map[spriteKey]*sprite{}

func flushWordSprites() {
	for _, s := range wordSprites {
		s.b.Free()
	}
	wordSprites = map[spriteKey]*sprite{}
}

func wordSprite(f *Font, text string, col uint32, effect int) *sprite {
	if f == nil || text == "" {
		return nil
	}
	k := spriteKey{fontID(f), text, col, accent(), effect, cfg.GlowAmount}
	sp := wordSprites[k]
	if sp == nil {
		if len(wordSprites) > 360 {
			flushWordSprites()
		}
		sp = &sprite{}
		wordSprites[k] = sp
	}
	if sp.b != nil {
		return sp
	}
	if sp.hits++; sp.hits < 3 { // a word only passing through (fading, changing colour): not worth it
		return nil
	}
	tp := cachedTextPath(f, text)
	tw, th := tp.size(f, text)
	m := f.size*0.6*float32(math.Max(1, cfg.GlowAmount)) + 4
	w, h := int(math.Ceil(float64(tw+2*m))), int(math.Ceil(float64(th+2*m)))
	if w < 1 || h < 1 || w*h > 1600*900 {
		return nil
	}
	b := newBitmap(w, h)
	if b == nil {
		return nil
	}
	gClear(b.g, 0)
	gTranslate(b.g, m, m)
	drawEffect(b.g, tp.path, col, f.size, effect)
	gReset(b.g)
	gFlush(b.g)
	sp.b, sp.m = b, m
	return sp
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
		for i, wm := range []float32{1.0, 0.65, 0.35, 0.15} {
			strokePath(g, path, withAlpha(col|0xFF000000, a*[]float64{0.07, 0.12, 0.2, 0.35}[i]*math.Min(1.5, float64(amt))), size*0.5*amt*wm+1)
		}
		fillPathColor(g, path, col)
	case fxShadow: // a soft drop shadow, big enough to see (it was a 2 px black copy: lost on any dark screen)
		dx, dy := size*0.07*amt+2, size*0.10*amt+2
		gTranslate(g, dx, dy)
		for i, wm := range []float32{1.0, 0.6, 0.3} { // soft edge
			strokePath(g, path, withAlpha(0xFF000000, a*[]float64{0.18, 0.25, 0.35}[i]), size*0.16*amt*wm+1)
		}
		fillPathColor(g, path, withAlpha(0xFF000000, a*0.85))
		gTranslate(g, -dx, -dy)
		strokePath(g, path, withAlpha(0xFF000000, a*0.35), size*0.03+1) // a thin dark edge: the letters stand off
		fillPathColor(g, path, col)
	case fxOutline: // a clear outline in the accent colour (black alone vanished on dark screens), dark inside it
		w := size*0.10*amt + 2
		strokePath(g, path, withAlpha(accent()|0xFF000000, a), w)
		strokePath(g, path, withAlpha(0xFF000000, a*0.85), w*0.45)
		fillPathColor(g, path, col)
	case fxNeon:
		acc := accent()
		for i, wm := range []float32{1.0, 0.6, 0.3} {
			strokePath(g, path, withAlpha(acc, a*[]float64{0.12, 0.22, 0.45}[i]), size*0.42*amt*wm+1)
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
	secProf("")
	lv.w.clearCanvas()
	secProf("r.clear")
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
	secProf("r.runs")
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
	x, y := randomSpot(lv.rng, area, W, H+int(lv.fontSize(ui)*1.2), avoid, lv.last)
	lv.last = [2]int{x, y}
	lv.w.moveTo(x, y)
}
