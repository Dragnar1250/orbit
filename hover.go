//go:build windows || darwin

package main

// Hover mode: point at a widget on the desktop and it can be moved / resized straight away (no
// double-click), with a small bar of buttons on top — like Weyv.

import (
	"math"
	"time"
)

var (
	hoverName string // the widget under the mouse that is "awake" right now
	hoverBtn  = -1   // the toolbar button under the mouse
)

// setHover wakes a widget up (it takes the mouse) and sends the previous one back to sleep.
func setHover(name string) {
	if name == hoverName {
		return
	}
	if old := widgetByName(hoverName); old != nil && !editMode {
		old.setClickThrough(true)
	}
	hoverName, hoverBtn = name, -1
	if w := widgetByName(name); w != nil && !editMode {
		w.setClickThrough(false)
	}
	styleDirty = true
	requestRedraw()
}

// hoverCheck runs every frame: the mouse may have left without a move event reaching us
// (a window popped up, the lyrics moved to a new spot, hover was switched off …).
func hoverCheck() {
	if hoverName == "" || drag != nil {
		return
	}
	if !cfg.HoverEdit || editMode || widgetLocked(hoverName) {
		setHover("")
		return
	}
	pt := cursorPos()
	if widgetAt(pt) != hoverName || !desktopAt(pt) {
		setHover("")
	}
}

// hoverMove is called by the mouse hook for every move (UI thread).
func hoverMove(pt POINT) {
	if !cfg.HoverEdit || editMode || drag != nil || fs != nil {
		return
	}
	name := widgetAt(pt)
	if name != "" && !desktopAt(pt) {
		name = "" // something else is in front of the widget
	}
	if name != hoverName {
		setHover(name)
		return
	}
	if name != "" {
		if b := toolbarHit(widgetByName(name), pt); b != hoverBtn {
			hoverBtn = b
			styleDirty = true
			requestRedraw()
		}
	}
}

/* ---------- the button bar ---------- */

func iconFont() string {
	for _, f := range []string{"Segoe Fluent Icons", "Segoe MDL2 Assets"} {
		for _, have := range fontList {
			if have == f {
				return f
			}
		}
	}
	return ""
}

// toolbarRects: the boxes in the widget's own pixels, centred over the part that is on screen.
func toolbarRects(w *Widget) []RectF {
	if w == nil {
		return nil
	}
	n := len(toolbarFor(w.name))
	ui := float32(uiScale())
	s, gap := 22*ui, 4*ui
	wr, wa := w.rect(), workArea()
	vl := math.Max(0, float64(wa.Left-wr.Left))
	vr := math.Min(float64(w.w), float64(wa.Right-wr.Left))
	top := float32(math.Max(0, float64(wa.Top-wr.Top))) + 8*ui
	xs := toolbarLayout(n, vl, vr, float64(s), float64(gap))
	out := make([]RectF, len(xs))
	for i, x := range xs {
		out[i] = RectF{float32(x), top, s, s}
	}
	return out
}

// toolbarHit: which box is at a screen point (-1: none).
func toolbarHit(w *Widget, pt POINT) int {
	if w == nil || w.name != hoverName {
		return -1
	}
	r := w.rect()
	x, y := float32(pt.X-r.Left), float32(pt.Y-r.Top)
	for i, b := range toolbarRects(w) {
		if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
			return i
		}
	}
	return -1
}

// drawHover: a light outline with corner marks and the boxes (instead of the arrange frame).
func drawHover(g uintptr, w, h float32, name string) {
	o := roundRectPath(2, 2, w-4, h-4, 12)
	// an invisible veil, no outline: Windows lets clicks fall through fully transparent pixels,
	// and the whole box should be grabbable, not just the drawn parts
	fillPathColor(g, o, 0x03000000)
	freePath(o)
	ui := float32(uiScale())
	// one small grip in the bottom-right corner: drag it to resize
	gx, gy := w-5*ui, h-5*ui
	for i := float32(1); i <= 3; i++ {
		d := i * 4.5 * ui
		line(g, 0x50000000, 2.6*ui, gx-d, gy, gx, gy-d)
		line(g, 0xB0FFFFFF, 1.4*ui, gx-d, gy, gx, gy-d)
	}
	btns := toolbarFor(name)
	rects := toolbarRects(widgetByName(name))
	icf := iconFont()
	for i, b := range rects {
		if i >= len(btns) {
			break
		}
		bg := uint32(0xD81C1C22)
		if i == hoverBtn {
			bg = 0xF0393946
		}
		p := roundRectPath(b.X, b.Y, b.W, b.H, b.W*0.3)
		fillPathColor(g, p, bg)
		strokePath(g, p, 0x30FFFFFF, 1)
		freePath(p)
		var f *Font
		txt := btns[i].plain
		if icf != "" {
			f, txt = font(icf, float64(b.W*0.44), false), btns[i].glyph
		} else {
			f = font("Segoe UI Symbol", float64(b.W*0.46), false)
		}
		tw, th := measure(f, txt)
		drawText(g, f, txt, b.X+(b.W-tw)/2, b.Y+(b.H-th)/2, b.W, 0xFFFFFFFF)
	}
	if hoverNote != "" && time.Now().Before(hoverNoteUntil) && len(rects) > 0 && hoverBtn < 0 { // what a double-click just changed
		f := font("Segoe UI", float64(11*ui), true)
		tw, th := measure(f, hoverNote)
		last := rects[len(rects)-1]
		x := last.X + last.W - tw - 14*ui
		if x < 2 {
			x = 2
		}
		y := last.Y + last.H + 5*ui
		p := roundRectPath(x, y, tw+14*ui, th+6*ui, 6*ui)
		fillPathColor(g, p, 0xE6101014)
		freePath(p)
		drawText(g, f, hoverNote, x+7*ui, y+3*ui, tw+4, 0xFFFFFFFF)
	}
	if hoverBtn >= 0 && hoverBtn < len(rects) && hoverBtn < len(btns) { // the name of the box under the mouse
		b := rects[hoverBtn]
		f := font("Segoe UI", float64(11*ui), true)
		lbl := btns[hoverBtn].label
		tw, th := measure(f, lbl)
		x := b.X + b.W/2 - tw/2 - 7*ui
		if x+tw+14*ui > w-2 { // the boxes sit at the right edge: keep the name inside
			x = w - 2 - tw - 14*ui
		}
		if x < 2 {
			x = 2
		}
		y := b.Y + b.H + 5*ui
		p := roundRectPath(x, y, tw+14*ui, th+6*ui, 6*ui)
		fillPathColor(g, p, 0xE6101014)
		freePath(p)
		drawText(g, f, lbl, x+7*ui, y+3*ui, tw+4, 0xFFFFFFFF)
	}
}

// toolbarAction runs a box.
func toolbarAction(name string, i int) {
	btns := toolbarFor(name)
	if i < 0 || i >= len(btns) {
		return
	}
	switch btns[i].id {
	case "next": // next look
		withCfg(func() {
			switch name {
			case "lyrics":
				cfg.LyricsStyle = (cfg.LyricsStyle + 1) % len(styleNames)
			case "vinyl":
				cfg.DiscStyle = (cfg.DiscStyle + 1) % len(discNames)
			case "halo":
				cfg.HaloStyle = (cfg.HaloStyle + 1) % len(visNames)
			case "clock":
				cfg.ClockStyle = (cfg.ClockStyle + 1) % len(clockStyleNames)
			case "weather":
				cfg.WeatherStyle = (cfg.WeatherStyle + 1) % len(weatherStyleNames)
			}
		})
		layoutDirty = true
	case "themes":
		withCfg(func() {
			if name == "halo" { // the visualizer's colour gradients
				cfg.HaloPalette = (cfg.HaloPalette + 1) % (len(visPalettes) - 1) // skip "Custom"
			} else { // the colour theme (record rings, card, lyrics)
				cfg.Theme = (cfg.Theme + 1) % (len(themes) - 1)
			}
		})
	case "colors":
		withCfg(func() {
			if name == "vinyl" { // the record's colour; show it on a record that has one
				cfg.DiscColorMode = 2
				cfg.DiscColor = nextPreset(cfg.DiscColor)
				if cfg.DiscStyle == discPicture || cfg.DiscStyle == discBlack || cfg.DiscStyle == discGold {
					cfg.DiscStyle = discColoured
				}
			} else { // the visualizer: the ambient light behind it
				cfg.AmbHalo = true
				if cfg.AmbColorMode != 2 {
					cfg.AmbColorMode, cfg.AmbColor = 2, colorPresets[0]
				} else {
					cfg.AmbColor = nextPreset(cfg.AmbColor)
				}
			}
		})
	case "settings":
		openSettingsAt(name)
	case "hide":
		withCfg(func() {
			switch name {
			case "lyrics":
				cfg.ShowLyrics = false
			case "vinyl":
				cfg.ShowVinyl = false
			case "halo":
				cfg.ShowHalo = false
			case "clock":
				cfg.ShowClock = false
			case "weather":
				cfg.ShowWeather = false
			}
		})
		setHover("")
		trayBalloon(map[string]string{"lyrics": "Lyrics", "vinyl": "Vinyl", "halo": "Visualizer", "clock": "Clock", "weather": "Weather"}[name]+" hidden",
			"Bring it back from the tray menu or the Widgets page in the settings.")
	}
	saveConfigSoon()
	styleDirty = true
	requestRedraw()
}

var (
	hoverNote      string // a short note under the boxes ("Theme · Sunset")
	hoverNoteUntil time.Time
)

// doubleClickTheme: a double-click on a widget switches its colours (settings has its own box).
func doubleClickTheme(name string) {
	var got string
	withCfg(func() { got = themeCycle(cfg, name) })
	label := map[string]string{"halo": "Colours", "clock": "Colour", "weather": "Look"}[name]
	if label == "" {
		label = "Theme"
	}
	hoverNote, hoverNoteUntil = label+" · "+got, time.Now().Add(1500*time.Millisecond)
	saveConfigSoon()
	styleDirty = true
	requestRedraw()
}
