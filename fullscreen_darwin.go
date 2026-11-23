//go:build darwin

package main

// Full screen on the Mac: the same FullView as on Windows (fullscreen.go, skinview.go); this is
// its window, input, the music video window and the player controls it uses.

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework WebKit
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
	f := &FullView{winW: W, winH: H, mon: m, last: time.Now(), ver: -1, lyrIdx: -3, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	// big screens: draw at about 1080p and stretch — each pixel costs, and 4K has four times as many
	q := 1.0
	if W*H > 1920*1200 { // Retina: about 1920×1200 worth of pixels, the GPU stretches the rest
		q = math.Sqrt(1920 * 1200 / float64(W*H))
	}
	f.maxQ = q
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
	f.hwnd = uintptr(C.fs_open(C.double(m.Left), C.double(m.Top), C.double(W), C.double(H)))
	if glowView != nil {
		glowView.hideAll()
	}
	for _, w := range widgets { // the desktop widgets sleep under full screen
		if w.visible && !w.offscreen {
			w.hide()
		}
	}
	atomic.StoreInt32(&fpsNow, int32(cfg.FsFPS))
	logf("full screen on %dx%d", W, H)
	buildMenu()
}

func closeFullscreen() {
	f := fs
	if f == nil {
		return
	}
	fs = nil
	C.fs_close()
	f.edgeStop()
	video.Lock()
	video.Want = false
	video.Unlock()
	f.free()
	applyFPS()
	needRedraw, layoutDirty, styleDirty = true, true, true
	runtime.GC()
	debug.FreeOSMemory()
	logf("full screen closed")
	buildMenu()
}

// gpuUpscale: on the Mac the layer stretches a smaller frame on the GPU (Retina screens have 3–6×
// the pixels of 1080p; drawing all of them in software is what made full screen heavy).
const gpuUpscale = true

func (f *FullView) blit() {
	if f.buf == nil || f.hwnd == 0 {
		return
	}
	C.fs_show(og(f.buf.g))
}

func (f *FullView) blend(c *Canvas, x, y int, alpha byte) {
	if c == nil || alpha == 0 || f.buf == nil {
		return
	}
	C.og_blend(og(f.buf.g), og(c.g), C.float(x), C.float(y), C.float(float64(alpha)/255))
}

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
	q := float32(f.q)
	fx, fy := float32(x)*q, float32(y)*q
	switch kind {
	case 1:
		f.onKey(int(data))
	case 2:
		f.onChar(rune(data))
	case 3:
		f.onDown(fx, fy)
	case 4:
		f.onUp(fx)
	case 5:
		f.onDouble(fx, fy)
	case 6:
		f.onRight()
	case 7:
		f.onWheel(fx, fy, int(data))
	case 8:
		f.onMove(POINT{int32(x), int32(y)}, fx, fy)
	case 9:
		closeFullscreen()
	}
	requestRedraw()
}

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

/* ---------- the music video ---------- */

type edgeVideo struct {
	open      bool
	notWanted time.Time
	started   time.Time
	failed    bool
}

func (f *FullView) videoTick(snap MediaSnap, want bool, now time.Time) {
	e := &f.edge
	video.Lock()
	video.Want = want
	seen := video.Seen
	video.Unlock()
	if !want {
		if e.notWanted.IsZero() {
			e.notWanted = now
		}
		if now.Sub(e.notWanted) > 5*time.Second {
			f.edgeStop()
		}
		f.videoOn = false
		return
	}
	e.notWanted = time.Time{}
	video.forSong(snap.Track)
	if !e.open && settingsSrv != nil {
		u := fmt.Sprintf("http://127.0.0.1:%d/video?t=%s", settingsSrv.port, settingsSrv.token)
		s := C.CString(u)
		C.fs_video_open(s)
		C.free(unsafe.Pointer(s))
		e.open, e.started = true, now
		logf("video: WebKit window opened")
	}
	// see-through only while the video really plays (otherwise the normal background stays)
	f.videoOn = e.open && C.fs_video_open_now() != 0 && now.Sub(seen) < 2500*time.Millisecond
}

func (f *FullView) edgeStop() {
	if f.edge.open {
		C.fs_video_close()
	}
	f.edge = edgeVideo{}
	f.videoOn = false
}

func (f *FullView) refocus() { C.fs_refocus() }

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
