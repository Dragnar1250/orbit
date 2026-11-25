//go:build windows || darwin

package main

// Arranging widgets with the mouse: shared by Windows and Mac.

import (
	"math"
)

var arrangeWidgets = []string{"vinyl", "lyrics", "halo", "clock", "weather"}

func handlePx() int { return int(22 * math.Max(1, uiScale())) }

// widgetSize: the window size a widget will have at the given scale.
func widgetSize(name string, scale float64) (int, int, bool) {
	ui := uiScale()
	switch name {
	case "vinyl":
		R := 190 * ui * scale
		W := int(R*1.08 + R*1.3)
		if cfg.ShowList || cfg.VinylLayout == 1 {
			W = int(R*1.08 + R*2.85)
		}
		return W, int(R*1.12 + R*1.18), true
	case "clock":
		W, H := clockBox(cfg.ClockStyle, cfg.ClockDate, uiScale()*scale)
		return W, H, true
	case "weather":
		W, H := weatherBox(cfg.WeatherStyle, uiScale()*scale)
		return W, H, true
	case "halo":
		S := haloSquare(scale)
		H := S
		if cfg.HaloCard > 0 { // room for the now playing card underneath
			H += int(float64(S) * 0.16)
		}
		return S, H, true
	}
	return 0, 0, false // lyrics: size follows the text, we store the centre
}

// applyLive moves a widget's centre to (cx, cy) screen pixels with the given scale.
func applyLive(name string, cx, cy, scale float64, final bool) {
	w := widgetByName(name)
	if w == nil {
		return
	}
	scale = clampScale(scale)
	var oldScale float64
	withCfg(func() {
		p := cfg.widget(name)
		oldScale = p.Scale
		p.Scale = scale
		p.Set = true
		if W, H, ok := widgetSize(name, scale); ok {
			p.X, p.Y = topLeftFor(cx, cy, W, H)
		} else {
			p.X, p.Y = int(math.Round(cx)), int(math.Round(cy))
			cfg.LyricsPlace = 1
		}
	})
	if math.Abs(oldScale-scale) < 1e-9 && w.canvas != nil {
		// pure move: move the window right away for a snappy drag
		x, y := topLeftFor(cx, cy, w.w, w.h)
		w.moveTo(x, y)
	} else {
		styleDirty = true
	}
	layoutDirty = true
	requestRedraw()
	if final {
		p := cfg.widget(name)
		logf("arrange: %s → centre %.0f,%.0f size %.0f%% (window %d,%d)", name, cx, cy, scale*100, p.X, p.Y)
		saveConfigSoon()
	}
}

func centreOf(w *Widget) (float64, float64) {
	r := w.rect()
	return float64(r.Left+r.Right) / 2, float64(r.Top+r.Bottom) / 2
}

func wheelResize(name string, delta int) {
	w := widgetByName(name)
	if w == nil {
		return
	}
	cx, cy := centreOf(w)
	applyLive(name, cx, cy, wheelScale(cfg.widget(name).Scale, delta), true)
	flash(name)
}

type dragState struct {
	w                    *Widget
	corner               int
	startX, startY       int32
	startCX, startCY     float64
	startScale, startDst float64
}

var drag *dragState

func hoverCorner(w *Widget) int {
	pt := cursorPos()
	r := w.rect()
	grip := handlePx()
	if !editMode {
		grip = int(22 * uiScale()) // the small grip drawn in the corner
	}
	return gripCorner(int(pt.X-r.Left), int(pt.Y-r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), grip, editMode)
}

func beginDrag(w *Widget) {
	pt := cursorPos()
	cx, cy := centreOf(w)
	drag = &dragState{w: w, corner: hoverCorner(w), startX: pt.X, startY: pt.Y, startCX: cx, startCY: cy,
		startScale: cfg.widget(w.name).Scale, startDst: math.Hypot(float64(pt.X)-cx, float64(pt.Y)-cy)}
	w.capture(true)
}

func dragMove(final bool) {
	if drag == nil {
		return
	}
	pt := cursorPos()
	d := drag
	if d.corner == 0 {
		applyLive(d.w.name, d.startCX+float64(pt.X-d.startX), d.startCY+float64(pt.Y-d.startY), d.startScale, final)
	} else {
		dist := math.Hypot(float64(pt.X)-d.startCX, float64(pt.Y)-d.startCY)
		applyLive(d.w.name, d.startCX, d.startCY, dragScale(d.startScale, d.startDst, dist), final)
	}
}

func endDrag() {
	if drag == nil {
		return
	}
	pt := cursorPos()
	moved := math.Abs(float64(pt.X-drag.startX))+math.Abs(float64(pt.Y-drag.startY)) > 3
	name, w := drag.w.name, drag.w
	if moved {
		dragMove(true)
	}
	drag = nil
	releaseCapture()
	if !moved {
		lastClick.up = true // a plain click (click-to-play on the record)
		return
	}
	if name == "vinyl" && cfg.VinylLayout == 1 { // the corner record snaps to the nearest corner
		cx, cy := centreOf(w)
		if vinyl != nil {
			r := w.rect()
			cx, cy = float64(r.Left)+vinyl.cx, float64(r.Top)+vinyl.cy
		}
		wa := workArea()
		c := cornerFor(cx, cy, float64(wa.Left), float64(wa.Top), float64(wa.Right), float64(wa.Bottom))
		withCfg(func() { cfg.VinylCorner = c })
		layoutDirty, styleDirty = true, true
		saveConfigSoon()
		requestRedraw()
	}
}

func widgetAt(pt POINT) string {
	for _, name := range arrangeWidgets {
		w := widgetByName(name)
		if w == nil || !w.visible || widgetLocked(name) {
			continue
		}
		r := w.rect()
		if pt.X >= r.Left && pt.X < r.Right && pt.Y >= r.Top && pt.Y < r.Bottom {
			return name
		}
	}
	return ""
}

var banner *Widget

var bannerSig string

func updateBanner() {
	if banner == nil {
		return
	}
	if !editMode {
		banner.hide()
		bannerSig = ""
		return
	}
	ui := uiScale()
	W, H := int(760*ui), int(54*ui)
	{ // wide enough for the words in any language
		aw, _ := measure(font("Segoe UI", 15*ui, true), T("Arrange mode"))
		hw, _ := measure(font("Segoe UI", 13*ui, false), T("drag to move · scroll or pull a corner to resize"))
		W = max(W, int(float64(aw+hw)+(22+18+30+116)*ui))
	}
	sig := ""
	if bannerSig == "" {
		sig = "x"
	}
	if sig == "" && banner.visible {
		return
	}
	bannerSig = "drawn"
	banner.resize(W, H)
	g := banner.g()
	gClear(g, 0)
	path := roundRectPath(1, 1, float32(W)-2, float32(H)-2, float32(H)/2)
	fillPathColor(g, path, 0xE6141419)
	strokePath(g, path, withAlpha(accent(), 0.8), 1.5)
	freePath(path)
	f := font("Segoe UI", 15*ui, true)
	fs := font("Segoe UI", 13*ui, false)
	am := T("Arrange mode")
	aw, _ := measure(f, am)
	hx := float32(22*ui) + aw + float32(18*ui)
	drawText(g, f, am, float32(22*ui), float32(H)/2-f.size*0.75, aw+float32(8*ui), 0xFFFFFFFF)
	drawText(g, fs, T("drag to move · scroll or pull a corner to resize"), hx, float32(H)/2-fs.size*0.72, float32(W)-hx-float32(116*ui), 0xC8FFFFFF)
	bw, bh := float32(96*ui), float32(H)-float32(16*ui)
	bx := float32(W) - bw - float32(10*ui)
	btn := roundRectPath(bx, float32(8*ui), bw, bh, bh/2)
	fillPathColor(g, btn, accent()|0xFF000000)
	freePath(btn)
	ft := font("Segoe UI", 14*ui, true)
	done := T("Done ✓")
	tw, _ := measure(ft, done)
	drawText(g, ft, done, bx+(bw-tw)/2, float32(8*ui)+bh/2-ft.size*0.72, bw, 0xFF16120A)
	wa := workArea()
	banner.moveTo(int(wa.Left+wa.Right)/2-W/2, int(wa.Top)+int(18*ui))
	banner.alpha = 255
	banner.present()
	// the bar sits above normal windows so "Done" can always be clicked
	banner.raiseTop()
}

// haloSquare: side of the visualizer square in pixels.
func haloSquare(scale float64) int { return int(460 * uiScale() * scale) }
