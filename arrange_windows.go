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

/* ---------- live move / resize ---------- */

/* ---------- dragging inside arrange mode ---------- */

func cursorPos() POINT {
	var pt POINT
	call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
	return pt
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
