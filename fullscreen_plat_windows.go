package main

// Full screen on Windows: its layered window, the window messages, putting frames on screen.

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
	pInvalidateRect   = user32.NewProc("InvalidateRect")
)

const (
	fsWS_VISIBLE      = 0x10000000
	fsWS_EX_TOPMOST   = 0x00000008
	fsCS_DBLCLKS      = 0x0008
	fsSRCCOPY         = 0x00CC0020
	fsWM_PAINT        = 0x000F
	fsWM_ERASEBKGND   = 0x0014
	fsWM_ACTIVATE     = 0x0006
	fsWM_KEYDOWN      = 0x0100
	fsWM_SYSKEYDOWN   = 0x0104
	fsWM_MOUSEMOVE    = 0x0200
	fsWM_LBUTTONDBL   = 0x0203
	fsWM_LBUTTONUP    = 0x0202
	fsWM_LBUTTONDOWN  = 0x0201
	fsWM_CHAR         = 0x0102
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
	f := &FullView{winW: W, winH: H, mon: m, last: time.Now(), ver: -1, lyrIdx: -3, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	// big screens: draw at about 1080p and stretch — each pixel costs, and 4K has four times as many
	q := 1.0 // sharp first: only drawn smaller if this PC turns out too slow (see measure)
	if W*H > 3840*2160 {
		q = math.Sqrt(3840 * 2160 / float64(W*H)) // beyond 4K
	}
	if !f.setupBuffers(q) {
		f.free()
		logf("full screen: out of memory for %dx%d", W, H)
		return
	}
	f.vin = &VinylView{w: offscreenWidget("vinyl"), last: time.Now(), version: -1, sx: 1, sy: 1, armT: 0}
	f.hal = &HaloView{w: offscreenWidget("halo"), last: time.Now()}
	f.lyr = &LyricsView{w: offscreenWidget("lyrics"), version: -1, idx: -2, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	f.glow = newGlowView()
	f.glow.mons, f.glow.prim = []RECT{{0, 0, int32(f.W), int32(f.H)}}, []bool{true}
	f.stars = make([]fsStar, 180)
	for i := range f.stars {
		f.stars[i] = f.newStar(true)
	}
	f.mouseAt, f.hintAt = time.Now(), time.Now()
	fs = f
	f.hwnd = call(pCreateWindowExW, fsWS_EX_TOPMOST|WS_EX_LAYERED, uintptr(unsafe.Pointer(fsClass)), uintptr(unsafe.Pointer(u16("Orbit — Full screen"))),
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
	atomic.StoreInt32(&fpsNow, int32(cfg.FsFPS))
	logf("full screen on %dx%d", W, H)
}

func closeFullscreen() {
	f := fs
	if f == nil {
		return
	}
	fs = nil
	if f.hwnd != 0 {
		call(pDestroyWindow, f.hwnd)
	}
	f.edgeStop()
	video.Lock()
	video.Want = false
	video.Unlock()
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
		f.blit()
		call(pValidateRect, hwnd, 0)
		return 0
	case fsWM_ACTIVATE: // let other windows in front while we are not the active one
		after := HWND_TOPMOST
		if wp&0xFFFF == 0 {
			after = fsHWND_NOTOPMOST
		}
		call(pSetWindowPos, hwnd, after, 0, 0, 0, 0, fsSWP_NOMOVESIZE)
		if e := f.edge.hwnd; e != 0 { // the video window follows: never left on top of other apps
			if after == fsHWND_NOTOPMOST {
				call(pSetWindowPos, e, fsHWND_NOTOPMOST, 0, 0, 0, 0, fsSWP_NOMOVESIZE)
			}
			call(pSetWindowPos, e, hwnd, 0, 0, 0, 0, fsSWP_NOMOVESIZE)
		}
		return 0
	case fsWM_KEYDOWN, fsWM_SYSKEYDOWN:
		if wp == 0x73 && msg == fsWM_SYSKEYDOWN { // Alt+F4
			closeFullscreen()
			return 0
		}
		f.onKey(int(wp))
		return 0
	case fsWM_CHAR:
		f.onChar(rune(wp))
		return 0
	case fsWM_LBUTTONDOWN:
		f.onDown(f.at(lp))
		return 0
	case fsWM_LBUTTONUP:
		x, _ := f.at(lp)
		f.onUp(x)
		return 0
	case fsWM_LBUTTONDBL:
		f.onDouble(f.at(lp))
		return 0
	case fsWM_RBUTTONUP:
		f.onRight()
		return 0
	case fsWM_MOUSEWHEEL:
		d := int(int16(uint16(wp >> 16)))
		var pt POINT
		call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
		call(pScreenToClient, f.hwnd, uintptr(unsafe.Pointer(&pt)))
		x, y := f.at(uintptr(uint16(pt.X)) | uintptr(uint16(pt.Y))<<16)
		f.onWheel(x, y, d)
		return 0
	case fsWM_MOUSEMOVE:
		pt := POINT{int32(int16(uint16(lp))), int32(int16(uint16(lp >> 16)))}
		x, y := f.at(lp)
		f.onMove(pt, x, y)
		return 0
	case fsWM_SETCURSOR:
		if f.cursorOff {
			call(pSetCursor, 0)
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

// blend copies a premultiplied canvas onto the frame (GDI's AlphaBlend is much faster than GDI+ here).
func (f *FullView) blend(c *Canvas, x, y int, alpha byte) {
	if c == nil || alpha == 0 {
		return
	}
	bf := uintptr(0) | uintptr(alpha)<<16 | uintptr(1)<<24 // AC_SRC_OVER, constant alpha, AC_SRC_ALPHA
	call(pAlphaBlend, f.buf.dc, uintptr(int32(x)), uintptr(int32(y)), uintptr(c.w), uintptr(c.h), c.dc, 0, 0, uintptr(c.w), uintptr(c.h), bf)
	call(pGdiFlush)
}

// gpuUpscale: a frame drawn smaller is stretched by the window system (Mac) instead of the CPU.
const gpuUpscale = false

func (f *FullView) blit() {
	if f.buf == nil || f.hwnd == 0 {
		return
	}
	src := f.buf
	if f.out != nil { // drawn smaller than the screen: stretch it up on all cores
		scaleBilinear(pix(f.out), f.out.w, f.out.h, pix(f.buf), f.buf.w, f.buf.h)
		src = f.out
	}
	// a layered window: where the frame is see-through the music video behind it shows
	size := SIZE{int32(src.w), int32(src.h)}
	at := POINT{0, 0}
	blend := BLENDFUNCTION{AC_SRC_OVER, 0, 255, AC_SRC_ALPHA}
	call(pUpdateLayeredWindow, f.hwnd, 0, 0, uintptr(unsafe.Pointer(&size)), src.dc, uintptr(unsafe.Pointer(&at)), 0, uintptr(unsafe.Pointer(&blend)), ULW_ALPHA)
}

var pScreenToClient = user32.NewProc("ScreenToClient")
