package main

// Frames in step with the screen. Full screen asks for a frame right after each refresh of the
// monitor it is on (or every 2nd one on a 120/144 Hz screen at 60 fps), instead of from a clock
// of its own that slowly slides against the screen's — which shows as an uneven step now and
// then, even when every frame is ready in time. Animations then use the time of that refresh,
// so every frame is the same step further on.
//
// The wait is D3DKMTWaitForVerticalBlankEvent (the display driver's own refresh signal); if that
// isn't there, DwmFlush (the desktop compositor's frame); and if neither works, the old sleep.

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	dwmapi                = syscall.NewLazyDLL("dwmapi.dll")
	pDwmFlush             = dwmapi.NewProc("DwmFlush")
	pD3DKMTOpenAdapterHdc = gdi32.NewProc("D3DKMTOpenAdapterFromHdc")
	pD3DKMTWaitForVBlank  = gdi32.NewProc("D3DKMTWaitForVerticalBlankEvent")
	pMonitorFromWindowVs  = user32.NewProc("MonitorFromWindow")
	vsyncOn               atomic.Int32 // 1 while full screen is open
	vsyncAdapter          atomic.Uint64
	vsyncSource           atomic.Uint32
	vblankNs              atomic.Int64 // when the last refresh was (for the animations' clock)
	refreshNs             atomic.Int64 // the screen's refresh interval, measured
	vsyncMode             atomic.Int32 // 0 none, 1 driver, 2 compositor (written to the log)
	vsyncOpened           = map[uintptr][2]uint64{}
)

// vsyncStart: full screen opened on hwnd's monitor.
func vsyncStart(hwnd uintptr) {
	mon := call(pMonitorFromWindowVs, hwnd, 2)
	h, ok := vsyncOpened[mon]
	if !ok {
		dc := call(pGetDC, hwnd)
		var a struct {
			hdc     uintptr
			adapter uint32
			luid    [2]uint32
			source  uint32
		}
		a.hdc = dc
		if pD3DKMTOpenAdapterHdc.Find() == nil {
			if st, _, _ := pD3DKMTOpenAdapterHdc.Call(uintptr(unsafe.Pointer(&a))); st == 0 && a.adapter != 0 {
				h = [2]uint64{uint64(a.adapter), uint64(a.source)}
			}
		}
		call(pReleaseDC, hwnd, dc)
		vsyncOpened[mon] = h
	}
	vsyncAdapter.Store(h[0])
	vsyncSource.Store(uint32(h[1]))
	vblankNs.Store(0)
	vsyncOn.Store(1)
}

func vsyncStop() { vsyncOn.Store(0); vblankNs.Store(0) }

// waitRefresh waits for the next refresh: true if it really did (false: use the clock instead).
func waitRefresh() bool {
	start := time.Now()
	ok := false
	if ad := vsyncAdapter.Load(); ad != 0 {
		w := struct{ adapter, device, source uint32 }{uint32(ad), 0, vsyncSource.Load()}
		if st, _, _ := pD3DKMTWaitForVBlank.Call(uintptr(unsafe.Pointer(&w))); st == 0 {
			ok = true
			vsyncMode.Store(1)
		} else {
			vsyncAdapter.Store(0) // not on this PC: don't ask again
		}
	}
	if !ok && pDwmFlush.Find() == nil {
		if r, _, _ := pDwmFlush.Call(); r == 0 {
			ok = true
			vsyncMode.Store(2)
		}
	}
	el := time.Since(start)
	if !ok || el < 2*time.Millisecond {
		// came back at once (nothing to compose, or no signal): not a refresh — don't spin
		if el < 2*time.Millisecond {
			time.Sleep(time.Duration(max(int64(4e6), refreshNs.Load()/2)) - el)
		}
		return false
	}
	return true
}

// vsyncFrames is frameLoop while full screen is open; it returns when full screen closes.
func vsyncFrames() {
	last, lastPost := time.Time{}, time.Time{}
	misses := 0
	for vsyncOn.Load() == 1 {
		if misses > 30 { // no refresh signal here: the clock it is, for this time
			return
		}
		if !waitRefresh() {
			misses++
			continue
		}
		misses = 0
		now := time.Now()
		if !last.IsZero() {
			d := now.Sub(last).Nanoseconds()
			if d > 3e6 && d < 60e6 { // one refresh (4–60 ms): keep a steady estimate
				r := refreshNs.Load()
				if r == 0 {
					r = d
				}
				refreshNs.Store(r + (d-r)/16)
			}
		}
		last = now
		vblankNs.Store(now.UnixNano())
		// every k-th refresh, so the rate is close to what full screen asks for
		// A frame is asked for at the first refresh at least one frame-time (less half a
		// refresh) after the last one. A frame that took too long is not punished with a whole
		// extra frame-time: the very next refresh picks it up (at 144 Hz, 60 fps: 72 frames a
		// second, and a slow one costs 7 ms, not 28).
		fps := float64(atomic.LoadInt32(&fpsNow))
		per := float64(refreshNs.Load())
		if fps > 0 && per > 0 && !lastPost.IsZero() && float64(now.Sub(lastPost).Nanoseconds()) < 1e9/fps-per/2 {
			continue
		}
		if atomic.LoadInt32(&framePending) == 1 {
			continue // the last one is still being drawn: the next refresh
		}
		lastPost = now
		if atomic.CompareAndSwapInt32(&framePending, 0, 1) {
			call(pPostMessageW, mainHwnd, WM_APP+2, 0, 0)
		}
	}
}

// frameNow: the time this frame should show — the refresh it follows, when there is one.
func frameNow() time.Time {
	now := time.Now()
	if vb := vblankNs.Load(); vb > 0 && vsyncOn.Load() == 1 {
		if back := now.UnixNano() - vb; back >= 0 && back < 40e6 {
			return now.Add(-time.Duration(back)) // (keeps Go's steady clock)
		}
	}
	return now
}
