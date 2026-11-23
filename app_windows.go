package main

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

/* ===================== Widgets (one layered window each) ===================== */

type Widget struct {
	name         string
	label        string
	hwnd         uintptr
	canvas       *Canvas
	w, h         int
	alpha        byte
	visible      bool
	movable      bool
	onWheel      func(delta int)
	clickThrough bool
	dragging     bool // the user is moving it right now — don't move it ourselves
	offscreen    bool // only a canvas (full screen draws it itself): no window
	ox, oy       int  // offscreen: where it would be
}

var (
	hInstance         uintptr
	mainHwnd          uintptr
	widgets           = map[uintptr]*Widget{}
	editMode          bool
	wndProcCB         = syscall.NewCallback(wndProc)
	widgetClass       = u16("OrbitLyricsWidget")
	mainClass         = u16("OrbitLyricsMain")
	taskbarCreatedMsg uint32
)

func newWidget(name, label string) *Widget {
	w := &Widget{name: name, label: label, alpha: 255, movable: true}
	ex := uintptr(WS_EX_LAYERED | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_TRANSPARENT)
	w.hwnd = call(pCreateWindowExW, ex, uintptr(unsafe.Pointer(widgetClass)), uintptr(unsafe.Pointer(u16("Orbit "+label))),
		WS_POPUP, 0, 0, 10, 10, 0, 0, hInstance, 0)
	w.clickThrough = true
	widgets[w.hwnd] = w
	return w
}

// resize makes sure the backing canvas has the given size.
func (w *Widget) resize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if w.canvas != nil && w.w == width && w.h == height {
		return
	}
	w.canvas.Free()
	w.canvas = newCanvas(width, height)
	w.w, w.h = width, height
}

// g: the canvas' graphics — 0 when the bitmap couldn't be made (GDI+ then refuses each call
// quietly instead of the app crashing on a nil canvas).
func (w *Widget) g() uintptr {
	if w.canvas == nil {
		return 0
	}
	return w.canvas.g
}

// present pushes the canvas to the screen (keeps the window where it is).
func (w *Widget) present() {
	if w.offscreen {
		w.visible = w.canvas != nil
		return
	}
	if w.canvas == nil {
		return
	}
	size := SIZE{int32(w.w), int32(w.h)}
	src := POINT{0, 0}
	blend := BLENDFUNCTION{AC_SRC_OVER, 0, w.outAlpha(), AC_SRC_ALPHA}
	call(pUpdateLayeredWindow, w.hwnd, 0, 0, uintptr(unsafe.Pointer(&size)), w.canvas.dc, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ULW_ALPHA)
	if !w.visible {
		call(pShowWindow, w.hwnd, SW_SHOWNOACTIVATE)
		call(pSetWindowPos, w.hwnd, w.zAfter(), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
		w.visible = true
	}
}

// ulwBuf is only touched on the UI thread.
var ulwBuf = new(struct {
	size  SIZE
	src   POINT
	blend BLENDFUNCTION
	dirty RECT
	info  ulwInfo
})

type ulwInfo struct {
	CbSize   uint32
	HdcDst   uintptr
	PptDst   uintptr
	Psize    uintptr
	HdcSrc   uintptr
	PptSrc   uintptr
	CrKey    uint32
	Pblend   uintptr
	DwFlags  uint32
	PrcDirty uintptr
}

// presentDirty pushes only part of the canvas (much less work for the compositor at 60 fps).
func (w *Widget) presentDirty(r RECT) {
	if w.offscreen {
		w.present()
		return
	}
	if w.canvas == nil || !w.visible || pUpdateLayeredWindowIndirect.Find() != nil {
		w.present()
		return
	}
	// everything UpdateLayeredWindowIndirect reads through pointers lives in one heap block that
	// Go never moves (stack variables can move when the stack grows → Windows read garbage)
	u := ulwBuf
	u.size = SIZE{int32(w.w), int32(w.h)}
	u.src = POINT{0, 0}
	u.blend = BLENDFUNCTION{AC_SRC_OVER, 0, w.outAlpha(), AC_SRC_ALPHA}
	u.dirty = r
	u.info = ulwInfo{HdcSrc: w.canvas.dc, Psize: uintptr(unsafe.Pointer(&u.size)), PptSrc: uintptr(unsafe.Pointer(&u.src)),
		Pblend: uintptr(unsafe.Pointer(&u.blend)), DwFlags: ULW_ALPHA, PrcDirty: uintptr(unsafe.Pointer(&u.dirty))}
	u.info.CbSize = uint32(unsafe.Sizeof(u.info))
	ok := call(pUpdateLayeredWindowIndirect, w.hwnd, uintptr(unsafe.Pointer(&u.info)))
	runtime.KeepAlive(u)
	if ok == 0 {
		w.present()
	}
}

func (w *Widget) hide() {
	if w.offscreen {
		w.visible = false
		return
	}
	if w.visible {
		call(pShowWindow, w.hwnd, SW_HIDE)
		w.visible = false
	}
}

// release hides the widget and gives its pixels back (a turned-off widget shouldn't hold megabytes).
func (w *Widget) release() {
	w.hide()
	if w.canvas != nil {
		w.canvas.Free()
		w.canvas = nil
		w.w, w.h = 0, 0
	}
}

func (w *Widget) moveTo(x, y int) {
	if w.offscreen {
		w.ox, w.oy = x, y
		return
	}
	if w.dragging {
		return
	}
	call(pSetWindowPos, w.hwnd, 0, uintptr(int32(x)), uintptr(int32(y)), 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
}

func (w *Widget) rect() RECT {
	if w.offscreen {
		return RECT{int32(w.ox), int32(w.oy), int32(w.ox + w.w), int32(w.oy + w.h)}
	}
	var r RECT
	call(pGetWindowRect, w.hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

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

// zAfter: desktop widgets sit at the bottom, "Always on top" ones float above everything.
func (w *Widget) zAfter() uintptr {
	if w.name == "glow" {
		return HWND_TOPMOST // light over everything, but it never takes a click
	}
	if p := cfg.Widgets[w.name]; p != nil && p.OnTop {
		return HWND_TOPMOST
	}
	return HWND_BOTTOM
}

// locked widgets ignore the mouse completely, even in arrange mode.
func widgetLocked(name string) bool {
	p := cfg.Widgets[name]
	return p != nil && p.Locked
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
	w := widgetByName(name)
	if w == nil {
		return
	}
	if flag == "onTop" {
		if on {
			call(pSetWindowPos, w.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
		} else {
			call(pSetWindowPos, w.hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
			call(pSetWindowPos, w.hwnd, HWND_BOTTOM, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
		}
	} else {
		if drag != nil && on {
			endDrag()
		}
		w.setClickThrough(!editMode || on)
	}
	flash(name)
	styleDirty = true
	requestRedraw()
}

func showWidget(name string) {
	flash(name)
	if w := widgetByName(name); w != nil && w.visible && w.zAfter() == HWND_BOTTOM {
		// pop it up for a moment so it can be seen even behind windows; it sinks back on the next move
		call(pSetWindowPos, w.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
		peekUntil[name] = time.Now().Add(2 * time.Second)
	}
	styleDirty = true
	requestRedraw()
}

var peekUntil = map[string]time.Time{}

// endPeeks sends widgets that were popped up by "Show me where it is" back to the desktop.
func endPeeks() {
	for name, t := range peekUntil {
		if time.Now().After(t) {
			delete(peekUntil, name)
			if w := widgetByName(name); w != nil && w.zAfter() == HWND_BOTTOM {
				call(pSetWindowPos, w.hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
				call(pSetWindowPos, w.hwnd, HWND_BOTTOM, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
			}
		}
	}
}

func (w *Widget) setClickThrough(on bool) {
	if w.offscreen {
		return
	}
	if w.clickThrough == on {
		return
	}
	ex := call(pGetWindowLongPtrW, w.hwnd, gwlExStyle)
	if on {
		ex |= WS_EX_TRANSPARENT
	} else {
		ex &^= WS_EX_TRANSPARENT
	}
	call(pSetWindowLongPtrW, w.hwnd, gwlExStyle, ex)
	w.clickThrough = on
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

/* ===================== Window procedure ===================== */

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC in window message 0x%X: %v\n%s", msg, r, debug.Stack())
			ret = 0
		}
	}()
	if w, ok := widgets[hwnd]; ok {
		if r, handled := widgetMsg(w, msg, wp, lp); handled {
			return r
		}
		switch msg {
		case WM_MOUSEACTIVATE:
			return MA_NOACTIVATE
		case WM_WINDOWPOSCHANGING:
			// stay behind normal windows like a desktop widget (the arrange bar stays on top)
			if w.name != "banner" {
				pos := (*WINDOWPOS)(unsafe.Pointer(lp))
				pos.HwndInsertAfter = w.zAfter()
				pos.Flags &^= SWP_NOZORDER
			}
			return 0
		case 0x0231: // WM_ENTERSIZEMOVE
			w.dragging = true
			return 0
		case WM_EXITSIZEMOVE:
			w.dragging = false
			w.savePos()
			requestRedraw()
			return 0
		}
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	switch msg {
	case WM_TIMER:
		tick()
		updateTick()
		return 0
	case WM_HOTKEY:
		if wp == 2 {
			toggleFullscreen()
		} else {
			setEditMode(!editMode)
		}
		return 0
	case WM_APP + 3: // a second copy was started: show the settings instead
		openSettings()
		return 0
	case WM_APP + 2: // frame from frameLoop
		// clicks and keys first: Windows hands out posted messages before input, so a frame
		// that is always waiting made Esc and the mouse feel stuck when frames were slow
		// (keys and clicks only — a moving mouse is almost always waiting, and skipping for it
		// dropped full screen to 2-3 frames a second; and never two frames in a row)
		if fs != nil && call(pGetQueueStatus, qsInput)>>16 != 0 && !skippedFrame {
			skippedFrame = true
			atomic.StoreInt32(&framePending, 0)
			return 0
		}
		skippedFrame = false
		tick()
		// the next frame may only be asked for once this one is done — never queued behind it
		atomic.StoreInt32(&framePending, 0)
		return 0
	case WM_APP + 1: // tray icon
		switch uint32(lp & 0xFFFF) {
		case WM_LBUTTONUP:
			openSettings()
		case WM_RBUTTONUP, WM_CONTEXTMENU:
			showMenu()
		}
		return 0
	case WM_DISPLAYCHANGE, WM_SETTINGCHANGE, WM_DPICHANGED:
		requestRedraw()
		layoutDirty = true
	case WM_DESTROY:
		removeTray()
		call(pPostQuitMessage, 0)
		return 0
	}
	if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg { // Explorer restarted → re-add tray icon
		addTray()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

/* ===================== Tray icon + menu ===================== */

var trayIcon uintptr

var pLoadImageW = user32.NewProc("LoadImageW")

// appIcon: the Orbit logo from the exe's resources at the given size (0 if missing).
func appIcon(size int) uintptr {
	return call(pLoadImageW, hInstance, 1, 1 /* IMAGE_ICON */, uintptr(size), uintptr(size), 0)
}

func makeTrayIcon() uintptr {
	if h := appIcon(int(call(pGetSystemMetrics, 49))); h != 0 { // SM_CXSMICON: the tray's size at this DPI
		return h
	}
	b := newBitmap(32, 32)
	if b == nil {
		return 0
	}
	g := b.g
	gClear(g, 0)
	fillCircle(g, 0xFF101012, 16, 16, 15)
	for r := float32(7); r < 14.5; r += 2 {
		strokeCircle(g, 0x30FFFFFF, 16, 16, r, 1)
	}
	fillCircle(g, 0xFFE8B04A, 16, 16, 6)
	fillCircle(g, 0xFF101012, 16, 16, 1.6)
	h := hiconFromBitmap(b)
	b.Free()
	return h
}

func trayData() *NOTIFYICONDATAW {
	d := &NOTIFYICONDATAW{HWnd: mainHwnd, UID: 1, UFlags: NIF_MESSAGE | NIF_ICON | NIF_TIP, UCallbackMessage: WM_APP + 1, HIcon: trayIcon}
	d.CbSize = uint32(unsafe.Sizeof(*d))
	tip, _ := syscall.UTF16FromString("Orbit")
	copy(d.SzTip[:], tip)
	return d
}

func addTray()    { call(pShellNotifyIconW, NIM_ADD, uintptr(unsafe.Pointer(trayData()))) }
func removeTray() { call(pShellNotifyIconW, NIM_DELETE, uintptr(unsafe.Pointer(trayData()))) }

func trayBalloon(title, text string) {
	d := trayData()
	d.UFlags |= NIF_INFO
	t, _ := syscall.UTF16FromString(title)
	m, _ := syscall.UTF16FromString(text)
	copy(d.SzInfoTitle[:], t)
	copy(d.SzInfo[:], m)
	d.DwInfoFlags = 1
	call(pShellNotifyIconW, NIM_MODIFY, uintptr(unsafe.Pointer(d)))
}

const (
	idSettings = 100 + iota
	idEdit
	idShowLyrics
	idShowVinyl
	idShowHalo
	idStartup
	idExit
	idRandom
	idShowClock
	idShowWeather
	idShowGlow
	idFullscreen
)

const idStyleBase = 200
const idMoveBase = 600 // + widget*32 + item

var moveWidgets = []string{"vinyl", "lyrics", "halo", "clock", "weather"}
var movePlaces = []struct {
	name string
	x, y float64
}{
	{"Top left", 14, 16}, {"Top", 50, 14}, {"Top right", 86, 16},
	{"Left", 14, 50}, {"Centre", 50, 50}, {"Right", 86, 50},
	{"Bottom left", 14, 84}, {"Bottom", 50, 86}, {"Bottom right", 86, 84},
}

const (
	moveBigger  = 20
	moveSmaller = 21
	moveReset   = 22
	moveCorner  = 24 // +0..3 (turntable only)
)

func menuItem(m uintptr, id int, text string, checked bool) {
	f := uintptr(MF_STRING)
	if checked {
		f |= MF_CHECKED
	}
	call(pAppendMenuW, m, f, uintptr(id), uintptr(unsafe.Pointer(u16(text))))
}

func subMenu(m uintptr, text string, sub uintptr) {
	call(pAppendMenuW, m, MF_POPUP, sub, uintptr(unsafe.Pointer(u16(text))))
}

func sep(m uintptr) { call(pAppendMenuW, m, MF_SEPARATOR, 0, 0) }

func showMenu() {
	m := call(pCreatePopupMenu)
	snap := media.Snapshot()
	now := "Nothing playing"
	if snap.Track != nil {
		now = "♪ " + snap.Track.Title + " — " + snap.Track.Artist
		if len([]rune(now)) > 60 {
			now = string([]rune(now)[:58]) + "…"
		}
	}
	call(pAppendMenuW, m, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(u16(now))))
	sep(m)
	menuItem(m, idSettings, "Settings…", false)
	menuItem(m, idEdit, "Arrange widgets with the mouse\tCtrl+Alt+E", editMode)
	menuItem(m, idFullscreen, "Full screen\tCtrl+Alt+F", fs != nil)
	st := call(pCreatePopupMenu)
	for i, n := range styleNames {
		menuItem(st, idStyleBase+i, n, cfg.LyricsStyle == i)
	}
	subMenu(m, "Lyrics style", st)
	menuItem(m, idRandom, "Lyrics in a random spot every line", cfg.LyricsPlace == 0)
	mv := call(pCreatePopupMenu)
	for wi, name := range moveWidgets {
		sub := call(pCreatePopupMenu)
		base := idMoveBase + wi*32
		if name == "vinyl" {
			for ci, cn := range cornerNames {
				menuItem(sub, base+moveCorner+ci, "Tuck into "+strings.ToLower(cn), cfg.VinylCorner == ci && !cfg.widget("vinyl").Set)
			}
			sep(sub)
		}
		for pi, pl := range movePlaces {
			menuItem(sub, base+pi, pl.name, false)
		}
		sep(sub)
		menuItem(sub, base+moveBigger, "Bigger  (+15%)", false)
		menuItem(sub, base+moveSmaller, "Smaller  (−15%)", false)
		menuItem(sub, base+moveReset, "Reset position and size", false)
		subMenu(mv, map[string]string{"vinyl": "Turntable", "lyrics": "Lyrics", "halo": "Halo", "clock": "Clock", "weather": "Weather"}[name], sub)
	}
	subMenu(m, "Move && resize", mv)
	sep(m)
	menuItem(m, idShowLyrics, "Show lyrics", cfg.ShowLyrics)
	menuItem(m, idShowVinyl, "Show turntable", cfg.ShowVinyl)
	menuItem(m, idShowHalo, "Show halo visualizer", cfg.ShowHalo)
	menuItem(m, idShowClock, "Show clock", cfg.ShowClock)
	menuItem(m, idShowWeather, "Show weather", cfg.ShowWeather)
	menuItem(m, idShowGlow, "Screen glow", cfg.ShowGlow)
	sep(m)
	menuItem(m, idStartup, "Start with Windows", startupEnabled())
	menuItem(m, idExit, "Exit Orbit", false)

	var pt POINT
	call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
	call(pSetForegroundWindow, mainHwnd)
	id := int(call(pTrackPopupMenu, m, TPM_RIGHTBUTTON|TPM_RETURNCMD|TPM_BOTTOMALIGN, uintptr(pt.X), uintptr(pt.Y), 0, mainHwnd, 0))
	call(pPostMessageW, mainHwnd, 0, 0, 0)
	call(pDestroyMenu, m) // destroys the sub menus too
	if id != 0 {
		onMenu(id)
	}
}

func onMenu(id int) {
	switch {
	case id == idSettings:
		openSettings()
		return
	case id == idEdit:
		setEditMode(!editMode)
		return
	case id == idFullscreen:
		toggleFullscreen()
		return
	case id == idStartup:
		exe, _ := os.Executable()
		setStartup(!startupEnabled(), exe)
		return
	case id == idExit:
		quitApp()
		return
	case id >= idMoveBase && id < idMoveBase+32*len(moveWidgets):
		wi, item := (id-idMoveBase)/32, (id-idMoveBase)%32
		moveFromMenu(moveWidgets[wi], item)
		return
	}
	withCfg(func() {
		switch {
		case id == idShowLyrics:
			cfg.ShowLyrics = !cfg.ShowLyrics
		case id == idShowVinyl:
			cfg.ShowVinyl = !cfg.ShowVinyl
		case id == idShowHalo:
			cfg.ShowHalo = !cfg.ShowHalo
		case id == idShowClock:
			cfg.ShowClock = !cfg.ShowClock
		case id == idShowWeather:
			cfg.ShowWeather = !cfg.ShowWeather
			kickWeather()
		case id == idShowGlow:
			cfg.ShowGlow = !cfg.ShowGlow
		case id == idRandom:
			cfg.LyricsPlace = 1 - cfg.LyricsPlace
		case id >= idStyleBase && id < idStyleBase+len(styleNames):
			cfg.LyricsStyle = id - idStyleBase
		}
	})
	settingsChanged("menu")
}

// setupCrashLog: fatal errors (which can't be caught) are written to crash.log.
func setupCrashLog() {
	p := filepath.Join(appDir(), "crash.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "goroutine") && time.Since(st.ModTime()) < 24*time.Hour {
			logf("the previous run crashed — details in crash.log")
		}
		if st.Size() > 256*1024 {
			_ = os.Remove(p)
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "\n=== %s  Orbit %s started ===\n", time.Now().Format("2006-01-02 15:04:05"), appVersion)
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	f.Close()
}

func quitApp() {
	closeFullscreen()
	saveConfigIfDue(true)
	call(pPostMessageW, mainHwnd, WM_CLOSE, 0, 0)
}

// moveFromMenu handles the tray "Move & resize" items (works without the settings window).
func moveFromMenu(name string, item int) {
	info := widgetInfo()[name]
	switch {
	case item < len(movePlaces):
		setWidget(name, movePlaces[item].x, movePlaces[item].y, info.Scale)
	case item == moveBigger:
		setWidget(name, info.X, info.Y, math.Min(3, info.Scale*1.15))
	case item == moveSmaller:
		setWidget(name, info.X, info.Y, math.Max(0.3, info.Scale/1.15))
	case item == moveReset:
		resetPositions(name)
		flash(name)
	case item >= moveCorner && item < moveCorner+4:
		withCfg(func() { cfg.VinylCorner = item - moveCorner })
		resetPositions("vinyl")
		flash("vinyl")
	}
}

// openSettings shows the settings window (Edge app mode = a plain window without tabs).
func openSettings() { openSettingsAt("") }

// openSettingsAt opens the settings window on a page ("vinyl", "clock" …).
func openSettingsAt(page string) {
	if settingsSrv == nil {
		messageBox("The settings window could not start (see orbit.log).", "Orbit")
		return
	}
	url := settingsSrv.URL()
	if page != "" {
		url += "#" + page
	}
	args := "--app=" + url + " --window-size=1120,800 --new-window"
	r := call(pShellExecuteW, 0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16("msedge.exe"))), uintptr(unsafe.Pointer(u16(args))), 0, 1)
	if r <= 32 { // no Edge → default browser
		logf("settings: Edge not found (%d), using default browser", r)
		call(pShellExecuteW, 0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(url))), 0, 0, 1)
	}
}

var editModeFlag atomic.Bool // readable from the settings server goroutine

func setEditMode(on bool) {
	if on && fs != nil {
		closeFullscreen()
	}
	if drag != nil {
		endDrag()
	}
	editMode = on
	editModeFlag.Store(on)
	hoverName, hoverBtn = "", -1
	for _, w := range widgets {
		if w.name == "glow" {
			continue // the screen glow never takes the mouse
		}
		w.setClickThrough(!on || widgetLocked(w.name))
	}
	logf("arrange mode %v", on)
	bannerSig = ""
	styleDirty = true
	requestRedraw()
}

// settingsChanged runs on the UI thread after any setting changed.
func settingsChanged(key string) {
	switch key {
	case "FPS":
		applyFPS()
		installMouseHook()
		registerHotkey()
		call(pSetTimer, mainHwnd, 1, 1000, 0) // safety net: keeps things alive if frames ever stall
		go frameLoop()
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
}

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

var (
	fpsNow       int32 // read by frameLoop
	framePending int32 // 1 while a frame message is waiting in the queue
)

// applyFPS: frames come from frameLoop (high-resolution sleep), not SetTimer, because
// Windows rounds SetTimer to ~15.6 ms steps and that can't give a smooth 60 fps.
var pGetQueueStatus = user32.NewProc("GetQueueStatus")

const qsInput = 0x0005 // QS_KEY | QS_MOUSEBUTTON (not QS_MOUSEMOVE)

var skippedFrame bool

func applyFPS() { atomic.StoreInt32(&fpsNow, int32(cfg.FPS)) }

func frameLoop() {
	next := time.Now()
	for {
		fps := atomic.LoadInt32(&fpsNow)
		d := 200 * time.Millisecond // 0 fps: just follow the song (5 checks a second)
		if fps > 0 {
			d = time.Second / time.Duration(fps)
		}
		next = next.Add(d)
		now := time.Now()
		if next.Before(now) {
			next = now.Add(d) // fell behind (sleep, busy PC): don't try to catch up
		}
		time.Sleep(next.Sub(now))
		if atomic.CompareAndSwapInt32(&framePending, 0, 1) {
			call(pPostMessageW, mainHwnd, WM_APP+2, 0, 0)
		}
	}
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

// installedFonts lists every font family on this PC (GDI+), sorted.
func installedFonts() []string {
	var coll uintptr
	if call(gpNewInstalledFontCollection, uintptr(unsafe.Pointer(&coll))) != 0 || coll == 0 {
		return legacyFonts
	}
	var n int32
	call(gpGetFontCollectionFamilyCount, coll, uintptr(unsafe.Pointer(&n)))
	if n <= 0 {
		return legacyFonts
	}
	fams := make([]uintptr, n)
	var found int32
	call(gpGetFontCollectionFamilyList, coll, uintptr(n), uintptr(unsafe.Pointer(&fams[0])), uintptr(unsafe.Pointer(&found)))
	seen := map[string]bool{}
	var out []string
	for i := int32(0); i < found && i < n; i++ {
		var name [32]uint16
		if call(gpGetFamilyName, fams[i], uintptr(unsafe.Pointer(&name[0])), 0) == 0 {
			s := syscall.UTF16ToString(name[:])
			if s != "" && !strings.HasPrefix(s, "@") && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return legacyFonts
	}
	return out
}

/* ===================== Main loop ===================== */

var (
	needRedraw  = true
	layoutDirty = true
	styleDirty  = true
	lastTrim    time.Time
)

func requestRedraw() { needRedraw = true }

var vinyl *VinylView
var lyricView *LyricsView
var halo *HaloView
var clockView *ClockView
var weatherView *WeatherView
var glowView *GlowView

func tick() {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC while drawing: %v\n%s", r, debug.Stack())
		}
	}()
	drainUI()
	trimCaches()
	endPeeks()
	hoverCheck()
	now := time.Now()
	snap := media.Snapshot()
	bands, bass, active := spectrum.Compute()
	ui := uiScale()
	if fs != nil { // full screen covers the desktop: only it is drawn
		atomic.StoreInt32(&bandsWanted, 1)
		fs.Update(now, snap, bands, bass)
		needRedraw, layoutDirty, styleDirty = false, false, false
		saveConfigIfDue(false)
		return
	}
	if snap.Playing {
		prefetchHiRes(snap.Track)
	}
	if vinyl != nil {
		vinyl.Update(now, snap, bands, bass, ui)
	}
	if halo != nil {
		halo.Update(now, snap, bands, bass, active, ui)
	}
	if lyricView != nil {
		lyricView.Update(now, snap, bass, ui)
	}
	if clockView != nil {
		clockView.Update(now, snap, bass, ui)
	}
	if weatherView != nil {
		weatherView.Update(now, ui)
	}
	if glowView != nil {
		glowView.Update(now, snap, bands, bass, ui)
	}
	updateBanner()
	needRedraw, layoutDirty, styleDirty = false, false, false
	saveConfigIfDue(false)
	if hoverNote != "" && now.After(hoverNoteUntil) { // the double-click note has been read
		hoverNote = ""
		styleDirty = true
		requestRedraw()
	}
	atomic.StoreInt32(&fpsNow, desktopFPS(snap, now))
	atomic.StoreInt32(&bandsWanted, b2i(wantBands()))
	// paging the app out keeps Task Manager's number low, but everything has to come back in
	// afterwards — do it while the music rests, and only rarely while it plays
	every := 15 * time.Minute
	if !snap.Playing {
		every = 2 * time.Minute
	}
	if now.Sub(lastTrim) > every {
		lastTrim = now
		runtime.GC()
		trimWorkingSet()
	}
}

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

var (
	gameCheckAt time.Time
	gameOn      bool
)

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

// uiScale: 1.0 on a 1080p screen, bigger on 1440p / 4K.
func uiScale() float64 {
	wa := workArea()
	h := float64(wa.Bottom - wa.Top)
	w := float64(wa.Right - wa.Left)
	s := math.Min(w/1920, h/1040)
	return math.Max(0.6, math.Min(3, s))
}

/* ===================== Start-up probe ===================== */

// The Windows media-controls and audio code talks to COM directly. To be safe, it is first
// tried in a child process: if anything there crashes, the app still runs (with fallbacks).
func probeMain() {
	runtime.LockOSThread()
	fmt.Println("PROBE_START")
	if s := newSMTC(); s != nil {
		fmt.Println("SMTC_INIT")
		thumbsEnabled = false
		ok := s.poll() // text + timeline only
		fmt.Printf("SMTC_OK %v\n", ok)
		if snap := media.Snapshot(); snap.Track != nil {
			fmt.Printf("SMTC_TRACK %s | %s | %.1f/%.1f playing=%v\n", snap.Track.Title, snap.Track.Artist, snap.Pos, snap.Track.Duration, snap.Playing)
			if sess := s.session(); sess != 0 {
				var op uintptr
				comCall(sess, 7, uintptr(unsafe.Pointer(&op)))
				if props, ok := await(op, 3*time.Second); ok {
					img := s.readThumbnail(props)
					comRelease(props)
					fmt.Printf("THUMB_OK %d\n", len(img))
				}
				comRelease(sess)
			}
		} else {
			fmt.Println("THUMB_OK untested") // nothing playing: thumbnail code path is guarded by QueryInterface
		}
	} else {
		fmt.Println("SMTC_FAIL")
	}
	call(pCoInitializeEx, 0, 0)
	var l loopback
	if l.open() {
		time.Sleep(300 * time.Millisecond)
		b, ok := l.read(make([]float32, 0, 4096))
		fmt.Printf("AUDIO_OK %v %d %.4f\n", ok, len(b), rms(b))
		l.close()
	} else {
		fmt.Println("AUDIO_FAIL")
	}
	fmt.Println("PROBE_END")
}

func runProbe() (smtcOK, thumbOK, audioOK bool) {
	exe, err := os.Executable()
	if err != nil {
		return false, false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--probe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	text := string(out)
	logf("probe: %q err=%v", strings.ReplaceAll(text, "\n", " | "), err)
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "SMTC_OK") {
			smtcOK = true
		}
		if strings.HasPrefix(l, "THUMB_OK") {
			thumbOK = true
		}
		if strings.HasPrefix(l, "AUDIO_OK") {
			audioOK = true
		}
	}
	return
}

/* ===================== main ===================== */

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--probe" {
		probeMain()
		return
	}
	runtime.LockOSThread()
	logf("Orbit %s starting", appVersion)
	if handleUpdateStart() { // an update that didn't work out: the old version was started instead
		return
	}
	setupCrashLog()
	// a small heap: collect a little more often and keep Go's memory near what is in use
	debug.SetGCPercent(60)
	debug.SetMemoryLimit(160 << 20)

	// single instance: the same version just opens its settings; an older version is closed
	// and replaced (otherwise an update would silently keep running the old copy)
	myTitle := "Orbit " + appVersion
	for attempt := 0; ; attempt++ {
		h, _, e := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16("OrbitLyrics.SingleInstance"))))
		errno, _ := e.(syscall.Errno)
		if errno != ERROR_ALREADY_EXISTS {
			break
		}
		call(pCloseHandle, h)
		if attempt == 0 {
			if old := call(pFindWindowW, uintptr(unsafe.Pointer(mainClass)), 0); old != 0 {
				if windowText(old) == myTitle {
					call(pPostMessageW, old, WM_APP+3, 0, 0) // "open your settings"
					return
				}
				logf("closing the older copy (%q) to take over", windowText(old))
				call(pPostMessageW, old, WM_CLOSE, 0, 0)
			}
		}
		if attempt > 60 {
			messageBox("Another Orbit is still running and didn't close.\nRight-click its icon in the tray → Exit, then start this one again.", "Orbit")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	if call(pSetProcessDpiAwarenessCtx, ^uintptr(3)) == 0 { // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		call(pSetProcessDPIAware)
	}
	loadConfig()
	media.SetOffset(cfg.OffsetSec)
	if !gdipInit() {
		messageBox("Could not start GDI+ (Windows graphics).", "Orbit")
		return
	}
	formats()

	smtcOK, thumbOK, audioOK := runProbe()
	go mediaLoop(smtcOK, thumbOK)
	if audioOK {
		go audioLoop()
	} else {
		logf("audio capture unavailable — halo will not move")
	}

	hInstance = call(pGetModuleHandleW, 0)
	cursor := call(pLoadCursorW, 0, 32512)
	for _, cls := range []*uint16{widgetClass, mainClass} {
		wc := WNDCLASSEXW{LpfnWndProc: wndProcCB, HInstance: hInstance, LpszClassName: cls, HCursor: cursor,
			HIcon: appIcon(int(call(pGetSystemMetrics, 11))), HIconSm: appIcon(int(call(pGetSystemMetrics, 49)))}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		call(pRegisterClassExW, uintptr(unsafe.Pointer(&wc)))
	}
	mainHwnd = call(pCreateWindowExW, WS_EX_TOOLWINDOW, uintptr(unsafe.Pointer(mainClass)), uintptr(unsafe.Pointer(u16(myTitle))), WS_POPUP, 0, 0, 0, 0, 0, 0, hInstance, 0)
	taskbarCreatedMsg = uint32(call(pRegisterWindowMessageW, uintptr(unsafe.Pointer(u16("TaskbarCreated")))))

	fonts := installedFonts()
	fontList = fonts
	logf("%d fonts installed", len(fonts))
	hooks = Hooks{
		Fonts:          func() []string { return fonts },
		StartupEnabled: startupEnabled,
		SetStartup: func(on bool) {
			exe, _ := os.Executable()
			setStartup(on, exe)
		},
		OpenFolder: func() {
			call(pShellExecuteW, 0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(appDir()))), 0, 0, 1)
		},
		Quit:            quitApp,
		SetEditMode:     setEditMode,
		EditMode:        editModeFlag.Load,
		SettingsChanged: settingsChanged,
		ResetPositions:  resetPositions,
		WidgetInfo:      widgetInfo,
		SetWidget:       setWidget,
		SetWidgetFlag:   setWidgetFlag,
		Flash:           showWidget,
		SetOpacity:      setOpacity,
		FormatDate:      formatDate,
		Fullscreen:      setFullscreen,
		UpdateInstall:   installUpdate,
		UpdateReady:     func() { runOnUI(func() { updateReady(false) }) },
	}
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
	go weatherLoop(func() (string, float64, float64, bool, bool) {
		var place string
		var lat, lon float64
		var f, want bool
		withCfg(func() {
			place, lat, lon, f, want = cfg.WeatherCity, cfg.WeatherLat, cfg.WeatherLon, cfg.WeatherF, cfg.ShowWeather
		})
		return place, lat, lon, f, want
	}, func() { runOnUI(func() { styleDirty = true; requestRedraw() }) })

	trayIcon = makeTrayIcon()
	addTray()
	// "Start with Windows" remembers the exe's path: keep it right after a move or a rename
	// (Orbit Lyrics.exe → Orbit.exe)
	if startupEnabled() {
		if exe, err := os.Executable(); err == nil {
			setStartup(true, exe)
		}
	}
	if _, err := os.Stat(filepath.Join(appDir(), "config.json")); err != nil {
		trayBalloon("Orbit is running", "Click the Orbit icon here for settings. Right-click for the quick menu.")
		saveConfigSoon()
		openSettings()
	}
	applyFPS()
	installMouseHook()
	registerHotkey()
	call(pSetTimer, mainHwnd, 1, 1000, 0) // safety net: keeps things alive if frames ever stall
	go frameLoop()
	afterUpdateStarted()
	go updateLoop()

	var msg MSG
	for {
		r := call(pGetMessageW, uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		call(pTranslateMessage, uintptr(unsafe.Pointer(&msg)))
		call(pDispatchMessageW, uintptr(unsafe.Pointer(&msg)))
	}
	saveConfigIfDue(true)
	logf("exit")
}
