//go:build darwin

package main

// Orbit for macOS: the same widgets, settings and arranging as on Windows (the drawing and the
// app logic are shared); this file is the Mac's window and menu side.

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework WebKit -framework CoreAudio -framework AudioToolbox -framework IOSurface -framework CoreVideo
#include <stdlib.h>
#include "gdip_darwin.h"
#include "app_darwin.h"
*/
import "C"

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
)

func init() { runtime.LockOSThread() } // Cocoa wants the main thread

/* ---------- widgets ---------- */

type Widget struct {
	name         string
	label        string
	win          uintptr // the NSPanel
	id           int
	canvas       *Canvas
	w, h         int
	x, y         int // where it is (pixels, from the top-left of the main screen)
	alpha        byte
	visible      bool
	movable      bool
	onWheel      func(delta int)
	clickThrough bool
	dragging     bool
	offscreen    bool
	ox, oy       int
	// the soft layer under the main picture (see softBegin) and the turning label
	softK     float64 // its size per point (0: none)
	softOnly  bool    // everything is drawn on the soft layer (the screen glow)
	soft      *Canvas
	softUsed  bool
	softShown bool
	softBlurR float32
	baseVer   int
	spinShown bool
}

// canvasOwner: which widget a canvas belongs to (softBegin gets only the canvas)
var canvasOwner = map[uintptr]*Widget{}

var (
	widgets  = map[int]*Widget{}
	widgetID int
	editMode bool
)

func newWidget(name, label string) *Widget {
	widgetID++
	w := &Widget{name: name, label: label, alpha: 255, movable: true, id: widgetID, clickThrough: true}
	w.win = uintptr(C.ow_new(C.int(w.id)))
	widgets[w.id] = w
	return w
}

func (w *Widget) resize(width, height int) {
	width, height = max(width, 1), max(height, 1)
	if w.canvas != nil && w.w == width && w.h == height {
		return
	}
	if w.canvas != nil {
		delete(canvasOwner, w.canvas.g)
	}
	w.canvas.Free()
	if w.softOnly { // nothing is drawn on the main canvas: keep it tiny
		w.canvas = newCanvas(1, 1)
	} else {
		w.canvas = newCanvas(width, height)
	}
	w.w, w.h = width, height
	if w.canvas != nil {
		canvasOwner[w.canvas.g] = w
	}
	w.soft.Free()
	w.soft = nil
}

// useSoft: this widget draws its glows and light on a soft layer of k times its size per point
// (0.5: half the points — a quarter of the pixels of a Retina screen, 1: the points).
func (w *Widget) useSoft(k float64, only bool) {
	w.softK = k
	w.softOnly = only
}

// softBegin: until softEnd, what is drawn on g goes to the soft layer instead (same place,
// transform and clip, far fewer pixels). False (and nothing changes) where there is none.
var redirFrom, redirTo uintptr

func softBegin(g uintptr) bool {
	w := canvasOwner[g]
	if w == nil || w.softK <= 0 || redirFrom != 0 {
		return false
	}
	k := math.Min(1, w.softK/float64(C.os_scale())) // per pixel of this screen
	sw, sh := int(math.Ceil(float64(w.w)*k)), int(math.Ceil(float64(w.h)*k))
	if w.soft == nil || w.soft.w != sw || w.soft.h != sh {
		w.soft.Free()
		w.soft = newCanvas(sw, sh)
		if w.soft == nil {
			return false
		}
	}
	C.og_soft_begin(og(w.soft.g), og(g), C.float(float64(sw)/float64(w.w)))
	w.softUsed = true
	redirFrom, redirTo = g, w.soft.g
	return true
}

func softEnd(g uintptr) {
	if redirFrom == g {
		redirFrom, redirTo = 0, 0
	}
}

// softBlur: blur this frame's soft layer by r canvas pixels when it is shown
func softBlur(g uintptr, r float32) {
	if w := canvasOwner[g]; w != nil && w.soft != nil && r > w.softBlurR {
		w.softBlurR = r
	}
}

// softClear: an empty soft layer (widgets that don't clear their canvas every frame)
func softClear(g uintptr) {
	if w := canvasOwner[g]; w != nil && w.soft != nil {
		C.og_clip_reset(og(w.soft.g))
		C.og_clear(og(w.soft.g), 0)
		w.softUsed = false
	}
}

// gpuLayers: the Mac turns the record's label and keeps its still picture on the GPU
func (w *Widget) gpuLayers() bool { return !w.offscreen }

// setBase: the widget's still picture (under the label and the main picture), sent when ver changes
func (w *Widget) setBase(b *Bitmap, ver int) {
	if b == nil {
		if w.baseVer != 0 {
			C.ow_layer(unsafe.Pointer(w.win), 1, nil, 1)
			w.baseVer = 0
		}
		return
	}
	if ver != w.baseVer {
		w.baseVer = ver
		C.ow_layer(unsafe.Pointer(w.win), 1, og(b.g), 1)
	}
}

// spin: picture b turned by deg around (cx, cy), by the GPU (b is sent again when ver changes)
func (w *Widget) spin(b *Bitmap, ver int, cx, cy, deg float64) {
	if b == nil {
		if w.spinShown {
			C.ow_spin(unsafe.Pointer(w.win), nil, 0, 0, 0, 0)
			w.spinShown = false
		}
		return
	}
	w.spinShown = true
	C.ow_spin(unsafe.Pointer(w.win), og(b.g), C.int(ver), C.double(cx), C.double(cy), C.double(deg))
}

func (w *Widget) g() uintptr {
	if w.canvas == nil {
		return 0
	}
	return w.canvas.g
}

func (w *Widget) level() int {
	switch {
	case w.name == "glow" || w.name == "banner":
		return 2
	case cfg.Widgets[w.name] != nil && cfg.Widgets[w.name].OnTop:
		return 1
	}
	return 0
}

func (w *Widget) present() {
	if w.offscreen {
		w.visible = w.canvas != nil
		return
	}
	if w.canvas == nil {
		return
	}
	C.ow_level(unsafe.Pointer(w.win), C.int(w.level()))
	if w.softOnly {
		C.ow_show(unsafe.Pointer(w.win), nil, C.double(w.x), C.double(w.y), C.double(w.w), C.double(w.h), C.double(float64(w.outAlpha())/255))
	} else {
		C.ow_show(unsafe.Pointer(w.win), og(w.canvas.g), C.double(w.x), C.double(w.y), C.double(w.w), C.double(w.h), C.double(float64(w.outAlpha())/255))
	}
	if w.soft != nil && w.softUsed {
		if w.softBlurR > 0 {
			C.og_blur(og(w.soft.g), C.float(float64(w.softBlurR)*float64(w.soft.w)/float64(w.w)))
		}
		C.ow_layer(unsafe.Pointer(w.win), 0, og(w.soft.g), C.double(float64(w.soft.w)/float64(w.w)))
		w.softShown = true
	} else if w.softShown {
		C.ow_layer(unsafe.Pointer(w.win), 0, nil, 1)
		w.softShown = false
	}
	w.softBlurR = 0
	w.visible = true
}

func (w *Widget) presentDirty(r RECT) { w.present() }

func (w *Widget) hide() {
	if w.offscreen {
		w.visible = false
		return
	}
	if w.visible {
		C.ow_hide(unsafe.Pointer(w.win)) // (its pictures go with it)
		w.visible = false
		w.softShown, w.spinShown, w.baseVer = false, false, 0
	}
}

func (w *Widget) release() {
	w.hide()
	if w.canvas != nil {
		delete(canvasOwner, w.canvas.g)
		w.canvas.Free()
		w.canvas = nil
		w.w, w.h = 0, 0
	}
	w.soft.Free()
	w.soft = nil
	w.softShown, w.spinShown, w.baseVer = false, false, 0
}

func (w *Widget) moveTo(x, y int) {
	if w.offscreen {
		w.ox, w.oy = x, y
		return
	}
	if w.dragging {
		return
	}
	w.x, w.y = x, y
	if w.visible {
		C.ow_move(unsafe.Pointer(w.win), C.double(x), C.double(y))
	}
}

func (w *Widget) rect() RECT {
	if w.offscreen {
		return RECT{int32(w.ox), int32(w.oy), int32(w.ox + w.w), int32(w.oy + w.h)}
	}
	return RECT{int32(w.x), int32(w.y), int32(w.x + w.w), int32(w.y + w.h)}
}

func (w *Widget) setClickThrough(on bool) {
	if w.offscreen || w.clickThrough == on {
		return
	}
	C.ow_mouse(unsafe.Pointer(w.win), C.int(b2i(!on)))
	w.clickThrough = on
}

func (w *Widget) capture(on bool) {}
func releaseCapture()             {}
func (w *Widget) raiseTop()       { C.ow_level(unsafe.Pointer(w.win), 2) }

// dragging moves the window directly (the drag code sets x/y while w.dragging is on)
func dragFollow(w *Widget, x, y int) {
	w.x, w.y = x, y
	C.ow_move(unsafe.Pointer(w.win), C.double(x), C.double(y))
}

/* ---------- screens, mouse ---------- */

func workArea() RECT {
	var r [4]C.double
	C.os_work_area(&r[0])
	if r[2] <= 0 {
		return RECT{0, 0, 1920, 1080}
	}
	return RECT{int32(r[0]), int32(r[1]), int32(r[0] + r[2]), int32(r[1] + r[3])}
}

func monitors() ([]RECT, []bool) {
	var r [64]C.double
	n := int(C.os_screens(&r[0], 16))
	var out []RECT
	var primary []bool
	for i := 0; i < n; i++ {
		out = append(out, RECT{int32(r[4*i]), int32(r[4*i+1]), int32(r[4*i] + r[4*i+2]), int32(r[4*i+1] + r[4*i+3])})
		primary = append(primary, i == 0)
	}
	return out, primary
}

func fullscreenOn(m RECT) bool { return false } // (a full-screen app has its own Space on the Mac)

func cursorPos() POINT {
	var x, y C.double
	C.os_cursor(&x, &y)
	return POINT{int32(x), int32(y)}
}

func desktopAt(pt POINT) bool { return C.os_desktop_at(C.double(pt.X), C.double(pt.Y)) != 0 }

var lastClick struct {
	t    time.Time
	pt   POINT
	name string
	id   int
	up   bool
}

func localDate(t time.Time, picture string) string { return "" }

func formatDate(t time.Time, long bool) string {
	var buf [128]C.char
	if n := C.os_format_date(C.longlong(t.Unix()), C.int(b2i(long)), &buf[0], 128); n > 0 {
		return C.GoString(&buf[0])
	}
	return englishDate(t, long)
}

func trayBalloon(title, text string) { logf("%s: %s", title, text) }

/* ---------- start ---------- */

var macLastPt POINT

func main() {
	logf("Orbit %s starting (macOS)", appVersion)
	debug.SetGCPercent(60)
	debug.SetMemoryLimit(160 << 20)
	if f, err := os.OpenFile(filepath.Join(appDir(), "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(f, "=== %s  Orbit %s started ===\n", time.Now().Format("2006-01-02 15:04:05"), appVersion)
		debug.SetCrashOutput(f, debug.CrashOptions{})
		f.Close()
	}
	if n := C.os_take_over(); n > 0 {
		logf("closed %d other Orbit(s) — only one runs at a time", int(n))
	}
	loadConfig()
	media.SetOffset(cfg.OffsetSec)
	icon := C.CString(menuIconPath())
	C.os_run(icon) // → goStarted, then runs until Quit
	C.free(unsafe.Pointer(icon))
	saveConfigIfDue(true)
	logf("exit")
}

//export goStarted
func goStarted() {
	fontList = macFonts()
	hooks.Fonts = func() []string { return fontList }
	hooks.Quit = func() { saveConfigIfDue(true); C.os_quit() }
	hooks.OpenFolder = func() { exec.Command("open", appDir()).Start() }
	hooks.StartupEnabled = loginItemEnabled
	hooks.SetStartup = func(on bool) { setLoginItem(on) }
	hooks.SetEditMode = func(on bool) { runOnUI(func() { setEditMode(on) }) }
	hooks.EditMode = editModeFlag.Load
	hooks.SettingsChanged = settingsChanged
	hooks.ResetPositions = resetPositions
	hooks.WidgetInfo = widgetInfo
	hooks.SetWidget = setWidget
	hooks.SetWidgetFlag = setWidgetFlag
	hooks.Flash = func(name string) { flash(name); requestRedraw() }
	hooks.SetOpacity = setOpacity
	hooks.FormatDate = formatDate
	hooks.OpenSettings = openSettings
	hooks.Fullscreen = func(open bool) { setFullscreen(open) }
	if srv, err := startSettingsServer(""); err == nil {
		settingsSrv = srv
		logf("settings window on 127.0.0.1:%d", srv.port)
	} else {
		logf("settings server failed: %v", err)
	}
	banner = newWidget("banner", "Arrange")
	banner.movable = false
	halo = newHaloView()
	vinyl = newVinylView()
	lyricView = newLyricsView()
	clockView = newClockView()
	weatherView = newWeatherView()
	glowView = newGlowView()
	if os.Getenv("ORBIT_FAKE_PLAY") != "" {
		go fakePlay() // the build machine's test: a song "plays" without Spotify
	} else {
		go macMediaLoop()
		go audioLoop()
	}
	go weatherLoop(func() (string, float64, float64, bool, bool) {
		var place string
		var lat, lon float64
		var f, want bool
		withCfg(func() {
			place, lat, lon, f, want = cfg.WeatherCity, cfg.WeatherLat, cfg.WeatherLon, cfg.WeatherF, cfg.ShowWeather
		})
		return place, lat, lon, f, want
	}, func() { runOnUI(func() { styleDirty = true; requestRedraw() }) })
	if _, err := os.Stat(filepath.Join(appDir(), "config.json")); err != nil {
		saveConfigSoon()
		if os.Getenv("ORBIT_NO_SETTINGS") == "" { // (the build machine's test opens it itself)
			go func() { time.Sleep(time.Second); runOnUI(openSettings) }()
		}
	}
	applyFPS()
	buildMenu()
}

/* ---------- the frame ---------- */

//export goTick
func goTick() {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC while drawing: %v\n%s", r, debug.Stack())
		}
	}()
	drainUI()
	trimCaches()
	now := time.Now()
	snap := media.Snapshot()
	// the mouse: hover toolbar and dragging (the Mac has no global hook; the pointer is read here)
	pt := cursorPos()
	if drag != nil {
		dragMove(false)
		if C.os_mouse_down() == 0 {
			endDrag()
		}
	} else if pt != macLastPt {
		hoverMove(pt)
	}
	macLastPt = pt
	hoverCheck()
	bands, bass, active := spectrum.Compute()
	if fs != nil { // full screen covers the screen: only it is drawn
		atomic.StoreInt32(&bandsWanted, 1)
		perf.tick(now)
		t := time.Now()
		fs.Update(now, snap, bands, bass)
		perf.add(6, t)
		perf.report(now, true)
		needRedraw, layoutDirty, styleDirty = false, false, false
		saveConfigIfDue(false)
		C.os_set_rate(C.double(max(5, atomic.LoadInt32(&fpsNow))))
		return
	}
	if snap.Playing {
		prefetchHiRes(snap.Track)
	}
	ui := uiScale()
	perf.tick(now)
	t := time.Now()
	vinyl.Update(now, snap, bands, bass, ui)
	t = perf.add(0, t)
	halo.Update(now, snap, bands, bass, active, ui)
	t = perf.add(1, t)
	lyricView.Update(now, snap, bass, ui)
	t = perf.add(2, t)
	clockView.Update(now, snap, bass, ui)
	t = perf.add(3, t)
	weatherView.Update(now, ui)
	t = perf.add(4, t)
	glowView.Update(now, snap, bands, bass, ui)
	perf.add(5, t)
	perf.report(now, snap.Playing)
	updateBanner()
	if layoutDirty {
		logWidgets()
	}
	needRedraw, layoutDirty, styleDirty = false, false, false
	saveConfigIfDue(false)
	if hoverNote != "" && now.After(hoverNoteUntil) {
		hoverNote = ""
		styleDirty = true
		requestRedraw()
	}
	if snap.Version != macTipVer {
		macTipVer = snap.Version
		tip := "Orbit — nothing playing"
		if snap.Track != nil {
			tip = snap.Track.Title + " — " + snap.Track.Artist
		}
		s := C.CString(tip)
		C.os_set_tooltip(s)
		C.free(unsafe.Pointer(s))
	}
	atomic.StoreInt32(&bandsWanted, b2i(wantBands() && snap.Playing))
	fps := desktopFPS(snap, now)
	if fps <= 0 {
		fps = 5
	}
	C.os_set_rate(C.double(fps))
	if now.Sub(lastTrim) > 2*time.Minute {
		lastTrim = now
		runtime.GC()
		debug.FreeOSMemory()
	}
}

var macTipVer = -1

//export goScreensChanged
func goScreensChanged() { layoutDirty, styleDirty = true, true; requestRedraw() }

//export goWidgetMouse
func goWidgetMouse(id C.int, kind C.int, delta C.int) {
	w := widgets[int(id)]
	if w == nil {
		return
	}
	if w.name == "banner" {
		if kind == 3 && editMode {
			setEditMode(false)
		}
		return
	}
	switch kind {
	case 1, 6: // down (6: double)
		if kind == 6 && !editMode && cfg.HoverEdit && w.name == hoverName {
			doubleClickTheme(w.name)
			return
		}
		if !editMode && w.name == hoverName {
			if b := toolbarHit(w, cursorPos()); b >= 0 {
				toolbarAction(w.name, b)
				return
			}
		}
		if (editMode || w.name == hoverName) && w.movable {
			beginDrag(w)
		}
	case 2:
		if drag != nil && drag.w == w {
			dragMove(false)
		}
	case 3:
		if drag != nil && drag.w == w {
			endDrag()
		}
	case 4:
		wheelResize(w.name, int(delta))
	}
	requestRedraw()
}

/* ---------- settings ---------- */

func setEditMode(on bool) {
	if drag != nil {
		endDrag()
	}
	editMode = on
	editModeFlag.Store(on)
	hoverName, hoverBtn = "", -1
	for _, w := range widgets {
		if w.name == "glow" {
			continue
		}
		w.setClickThrough(!on || widgetLocked(w.name))
	}
	logf("arrange mode %v", on)
	bannerSig = ""
	styleDirty = true
	requestRedraw()
	buildMenu()
}

func setWidgetFlag(name, flag string, on bool) {
	withCfg(func() {
		p := cfg.widget(name)
		if flag == "onTop" {
			p.OnTop = on
		} else {
			p.Locked = on
		}
	})
	saveConfigSoon()
	if w := widgetByName(name); w != nil {
		if flag != "onTop" {
			if drag != nil && on {
				endDrag()
			}
			w.setClickThrough(!editMode || on)
		}
		w.present()
	}
	flash(name)
	styleDirty = true
	requestRedraw()
}

func settingsChanged(key string) {
	switch key {
	case "FPS":
		applyFPS()
	case "OffsetSec":
		media.SetOffset(cfg.OffsetSec)
	case "WeatherF", "ShowWeather", "WeatherCity":
		kickWeather()
	case "VinylCorner", "TextSize", "ShowList", "LyricsPlace":
		layoutDirty = true
	}
	saveConfigSoon()
	styleDirty = true
	requestRedraw()
	buildMenu()
}

func openSettings() { openSettingsAt("") }

func openSettingsAt(page string) {
	if settingsSrv == nil {
		return
	}
	u := settingsSrv.URL()
	if page != "" {
		u += "#" + page
	}
	s := C.CString(u)
	C.os_open_settings(s) // Orbit's own window, not a browser
	C.free(unsafe.Pointer(s))
}

// goGlobalMouse: clicks and scrolls anywhere (the desktop gets them through the widgets). The
// same rules as the Windows mouse hook: click the record = play / pause, double-click = colours
// (or arrange), scroll over a widget = resize.
//
//export goGlobalMouse
func goGlobalMouse(kind, x, y, delta, clicks C.int) {
	pt := POINT{int32(x), int32(y)}
	if drag != nil || editMode {
		return
	}
	switch kind {
	case 4:
		if name := widgetAt(pt); name != "" && desktopAt(pt) {
			wheelResize(name, int(delta))
		}
	case 1:
		name := widgetAt(pt)
		if name == "" || !desktopAt(pt) {
			lastClick.name = ""
			return
		}
		if clicks >= 2 && lastClick.name == name {
			lastClick.name = ""
			if cfg.HoverEdit {
				doubleClickTheme(name)
			} else {
				setEditMode(true)
				flash(name)
			}
			return
		}
		lastClick.t, lastClick.pt, lastClick.name = time.Now(), pt, name
		lastClick.id++
		lastClick.up = false
		if name == "vinyl" && cfg.ClickPlay {
			id := lastClick.id
			dbl := time.Duration(float64(C.os_double_click_time()) * float64(time.Second))
			time.AfterFunc(dbl+40*time.Millisecond, func() {
				runOnUI(func() {
					if lastClick.id == id && lastClick.name == "vinyl" && lastClick.up && !editMode {
						mediaPlayPause()
					}
				})
			})
		}
	case 3:
		if lastClick.name != "" && math.Abs(float64(pt.X-lastClick.pt.X)) < 6 && math.Abs(float64(pt.Y-lastClick.pt.Y)) < 6 {
			lastClick.up = true
		}
	}
	requestRedraw()
}

/* ---------- the menu-bar menu ---------- */

var menuWidgets = []struct {
	label string
	key   *bool
}{}

func buildMenu() {
	C.os_menu_clear()
	add := func(title string, tag int, checked, enabled bool) {
		s := C.CString(title)
		C.os_menu_add(s, C.int(tag), C.int(b2i(checked)), C.int(b2i(enabled)))
		C.free(unsafe.Pointer(s))
	}
	sep := func() { add("", -1, false, false) }
	snap := media.Snapshot()
	now := "Nothing playing"
	if snap.Track != nil {
		now = snap.Track.Title + " — " + snap.Track.Artist
	}
	add(now, 0, false, false)
	sep()
	add("Settings…", 1, false, true)
	add("Arrange widgets", 2, editMode, true)
	add(map[bool]string{true: "Leave full screen", false: "Full screen"}[fs != nil], 20, false, true)
	sep()
	add("Lyrics", 10, cfg.ShowLyrics, true)
	add("Turntable", 11, cfg.ShowVinyl, true)
	add("Visualizer", 12, cfg.ShowHalo, true)
	add("Clock", 13, cfg.ShowClock, true)
	add("Weather", 14, cfg.ShowWeather, true)
	add("Screen glow", 15, cfg.ShowGlow, true)
	sep()
	add("Play / pause", 3, false, true)
	add("Next song", 4, false, true)
	add("Previous song", 5, false, true)
	sep()
	add("Quit Orbit", 9, false, true)
}

//export goMenuOpening
func goMenuOpening() { buildMenu() }

//export goMenu
func goMenu(tag C.int) {
	toggle := func(p *bool, key string) {
		withCfg(func() { *p = !*p })
		settingsChanged(key)
	}
	switch tag {
	case 1:
		openSettings()
	case 2:
		setEditMode(!editMode)
	case 20:
		setFullscreen(fs == nil)
	case 3:
		mediaPlayPause()
	case 4:
		macControl("next track")
	case 5:
		macControl("previous track")
	case 9:
		hooks.Quit()
	case 10:
		toggle(&cfg.ShowLyrics, "ShowLyrics")
	case 11:
		toggle(&cfg.ShowVinyl, "ShowVinyl")
	case 12:
		toggle(&cfg.ShowHalo, "ShowHalo")
	case 13:
		toggle(&cfg.ShowClock, "ShowClock")
	case 14:
		toggle(&cfg.ShowWeather, "ShowWeather")
	case 15:
		toggle(&cfg.ShowGlow, "ShowGlow")
	}
}

func mediaPlayPause() { macControl("playpause") }

//export goMediaPing
func goMediaPing() {
	select {
	case mediaPing <- struct{}{}:
	default:
	}
}

/* ---------- helpers ---------- */

func menuIconPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(exe), "..", "Resources", "menubar.png")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func macFonts() []string {
	out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e",
		"ObjC.import('AppKit'); $.NSFontManager.sharedFontManager.availableFontFamilies.js.map(f => f.js).join('\\n')").Output()
	if err != nil {
		return []string{"Helvetica Neue", "Menlo", "Avenir Next", "Georgia"}
	}
	var fs []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f = strings.TrimSpace(f); f != "" && !strings.HasPrefix(f, ".") {
			fs = append(fs, f)
		}
	}
	return fs
}

func loginItemPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.dragnar1250.orbit.plist")
}

func loginItemEnabled() bool { _, err := os.Stat(loginItemPath()); return err == nil }

func setLoginItem(on bool) {
	if !on {
		os.Remove(loginItemPath())
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.dragnar1250.orbit</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Interactive</string>
</dict></plist>
`, xmlEscape(exe))
	os.MkdirAll(filepath.Dir(loginItemPath()), 0o755)
	os.WriteFile(loginItemPath(), []byte(plist), 0o644)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// logWidgets: where every widget is after a layout change (orbit.log — for "I can't see it").
var lastWidgetLog string

func logWidgets() {
	wa := workArea()
	out := fmt.Sprintf("screen %dx%d (work area %d,%d %dx%d, scale %.1f):", wa.Right, wa.Bottom, wa.Left, wa.Top, wa.Right-wa.Left, wa.Bottom-wa.Top, float64(C.os_scale()))
	for _, name := range allWidgets {
		if w := widgetByName(name); w != nil {
			r := w.rect()
			out += fmt.Sprintf(" %s=%v@%d,%d %dx%d", name, w.visible, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top)
		}
	}
	if out != lastWidgetLog {
		lastWidgetLog = out
		logf("%s", out)
	}
}

// fakePlay (tests only, ORBIT_FAKE_PLAY=1): a real song's title so its lyrics load, a moving
// position, and a beat in the audio so the visualizer and the motion effects have music.
func fakePlay() {
	media.SetTrack("Swim", "Chase Atlantic", "Phases", "spotify")
	media.SetDuration(keyOf("Swim", "Chase Atlantic"), 240)
	media.SetPosition(20, true)
	buf := make([]float32, 1440)
	t := 0.0
	for {
		for i := range buf {
			beat := math.Exp(-math.Mod(t, 0.5) * 9) // a kick twice a second
			buf[i] = float32(0.6*beat*math.Sin(2*math.Pi*60*t) + 0.15*math.Sin(2*math.Pi*440*t) + 0.08*math.Sin(2*math.Pi*2500*t))
			t += 1.0 / 48000
		}
		spectrum.Feed(buf, 48000)
		time.Sleep(30 * time.Millisecond)
	}
}

// perf: where a frame's time goes, in orbit.log — every minute while music plays (every 5 s with
// ORBIT_PERF=1). Frames per second as they really came, the slowest gap, and each widget's share.
type perfStats struct {
	frames  int
	maxGap  time.Duration
	last    time.Time
	from    time.Time
	spent   [7]time.Duration
	every   time.Duration
	started bool
}

var perf perfStats
var perfNames = [7]string{"turntable", "visualizer", "lyrics", "clock", "weather", "glow", "full screen"}

func (p *perfStats) tick(now time.Time) {
	if !p.started {
		p.started, p.from, p.every = true, now, time.Minute
		if os.Getenv("ORBIT_PERF") != "" {
			p.every = 5 * time.Second
		}
	}
	if !p.last.IsZero() {
		if gap := now.Sub(p.last); gap > p.maxGap && gap < time.Second {
			p.maxGap = gap
		}
	}
	p.last = now
	p.frames++
}

func (p *perfStats) add(i int, from time.Time) time.Time {
	n := time.Now()
	p.spent[i] += n.Sub(from)
	return n
}

func (p *perfStats) report(now time.Time, playing bool) {
	el := now.Sub(p.from)
	if el < p.every {
		return
	}
	if playing && p.frames > 0 {
		var secs C.double
		n := int(C.os_present_stats(&secs))
		out := fmt.Sprintf("perf: %.1f fps (screen %.0f Hz), slowest gap %.0f ms, shown %d pictures (%.2f ms each);", float64(p.frames)/el.Seconds(), float64(C.os_refresh_rate()), float64(p.maxGap.Microseconds())/1000, n, float64(secs)*1000/float64(max(n, 1)))
		for i, d := range p.spent {
			if d > 0 {
				out += fmt.Sprintf(" %s %.2f ms", perfNames[i], float64(d.Microseconds())/1000/float64(p.frames))
			}
		}
		logf("%s a frame", out)
	} else {
		var secs C.double
		C.os_present_stats(&secs)
	}
	p.frames, p.maxGap, p.from, p.spent = 0, 0, now, [7]time.Duration{}
}

// otherDesktopWidgets: the Mac's own desktop widgets (Calendar, Photos, Weather …, macOS 14+) —
// the lyrics keep clear of them like they do of Orbit's. Asked at most every 5 s.
var (
	deskWidgets   []RECT
	deskWidgetsAt time.Time
	deskWidgetLog string
)

func otherDesktopWidgets() []RECT {
	if time.Since(deskWidgetsAt) < 5*time.Second {
		return deskWidgets
	}
	deskWidgetsAt = time.Now()
	var r [256]C.double
	n := int(C.os_desktop_widgets(&r[0], 64))
	deskWidgets = deskWidgets[:0]
	for i := 0; i < n; i++ {
		deskWidgets = append(deskWidgets, RECT{int32(r[4*i]), int32(r[4*i+1]), int32(r[4*i] + r[4*i+2]), int32(r[4*i+1] + r[4*i+3])})
	}
	if s := fmt.Sprint(deskWidgets); s != deskWidgetLog {
		deskWidgetLog = s
		logf("macOS desktop widgets the lyrics avoid: %d %v", len(deskWidgets), deskWidgets)
	}
	return deskWidgets
}
