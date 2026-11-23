//go:build windows || darwin

package main

// The desktop app's own logic, the same on Windows and Mac: where widgets are, settings
// changes, the frame scheduler, which views draw. The windows themselves are platform code.

import (
	"math"
	"sync/atomic"
	"time"
)

// outAlpha = fade alpha × the widget's opacity setting.
func (w *Widget) outAlpha() byte {
	if p := cfg.Widgets[w.name]; p != nil && p.Opacity > 0 && p.Opacity < 1 && w.name != "banner" {
		return byte(float64(w.alpha) * p.Opacity)
	}
	return w.alpha
}

func setOpacity(name string, v float64) {
	withCfg(func() { cfg.widget(name).Opacity = v })
	saveConfigSoon()
	if w := widgetByName(name); w != nil && w.visible {
		w.present()
	}
}

// locked widgets ignore the mouse completely, even in arrange mode.
func widgetLocked(name string) bool {
	p := cfg.Widgets[name]
	return p != nil && p.Locked
}

// savePos stores where the user dragged the widget.
func (w *Widget) savePos() {
	r := w.rect()
	withCfg(func() {
		p := cfg.widget(w.name)
		p.X, p.Y, p.Set = int(r.Left), int(r.Top), true
		if w.name == "lyrics" {
			// lyrics change size every line, so remember their centre; dragging also pins them
			p.X, p.Y = int(r.Left+r.Right)/2, int(r.Top+r.Bottom)/2
			cfg.LyricsPlace = 1
		}
	})
	saveConfigSoon()
}

var editModeFlag atomic.Bool // readable from the settings server goroutine

func resetPositions(which string) {
	withCfg(func() {
		for _, k := range allWidgets {
			if which == "all" || which == k {
				cfg.Widgets[k] = &WidgetPos{Scale: 1}
			}
		}
		if which == "all" || which == "lyrics" {
			cfg.LyricsPlace = 0
		}
	})
	saveConfigSoon()
	layoutDirty = true
	styleDirty = true
	requestRedraw()
}

func widgetByName(name string) *Widget {
	switch {
	case name == "lyrics" && lyricView != nil:
		return lyricView.w
	case name == "vinyl" && vinyl != nil:
		return vinyl.w
	case name == "halo" && halo != nil:
		return halo.w
	case name == "clock" && clockView != nil:
		return clockView.w
	case name == "weather" && weatherView != nil:
		return weatherView.w
	case name == "banner":
		return banner
	}
	return nil
}

// widgetInfo reports where each widget is (centre in % of the work area). Called from the
// settings server goroutine: only thread-safe Win32 calls and locked config reads here.
func widgetInfo() map[string]WidgetInfo {
	wa := workArea()
	ww, wh := float64(wa.Right-wa.Left), float64(wa.Bottom-wa.Top)
	out := map[string]WidgetInfo{}
	for _, name := range allWidgets {
		info := WidgetInfo{X: 50, Y: 50, Scale: 1}
		withCfg(func() {
			if p := cfg.Widgets[name]; p != nil {
				info.Scale, info.OnTop, info.Locked, info.Opacity = p.Scale, p.OnTop, p.Locked, p.Opacity
				if info.Opacity == 0 {
					info.Opacity = 1
				}
				if name == "lyrics" && p.Set {
					info.X, info.Y = (float64(p.X)-float64(wa.Left))/ww*100, (float64(p.Y)-float64(wa.Top))/wh*100
				}
			}
		})
		if w := widgetByName(name); w != nil {
			info.Visible = w.visible
			if r := w.rect(); r.Right > r.Left && (name != "lyrics" || info.X == 50) {
				info.X = (float64(r.Left+r.Right)/2 - float64(wa.Left)) / ww * 100
				info.Y = (float64(r.Top+r.Bottom)/2 - float64(wa.Top)) / wh * 100
			}
		}
		out[name] = info
	}
	return out
}

// setWidget moves a widget's centre to x%,y% of the work area and sets its size (UI thread).
func setWidget(name string, x, y, scale float64) {
	w := widgetByName(name)
	if w == nil {
		return
	}
	wa := workArea()
	cx := float64(wa.Left) + x/100*float64(wa.Right-wa.Left)
	cy := float64(wa.Top) + y/100*float64(wa.Bottom-wa.Top)
	r := w.rect()
	withCfg(func() {
		p := cfg.widget(name)
		old := p.Scale
		if old <= 0 {
			old = 1
		}
		width := float64(r.Right-r.Left) * scale / old
		height := float64(r.Bottom-r.Top) * scale / old
		p.Scale = scale
		if name == "lyrics" {
			p.X, p.Y = int(cx), int(cy) // lyrics remember their centre
			cfg.LyricsPlace = 1
		} else {
			p.X, p.Y = int(cx-width/2), int(cy-height/2)
		}
		p.Set = true
		logf("widget %s moved to %d,%d (%.0f%%,%.0f%%) size %.0f%%", name, p.X, p.Y, x, y, scale*100)
	})
	flash(name)
	saveConfigSoon()
	layoutDirty = true
	styleDirty = true
	requestRedraw()
}

// flash outlines a widget for two seconds so you can see what just moved.
var flashUntil = map[string]time.Time{}

func flash(name string) { flashUntil[name] = time.Now().Add(2 * time.Second) }

// framed: show the dashed outline (edit mode, or just moved)
func framed(name string) bool {
	return editMode || time.Now().Before(flashUntil[name]) || (hoverName == name && name != "")
}

func requestRedraw() { needRedraw = true }

var vinyl *VinylView

var lyricView *LyricsView

var halo *HaloView

var clockView *ClockView

var weatherView *WeatherView

var glowView *GlowView

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// wantBands: does anything on the desktop move with the music's levels?
func wantBands() bool {
	if cfg.FPS <= 0 {
		return false
	}
	switch {
	case cfg.ShowHalo, cfg.ShowGlow && cfg.GlowMode == 0, cfg.ShowClock && cfg.ClockPulse:
		return true
	case cfg.ShowVinyl && (cfg.GrooveLight || cfg.DiscGlow > 0 || (cfg.AmbVinyl && cfg.AmbBright > 0)):
		return true
	case cfg.ShowLyrics && (cfg.Motion == moPulse || cfg.Motion == moStretch || cfg.Motion == moGlow):
		return true
	}
	return false
}

// uiScale: 1.0 on a 1080p screen, bigger on 1440p / 4K.
func uiScale() float64 {
	wa := workArea()
	h := float64(wa.Bottom - wa.Top)
	w := float64(wa.Right - wa.Left)
	s := math.Min(w/1920, h/1040)
	return math.Max(0.6, math.Min(3, s))
}

var (
	needRedraw  = true
	layoutDirty = true
	styleDirty  = true
	lastTrim    time.Time
)

var (
	fpsNow       int32 // read by the frame loop
	framePending int32 // 1 while a frame is waiting to be drawn
)

func applyFPS() { atomic.StoreInt32(&fpsNow, int32(cfg.FPS)) }

// desktopFPS: the chosen frame rate while something moves; when nothing does (paused, nothing
// playing, a game covering the only screen) far fewer frames — same look, a fraction of the CPU.
func desktopFPS(snap MediaSnap, now time.Time) int32 {
	fps := cfg.FPS
	if fps <= 0 {
		return 0
	}
	if now.Sub(gameCheckAt) > time.Second {
		gameCheckAt = now
		gameOn = false
		if mons, _ := monitors(); len(mons) == 1 && fullscreenOn(mons[0]) && !(cfg.ShowGlow && cfg.GlowFullscreen) {
			gameOn = true
			for _, p := range cfg.Widgets {
				if p != nil && p.OnTop {
					gameOn = false // a widget floats over the game: keep it smooth
				}
			}
		}
	}
	busy := editMode || drag != nil || hoverName != ""
	for _, t := range flashUntil {
		if now.Before(t) {
			busy = true
		}
	}
	if busy {
		return int32(fps)
	}
	if gameOn {
		return 2 // everything is behind the game: just keep the song position and lyrics in step
	}
	if snap.Playing && snap.Track != nil {
		return int32(fps)
	}
	idle := 10
	if snap.Track == nil {
		idle = 5
	}
	if (cfg.ShowGlow && cfg.GlowMode == 1) || (cfg.ShowWeather && cfg.WeatherAnimate) {
		idle = 20 // these move without music
	}
	return int32(min(fps, idle))
}

var (
	gameCheckAt time.Time
	gameOn      bool
)
