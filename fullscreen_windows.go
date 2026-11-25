package main

// Full screen: Cinema — one ordinary window over a whole monitor (see cinema_windows.go).
// Esc, F11 or a double-click close it.

import (
	"math"
	"math/rand"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	pBitBlt           = gdi32.NewProc("BitBlt")
	pGdiFlush         = gdi32.NewProc("GdiFlush")
	msimg32           = syscall.NewLazyDLL("msimg32.dll")
	pAlphaBlend       = msimg32.NewProc("AlphaBlend")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pMonitorFromPoint = user32.NewProc("MonitorFromPoint")
	pSetFocus         = user32.NewProc("SetFocus")
	pValidateRect     = user32.NewProc("ValidateRect")
)

const (
	fsWS_VISIBLE      = 0x10000000
	fsWS_EX_TOPMOST   = 0x00000008
	fsCS_DBLCLKS      = 0x0008
	fsWM_PAINT        = 0x000F
	fsWM_ERASEBKGND   = 0x0014
	fsWM_ACTIVATE     = 0x0006
	fsWM_KEYDOWN      = 0x0100
	fsWM_SYSKEYDOWN   = 0x0104
	fsWM_MOUSEMOVE    = 0x0200
	fsWM_LBUTTONDBL   = 0x0203
	fsWM_LBUTTONUP    = 0x0202
	fsWM_LBUTTONDOWN  = 0x0201
	fsWM_RBUTTONUP    = 0x0205
	fsWM_MOUSEWHEEL   = 0x020A
	fsWM_SETCURSOR    = 0x0020
	fsHWND_NOTOPMOST  = ^uintptr(1) // -2
	fsSWP_NOMOVESIZE  = 0x0001 | 0x0002 | 0x0010
	fsMONITOR_NEAREST = 2
)

var (
	fsClass      = u16("OrbitLyricsFullScreen")
	fsProcCB     uintptr // set on first use (a package-level NewCallback(fsProc) would be an init cycle)
	fsRegistered bool
)

/* ---------- open / close ---------- */

// fsMonitor: the screen under the mouse, or the main one.
func fsMonitor() RECT {
	mons, prim := monitors()
	if cfg.FsScreen == 0 {
		var pt POINT
		call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
		h := call(pMonitorFromPoint, uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, fsMONITOR_NEAREST)
		mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
		if h != 0 && call(pGetMonitorInfoW, h, uintptr(unsafe.Pointer(&mi))) != 0 {
			return mi.Monitor
		}
	}
	for i, m := range mons {
		if prim[i] {
			return m
		}
	}
	if len(mons) > 0 {
		return mons[0]
	}
	return RECT{0, 0, int32(call(pGetSystemMetrics, 0)), int32(call(pGetSystemMetrics, 1))}
}

func openFullscreen() {
	if editMode {
		setEditMode(false)
	}
	setHover("")
	if !fsRegistered {
		fsProcCB = syscall.NewCallback(fsProc)
		cursor := call(pLoadCursorW, 0, 32512)
		wc := WNDCLASSEXW{Style: fsCS_DBLCLKS, LpfnWndProc: fsProcCB, HInstance: hInstance, LpszClassName: fsClass, HCursor: cursor,
			HIcon: appIcon(int(call(pGetSystemMetrics, 11))), HIconSm: appIcon(int(call(pGetSystemMetrics, 49)))} // the taskbar / Alt+Tab icon
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		call(pRegisterClassExW, uintptr(unsafe.Pointer(&wc)))
		fsRegistered = true
	}
	m := fsMonitor()
	W, H := int(m.Right-m.Left), int(m.Bottom-m.Top)
	f := &FullView{winW: W, winH: H, mon: m, q: 1, last: time.Now(), ver: -1}
	if !f.cinemaAlloc() {
		f.free()
		logf("full screen: out of memory for %dx%d", W, H)
		return
	}
	f.lyr = &LyricsView{w: offscreenWidget("lyrics"), version: -1, idx: -2, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	f.vin = &VinylView{w: offscreenWidget("vinyl"), last: time.Now(), version: -1, sx: 1, sy: 1, armT: 0, swapT: -1}
	f.mouseAt = time.Now()
	fs = f
	// an ordinary window: Windows shows what we draw without blending the whole screen
	f.hwnd = call(pCreateWindowExW, fsWS_EX_TOPMOST, uintptr(unsafe.Pointer(fsClass)), uintptr(unsafe.Pointer(u16("Orbit — Cinema"))),
		WS_POPUP|fsWS_VISIBLE, uintptr(m.Left), uintptr(m.Top), uintptr(W), uintptr(H), 0, 0, hInstance, 0)
	if f.hwnd == 0 {
		logf("full screen: CreateWindowEx failed")
		fs = nil
		f.free()
		return
	}
	// a background app may only take the focus right after a key press: tap Alt first
	call(pKeybdEvent, 0x12, 0, 0, 0)
	call(pSetForegroundWindow, f.hwnd)
	call(pKeybdEvent, 0x12, 0, 2, 0)
	call(pSetFocus, f.hwnd)
	if glowView != nil {
		glowView.hideAll()
	}
	atomic.StoreInt32(&fpsNow, 60)
	vsyncStart(f.hwnd)
	setPriority(true)
	f.say("Press H for shortcuts")
	logf("full screen (Cinema) on %dx%d", W, H)
}

func closeFullscreen() {
	f := fs
	if f == nil {
		return
	}
	fs = nil
	// how smooth it was (for a report: Orbit's folder → orbit.log)
	logf("full screen closed: %dx%d, %.1f ms a frame, %.0f Hz screen, %d fps asked — %s — %s", f.winW, f.winH, f.cin.frameMs,
		1e9/math.Max(1, float64(refreshNs.Load())), cfg.FsFPS, frameStats(), profLine())
	vsyncStop()
	setPriority(false)
	if f.hwnd != 0 {
		call(pDestroyWindow, f.hwnd)
	}
	f.free()
	applyFPS()
	needRedraw, layoutDirty, styleDirty = true, true, true
	// hand the full-screen buffers (tens of MB on a big screen) straight back to Windows.
	// (Not trimWorkingSet: that pages the whole app out and the desktop stutters while it
	// comes back.)
	runtime.GC()
	debug.FreeOSMemory()
	logf("full screen closed")
}

func fsProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	f := fs
	if f == nil || f.hwnd != hwnd && f.hwnd != 0 {
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	switch msg {
	case fsWM_ERASEBKGND:
		return 1
	case fsWM_PAINT:
		f.cinemaPresent(true, nil)
		call(pValidateRect, hwnd, 0)
		return 0
	case fsWM_ACTIVATE: // let other windows in front while we are not the active one
		after := HWND_TOPMOST
		if wp&0xFFFF == 0 {
			after = fsHWND_NOTOPMOST
		}
		call(pSetWindowPos, hwnd, after, 0, 0, 0, 0, fsSWP_NOMOVESIZE)
		return 0
	case fsWM_KEYDOWN, fsWM_SYSKEYDOWN:
		if wp == 0x73 && msg == fsWM_SYSKEYDOWN { // Alt+F4
			closeFullscreen()
			return 0
		}
		f.cinemaKey(int(wp))
		return 0
	case fsWM_LBUTTONDOWN:
		x, y := f.at(lp)
		f.cinemaDown(float64(x), float64(y))
		return 0
	case fsWM_LBUTTONUP:
		f.cinemaUp()
		return 0
	case fsWM_LBUTTONDBL:
		x, y := f.at(lp)
		if f.cinemaDown(float64(x), float64(y)) {
			return 0 // a quick second click on a button is still a click
		}
		closeFullscreen()
		return 0
	case fsWM_RBUTTONUP:
		f.wake()
		showMenu()
		if fs != nil { // the menu took the focus: take it back
			call(pSetForegroundWindow, fs.hwnd)
		}
		return 0
	case fsWM_MOUSEWHEEL:
		f.wake()
		f.cinemaWheel(int(int16(uint16(wp>>16))) > 0)
		return 0
	case fsWM_MOUSEMOVE:
		pt := POINT{int32(int16(uint16(lp))), int32(int16(uint16(lp >> 16)))}
		if pt != f.mousePt { // Windows repeats WM_MOUSEMOVE without a real move
			f.mousePt = pt
			f.wake()
			x, y := f.at(lp)
			f.cinemaMove(float64(x), float64(y))
		}
		return 0
	case fsWM_SETCURSOR:
		if f.cursorOff {
			call(pSetCursor, 0)
			return 1
		}
		if f.cin.hover != cinHoverNone {
			call(pSetCursor, call(pLoadCursorW, 0, IDC_HAND))
			return 1
		}
	case WM_CLOSE:
		closeFullscreen()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// at: mouse position (window pixels in lp) in frame pixels.
func (f *FullView) at(lp uintptr) (float32, float32) {
	q := float32(f.q)
	return float32(int16(uint16(lp))) * q, float32(int16(uint16(lp>>16))) * q
}

/* ---------- every frame ---------- */

func (f *FullView) cinemaPresent(full bool, dirty []rectF) {
	c := &f.cin
	if c.buf == nil || f.hwnd == 0 {
		return
	}
	gdiFlush()
	dc := call(pGetDC, f.hwnd)
	if dc == 0 {
		return
	}
	defer call(pReleaseDC, f.hwnd, dc)
	const srccopy = 0x00CC0020
	if full {
		call(pBitBlt, dc, 0, 0, uintptr(c.buf.w), uintptr(c.buf.h), c.buf.dc, 0, 0, srccopy)
		return
	}
	for _, r := range dirty {
		x0, y0 := int(math.Max(0, math.Floor(r.X))), int(math.Max(0, math.Floor(r.Y)))
		x1, y1 := int(math.Min(float64(c.buf.w), math.Ceil(r.right()))), int(math.Min(float64(c.buf.h), math.Ceil(r.bottom())))
		if x1 > x0 && y1 > y0 {
			call(pBitBlt, dc, uintptr(x0), uintptr(y0), uintptr(x1-x0), uintptr(y1-y0), c.buf.dc, uintptr(x0), uintptr(y0), srccopy)
		}
	}
}

// pix: a canvas' pixels as a Go slice (the DIB memory itself).
func pix(c *Canvas) []uint32 {
	if c == nil || c.bits == 0 {
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(c.bits)), c.w*c.h)
}

var gpDrawImageRectRect = gdiplus.NewProc("GdipDrawImageRectRect")

// withCfg on the UI thread while Cinema holds the settings runs at once instead of waiting
func init() { threadID = func() uint64 { return uint64(call(pGetCurrentThreadId)) } }

func fsCapture(h uintptr, on bool) {
	if on {
		call(pSetCapture, h)
	} else {
		call(pReleaseCapture)
	}
}

func fsInFront(f *FullView) bool { return call(pGetForegroundWnd) == f.hwnd }
func showCursor(on bool) {
	if on {
		call(pSetCursor, call(pLoadCursorW, 0, 32512))
	} else {
		call(pSetCursor, 0)
	}
}
