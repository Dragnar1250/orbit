package main

import (
	"math"
	"strings"
)

// Arrange-mode math (kept free of Windows calls so it can be unit-tested).

const (
	minScale = 0.3
	maxScale = 3.0
)

func clampScale(s float64) float64 {
	if math.IsNaN(s) || s <= 0 {
		return 1
	}
	return math.Max(minScale, math.Min(maxScale, s))
}

// wheelScale: one wheel notch (120) = 7 % bigger or smaller.
func wheelScale(scale float64, delta int) float64 {
	return clampScale(scale * math.Pow(1.07, float64(delta)/120))
}

// cornerAt tells whether (x, y) inside a w×h box is on a resize handle.
// 0 = no (move), 1 = top-left, 2 = top-right, 3 = bottom-left, 4 = bottom-right.
func cornerAt(x, y, w, h, handle int) int {
	left, right := x < handle, x >= w-handle
	top, bottom := y < handle, y >= h-handle
	switch {
	case top && left:
		return 1
	case top && right:
		return 2
	case bottom && left:
		return 3
	case bottom && right:
		return 4
	}
	return 0
}

// gripCorner: pointing at a widget, only the small grip at its bottom-right resizes it
// (arrange mode keeps all four corners).
func gripCorner(x, y, w, h, grip int, arrange bool) int {
	if arrange {
		return cornerAt(x, y, w, h, grip)
	}
	if x >= w-grip && y >= h-grip && x < w && y < h {
		return 4
	}
	return 0
}

// themeCycle: what a double-click on a widget switches — its colours.
func themeCycle(c *Config, name string) string {
	switch name {
	case "halo":
		c.HaloPalette = (c.HaloPalette + 1) % (len(visPalettes) - 1) // skip "Custom"
		return visPalettes[c.HaloPalette].Name
	case "clock":
		c.ClockColorMode = (c.ClockColorMode + 1) % len(clockColorNames)
		return clockColorNames[c.ClockColorMode]
	case "weather":
		c.WeatherStyle = (c.WeatherStyle + 1) % len(weatherStyleNames)
		return weatherStyleNames[c.WeatherStyle]
	}
	c.Theme = (c.Theme + 1) % (len(themes) - 1) // lyrics and the record: the colour theme (not "Custom")
	return themes[c.Theme].Name
}

// dragScale: dragging a corner away from / toward the centre grows / shrinks the widget.
func dragScale(startScale, startDist, curDist float64) float64 {
	if startDist < 8 {
		startDist = 8
	}
	v := startScale * curDist / startDist
	if math.IsNaN(v) {
		return clampScale(startScale)
	}
	return math.Max(minScale, math.Min(maxScale, v)) // pulled all the way in = smallest, not reset
}

// topLeftFor returns the top-left corner that puts a w×h box centred on (cx, cy).
func topLeftFor(cx, cy float64, w, h int) (int, int) {
	return int(math.Round(cx - float64(w)/2)), int(math.Round(cy - float64(h)/2))
}

// isDesktopClass: window classes that make up the bare desktop (icons / wallpaper).
func isDesktopClass(c string) bool {
	switch c {
	case "Progman", "WorkerW", "SHELLDLL_DefView", "SysListView32":
		return true
	}
	return false
}

// ambLevel: brightness (0..1) of the ambient light for an effect at time t (seconds), with the bass.
func ambLevel(effect int, t float64, bass float64) float64 {
	switch effect {
	case 1: // breathe: one slow breath every 4.5 s
		return 0.62 + 0.38*math.Sin(t*2*math.Pi/4.5)
	case 2: // beat
		return 0.4 + 0.6*math.Max(0, math.Min(1, bass*1.3))
	case 3: // flicker: like a candle, never dark
		f := 0.5*math.Sin(t*7.3) + 0.3*math.Sin(t*13.1+1) + 0.2*math.Sin(t*23.7+2)
		return 0.78 + 0.2*f
	}
	return 1 // still, drift (drift moves the colour, not the brightness)
}

// cornerFor: which screen corner (0 bottom-left, 1 bottom-right, 2 top-left, 3 top-right) a point is nearest.
func cornerFor(x, y, left, top, right, bottom float64) int {
	c := 0
	if x > (left+right)/2 {
		c |= 1
	}
	if y < (top+bottom)/2 {
		c |= 2
	}
	return c
}

// toolbarLayout: the hover buttons, centred over the visible part (vl..vr) of a widget, at the top.
// Returns the left edge of each button; s is the button size.
func toolbarLayout(n int, vl, vr, s, gap float64) []float64 {
	total := float64(n)*s + float64(n-1)*gap
	x := vr - 8 - total // top-right corner of the visible part
	if x < vl+2 {
		x = vl + 2
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = x + float64(i)*(s+gap)
	}
	return out
}

type tbButton struct {
	id, glyph, plain, label string
}

var (
	btnNext     = tbButton{"next", "\uE8B1", "↻", "Next look"}
	btnThemes   = tbButton{"themes", "\uE790", "◐", "Themes"}
	btnColors   = tbButton{"colors", "\uE7E6", "●", "Colors"}
	btnSettings = tbButton{"settings", "\uE713", "⚙", "Settings"}
	btnHide     = tbButton{"hide", "\uE711", "✕", "Hide it"}
)

// toolbarFor: the boxes on top of each widget.
func toolbarFor(name string) []tbButton {
	switch name {
	case "lyrics":
		return []tbButton{btnSettings, btnHide}
	case "vinyl":
		return []tbButton{btnNext, btnThemes, btnColors, btnSettings, btnHide}
	case "halo":
		c := btnColors
		c.label = "Color"
		return []tbButton{btnNext, btnThemes, c, btnSettings, btnHide}
	}
	return []tbButton{btnNext, btnSettings, btnHide}
}

// colour presets the Colors box steps through
var colorPresets = []string{"#FF4D6D", "#FF8A3D", "#FFD23F", "#3DDC97", "#3DB4FF", "#7B5CFF", "#E15BFF", "#FFFFFF"}

func nextPreset(cur string) string {
	for i, c := range colorPresets {
		if strings.EqualFold(c, cur) {
			return colorPresets[(i+1)%len(colorPresets)]
		}
	}
	return colorPresets[0]
}

// scratchDelta: the turn from angle a to b (degrees), the short way round.
func scratchDelta(a, b float64) float64 {
	d := math.Mod(b-a+540, 360) - 180
	if d == -180 {
		d = 180
	}
	return d
}

// scratchPos: the song position after turning the record by deg degrees (positive = the way
// it spins), one full turn = perTurn seconds; kept inside the song.
func scratchPos(pos, deg, perTurn, dur float64) float64 {
	pos += deg / 360 * perTurn
	if pos < 0 {
		pos = 0
	}
	if dur > 0 && pos > dur-0.5 {
		pos = math.Max(0, dur-0.5)
	}
	return pos
}
