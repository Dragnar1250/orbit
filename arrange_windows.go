package main

// Mouse control of the widgets.
//  - Arrange mode (double-click a widget on the desktop, Ctrl+Alt+E, tray or settings):
//    a box with corner handles around every widget; drag inside to move, drag a corner or
//    scroll to resize; a bar at the top has a "Done" button.
//  - Outside arrange mode, scrolling over a widget on the bare desktop resizes it too.

import (
	"math"
	"syscall"
	"time"
	"unsafe"
)

var (
	pSetCapture         = user32.NewProc("SetCapture")
	pReleaseCapture     = user32.NewProc("ReleaseCapture")
	pSetCursor          = user32.NewProc("SetCursor")
	pSetWindowsHookExW  = user32.NewProc("SetWindowsHookExW")
	pCallNextHookEx     = user32.NewProc("CallNextHookEx")
	pKeybdEvent         = user32.NewProc("keybd_event")
	pRegisterHotKey     = user32.NewProc("RegisterHotKey")
	pWindowFromPoint    = user32.NewProc("WindowFromPoint")
	pGetAncestor        = user32.NewProc("GetAncestor")
	pGetClassNameW      = user32.NewProc("GetClassNameW")
	pGetDoubleClickTime = user32.NewProc("GetDoubleClickTime")
)

const (
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_SETCURSOR      = 0x0020
	WM_CAPTURECHANGED = 0x0215
	WM_HOTKEY         = 0x0312
	HTCLIENT          = 1
	WH_MOUSE_LL       = 14
	IDC_SIZEALL       = 32646
	IDC_SIZENWSE      = 32642
	IDC_SIZENESW      = 32643
	IDC_HAND          = 32649
)

type msllHook struct {
	Pt        POINT
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

var arrangeWidgets = []string{"vinyl", "lyrics", "halo", "clock", "weather"}

func handlePx() int { return int(22 * math.Max(1, uiScale())) }

/* ---------- live move / resize ---------- */

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

/* ---------- dragging inside arrange mode ---------- */

type dragState struct {
	w                    *Widget
	corner               int
	startX, startY       int32
	startCX, startCY     float64
	startScale, startDst float64
}

var drag *dragState

func cursorPos() POINT {
	var pt POINT
	call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
	return pt
}

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
	call(pSetCapture, w.hwnd)
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
	call(pReleaseCapture)
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

// widgetMsg handles mouse messages of the widget windows. ok=false → default handling.
func widgetMsg(w *Widget, msg uint32, wp, lp uintptr) (uintptr, bool) {
	if w.name == "banner" {
		switch msg {
		case WM_NCHITTEST:
			if editMode {
				return HTCLIENT, true
			}
			return ^uintptr(0), true
		case WM_SETCURSOR:
			call(pSetCursor, call(pLoadCursorW, 0, IDC_HAND))
			return 1, true
		case WM_LBUTTONUP:
			setEditMode(false)
			return 0, true
		}
		return 0, false
	}
	switch msg {
	case WM_NCHITTEST:
		if (editMode || w.name == hoverName) && w.movable {
			return HTCLIENT, true
		}
		return ^uintptr(0), true // HTTRANSPARENT
	case WM_SETCURSOR:
		if !editMode && w.name == hoverName && toolbarHit(w, cursorPos()) >= 0 {
			call(pSetCursor, call(pLoadCursorW, 0, IDC_HAND))
			return 1, true
		}
		if editMode || w.name == hoverName {
			cur := uintptr(IDC_SIZEALL)
			switch hoverCorner(w) {
			case 1, 4:
				cur = IDC_SIZENWSE
			case 2, 3:
				cur = IDC_SIZENESW
			}
			call(pSetCursor, call(pLoadCursorW, 0, cur))
			return 1, true
		}
	case WM_LBUTTONDOWN:
		if !editMode && w.name == hoverName {
			if b := toolbarHit(w, cursorPos()); b >= 0 {
				toolbarAction(w.name, b)
				return 0, true
			}
		}
		if editMode || w.name == hoverName {
			beginDrag(w)
			return 0, true
		}
	case WM_MOUSEMOVE:
		if drag != nil && drag.w == w {
			dragMove(false)
			return 0, true
		}
	case WM_LBUTTONUP:
		if drag != nil && drag.w == w {
			endDrag()
			return 0, true
		}
	case WM_CAPTURECHANGED:
		if drag != nil && drag.w == w { // capture lost (another window popped up): keep where it is
			drag = nil
			saveConfigSoon()
		}
		return 0, true
	case WM_MOUSEWHEEL:
		wheelResize(w.name, int(int16(uint16(wp>>16))))
		return 0, true
	case WM_RBUTTONUP:
		if editMode || w.name == hoverName {
			showMenu()
			return 0, true
		}
	}
	return 0, false
}

/* ---------- low-level mouse hook: scroll-to-resize + double-click on the desktop ---------- */

var (
	hookHandle uintptr
	lastClick  struct {
		t    time.Time
		pt   POINT
		name string
		id   int  // bumps with every click, so a pending play/pause knows it was the last one
		up   bool // released without moving → a real click, not the start of a drag
	}
	hookCB = syscall.NewCallback(mouseHook)
)

func installMouseHook() {
	if hookHandle != 0 {
		return
	}
	hookHandle = call(pSetWindowsHookExW, WH_MOUSE_LL, hookCB, hInstance, 0)
	if hookHandle == 0 {
		logf("mouse hook not available — scroll-to-resize on the desktop is off")
	}
}

// desktopAt: is the bare desktop (not an app window) under this point?
func desktopAt(pt POINT) bool {
	h := call(pWindowFromPoint, uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32)
	if h == 0 {
		return false
	}
	if _, mine := widgets[h]; mine {
		return true
	}
	root := call(pGetAncestor, h, 2) // GA_ROOT
	var buf [64]uint16
	n := call(pGetClassNameW, root, uintptr(unsafe.Pointer(&buf[0])), 64)
	return isDesktopClass(syscall.UTF16ToString(buf[:n]))
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

func mouseHook(code, wp, lp uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			logf("mouse hook panic: %v", r)
		}
	}()
	if int32(code) >= 0 && lp != 0 && drag == nil {
		ev := (*msllHook)(unsafe.Pointer(lp))
		switch uint32(wp) {
		case WM_MOUSEWHEEL:
			if name := widgetAt(ev.Pt); name != "" && (editMode || desktopAt(ev.Pt)) {
				delta := int(int16(uint16(ev.MouseData >> 16)))
				n := name
				runOnUI(func() { wheelResize(n, delta) })
				return 1 // swallow: the desktop doesn't need this scroll
			}
		case WM_LBUTTONDOWN:
			if editMode {
				break
			}
			name := widgetAt(ev.Pt)
			if name == "" || !desktopAt(ev.Pt) {
				lastClick.name = ""
				break
			}
			dbl := time.Duration(call(pGetDoubleClickTime)) * time.Millisecond
			if lastClick.name == name && time.Since(lastClick.t) < dbl &&
				math.Abs(float64(ev.Pt.X-lastClick.pt.X)) < 8 && math.Abs(float64(ev.Pt.Y-lastClick.pt.Y)) < 8 {
				lastClick.name = ""
				if cfg.HoverEdit { // pointing already moves it, and settings has its own box: a double-click changes the colours
					runOnUI(func() { doubleClickTheme(name) })
				} else {
					runOnUI(func() { setEditMode(true); flash(name) })
				}
				return 1
			}
			lastClick.t, lastClick.pt, lastClick.name = time.Now(), ev.Pt, name
			lastClick.id++
			lastClick.up = false
			if name == "vinyl" && cfg.ClickPlay {
				// wait a double-click time: a second click means "arrange", not "pause"
				id := lastClick.id
				time.AfterFunc(dbl+40*time.Millisecond, func() {
					runOnUI(func() {
						if lastClick.id == id && lastClick.name == "vinyl" && lastClick.up && !editMode {
							mediaPlayPause()
						}
					})
				})
			}
		case WM_MOUSEMOVE:
			hoverMove(ev.Pt)
		case WM_LBUTTONUP:
			if lastClick.name != "" && math.Abs(float64(ev.Pt.X-lastClick.pt.X)) < 6 && math.Abs(float64(ev.Pt.Y-lastClick.pt.Y)) < 6 {
				lastClick.up = true
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(hookHandle, code, wp, lp)
	return r
}

// mediaPlayPause presses the keyboard's play/pause media key (Spotify and every other player listen to it).
func mediaPlayPause() {
	const vkMediaPlayPause, keyUp = 0xB3, 0x2
	call(pKeybdEvent, vkMediaPlayPause, 0, 0, 0)
	call(pKeybdEvent, vkMediaPlayPause, 0, keyUp, 0)
	logf("click on the record: play/pause")
}

/* ---------- Ctrl+Alt+E ---------- */

func registerHotkey() {
	const modAlt, modCtrl, modNoRepeat = 0x1, 0x2, 0x4000
	if call(pRegisterHotKey, mainHwnd, 1, modAlt|modCtrl|modNoRepeat, 'E') == 0 {
		logf("Ctrl+Alt+E is used by another program — arrange mode via tray / double-click instead")
	}
	if call(pRegisterHotKey, mainHwnd, 2, modAlt|modCtrl|modNoRepeat, 'F') == 0 {
		logf("Ctrl+Alt+F is used by another program — full screen via the tray / settings instead")
	}
}

/* ---------- the "Arrange mode — Done" bar ---------- */

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
	call(gpFillPath, g, brush(0xE6141419), path)
	strokePath(g, path, withAlpha(accent(), 0.8), 1.5)
	freePath(path)
	f := font("Segoe UI", 15*ui, true)
	fs := font("Segoe UI", 13*ui, false)
	drawText(g, f, "Arrange mode", float32(22*ui), float32(H)/2-f.size*0.75, float32(200*ui), 0xFFFFFFFF)
	drawText(g, fs, "drag to move · scroll or pull a corner to resize", float32(150*ui), float32(H)/2-fs.size*0.72, float32(440*ui), 0xC8FFFFFF)
	bw, bh := float32(96*ui), float32(H)-float32(16*ui)
	bx := float32(W) - bw - float32(10*ui)
	btn := roundRectPath(bx, float32(8*ui), bw, bh, bh/2)
	call(gpFillPath, g, brush(accent()|0xFF000000), btn)
	freePath(btn)
	ft := font("Segoe UI", 14*ui, true)
	tw, _ := measure(ft, "Done ✓")
	drawText(g, ft, "Done ✓", bx+(bw-tw)/2, float32(8*ui)+bh/2-ft.size*0.72, bw, 0xFF16120A)
	wa := workArea()
	banner.moveTo(int(wa.Left+wa.Right)/2-W/2, int(wa.Top)+int(18*ui))
	banner.alpha = 255
	banner.present()
	// the bar sits above normal windows so "Done" can always be clicked
	call(pSetWindowPos, banner.hwnd, ^uintptr(0), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE) // HWND_TOPMOST
}

// haloSquare: side of the visualizer square in pixels.
func haloSquare(scale float64) int { return int(460 * uiScale() * scale) }
