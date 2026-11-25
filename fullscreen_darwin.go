//go:build darwin

package main

// Full screen (Cinema) on the Mac: the same view as on Windows (fullscreen.go, cinemaview.go);
// this is its window, its keys and mouse, and the player controls it uses.

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <stdlib.h>
#include "gdip_darwin.h"
#include "app_darwin.h"
#include "fullscreen_darwin.h"
*/
import "C"

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
)

// fsMonitor: the screen under the mouse, or the main one.
func fsMonitor() RECT {
	mons, prim := monitors()
	if cfg.FsScreen == 0 {
		pt := cursorPos()
		for _, m := range mons {
			if pt.X >= m.Left && pt.X < m.Right && pt.Y >= m.Top && pt.Y < m.Bottom {
				return m
			}
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
	return RECT{0, 0, 1920, 1080}
}

func openFullscreen() {
	if editMode {
		setEditMode(false)
	}
	setHover("")
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
	f.hwnd = uintptr(C.fs_open(C.double(m.Left), C.double(m.Top), C.double(W), C.double(H)))
	if glowView != nil {
		glowView.hideAll()
	}
	for _, w := range widgets { // the desktop widgets sleep under full screen
		if w.visible && !w.offscreen {
			w.hide()
		}
	}
	atomic.StoreInt32(&fpsNow, int32(fsRate()))
	f.say("Press H for shortcuts")
	logf("full screen (Cinema) on %dx%d", W, H)
	buildMenu()
}

// fsRate: Cinema's frames a second (60, the screen's own rate up to 120 on ProMotion, or 30).
func fsRate() int {
	switch cfg.FsFPS {
	case 30:
		return 30
	case 240:
		return 120
	}
	return 60
}

func closeFullscreen() {
	f := fs
	if f == nil {
		return
	}
	fs = nil
	C.fs_close()
	f.free()
	applyFPS()
	needRedraw, layoutDirty, styleDirty = true, true, true
	runtime.GC()
	debug.FreeOSMemory()
	logf("full screen closed")
	buildMenu()
}

// cinemaPresent: the finished frame to the screen (the layer shows it; nothing is drawn twice).
func (f *FullView) cinemaPresent(full bool, dirty []rectF) {
	if c := &f.cin; c.buf != nil && f.hwnd != 0 {
		C.fs_show(og(c.buf.g))
	}
}

// refreshNs, vsyncMode: for Cinema's log line (the Mac's frames come from its display link).
var refreshNs, vsyncMode atomic.Int64

// frameNow: the time this frame shows (the display link calls at each refresh)
func frameNow() time.Time { return time.Now() }

// goFSEvent: the full-screen window's keys and mouse. x, y: window pixels.
//
//export goFSEvent
func goFSEvent(kind C.int, x, y C.double, data C.int) {
	f := fs
	if f == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logf("full screen input panic: %v\n%s", r, debug.Stack())
		}
	}()
	fx, fy := float64(x), float64(y)
	switch kind {
	case 1:
		f.cinemaKey(int(data))
	case 3:
		f.wake()
		f.cinemaDown(fx, fy)
	case 4:
		f.cinemaUp()
	case 5:
		f.wake()
		if !f.cinemaDown(fx, fy) {
			closeFullscreen()
		}
	case 6:
		f.wake()
		showMenu()
	case 7:
		f.wake()
		f.cinemaWheel(data > 0)
	case 8:
		pt := POINT{int32(x), int32(y)}
		if pt != f.mousePt {
			f.mousePt = pt
			f.wake()
			f.cinemaMove(fx, fy)
		}
	case 9:
		closeFullscreen()
	}
	requestRedraw()
}

// ctrlDown: Control or Command held (Cinema's ⌃/⌘ + arrows: previous / next song)
func ctrlDown() bool { return C.os_ctrl_down() != 0 }

func fsInFront(f *FullView) bool   { return C.fs_in_front() != 0 }
func showCursor(on bool)           { C.fs_cursor(C.int(b2i(on))) }
func fsCapture(h uintptr, on bool) {}
func fsRefocus()                   { C.fs_refocus() }
func showMenu()                    { C.os_show_menu() }

func openURL(u string) bool {
	s := C.CString(u)
	defer C.free(unsafe.Pointer(s))
	return C.os_open_url(s) != 0
}

func popupMenu(items []string, checked int, owner uintptr) int {
	cs := make([]*C.char, len(items))
	for i, it := range items {
		cs[i] = C.CString(it)
	}
	defer func() {
		for _, c := range cs {
			C.free(unsafe.Pointer(c))
		}
	}()
	if len(cs) == 0 {
		return -1
	}
	arr := C.malloc(C.size_t(len(cs)) * C.size_t(unsafe.Sizeof(uintptr(0))))
	defer C.free(arr)
	sl := unsafe.Slice((**C.char)(arr), len(cs))
	copy(sl, cs)
	return int(C.os_popup((**C.char)(arr), C.int(len(cs)), C.int(checked)))
}

/* ---------- the player's controls (Spotify / Apple Music by AppleScript) ---------- */

func macPlayerApp() string {
	switch macApp {
	case "music":
		return "Music"
	case "spotify":
		return "Spotify"
	}
	if running("Spotify") {
		return "Spotify"
	}
	return "Music"
}

// mediaKey: the Windows media keys full screen presses, done on the Mac's player.
func mediaKey(vk uintptr) {
	switch vk {
	case vkMediaNext:
		macControl("next track")
	case vkMediaPrev:
		macControl("previous track")
	default:
		macControl("playpause")
	}
}

var smtcReady atomic.Bool // (Windows only; here the player is asked directly)

func requestSeek(sec float64) bool {
	media.Seek(sec)
	go osascript(fmt.Sprintf(`tell application "%s" to set player position to %s`, macPlayerApp(), strings.Replace(fmt.Sprintf("%.2f", math.Max(0, sec)), ",", ".", 1)))
	return true
}

func requestShuffle(on bool) bool {
	v := map[bool]int{true: 1, false: 0}[on]
	media.WantModes(v, -1)
	if macPlayerApp() == "Spotify" {
		go osascript(fmt.Sprintf(`tell application "Spotify" to set shuffling to %v`, on))
	} else {
		go osascript(fmt.Sprintf(`tell application "Music" to set shuffle enabled to %v`, on))
	}
	return true
}

// requestRepeat: 0 off, 1 this song, 2 the list (Spotify only knows on/off: both 1 and 2 are on).
func requestRepeat(mode int) bool {
	media.WantModes(-1, mode)
	if macPlayerApp() == "Spotify" {
		go osascript(fmt.Sprintf(`tell application "Spotify" to set repeating to %v`, mode != 0))
	} else {
		go osascript(`tell application "Music" to set song repeat to ` + []string{"off", "one", "all"}[mode])
	}
	return true
}

var _ = rand.Int
