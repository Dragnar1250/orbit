package main

// The music video behind full screen. Orbit has no browser of its own, so it borrows the one every
// Windows PC has: Microsoft Edge, opened as a bare app window (its own profile in Orbit's folder,
// muted, no tabs or bars) showing Orbit's video page. The window is tucked right under the
// full-screen window, which turns see-through where the video should show.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var pIsWindow = user32.NewProc("IsWindow")

type edgeVideo struct {
	cmd        *exec.Cmd
	hwnd       uintptr
	started    time.Time
	lastPlace  time.Time
	lastFind   time.Time
	placed     bool
	failed     bool
	notWanted  time.Time
	foundPID   uint32
	launchTime int
}

const videoTitle = "Orbit Video"

var (
	edgeFound    uintptr
	edgeEnumCB   uintptr
	edgeSearchFn = func(hwnd uintptr) bool {
		if windowText(hwnd) != videoTitle {
			return false
		}
		var buf [64]uint16
		n := call(pGetClassNameW, hwnd, uintptr(unsafe.Pointer(&buf[0])), 64)
		return syscall.UTF16ToString(buf[:n]) == "Chrome_WidgetWin_1"
	}
)

// edgePath: where Edge is installed (it ships with Windows 10 and 11).
func edgePath() string {
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
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
		if now.Sub(e.notWanted) > 5*time.Second { // switched to another background for a while: close it
			f.edgeStop()
		}
		f.videoOn = false
		return
	}
	e.notWanted = time.Time{}
	video.forSong(snap.Track)
	if e.failed {
		f.videoOn = false
		return
	}
	if e.cmd == nil {
		f.edgeStart()
	}
	if e.hwnd == 0 {
		if now.Sub(e.lastFind) > 300*time.Millisecond {
			e.lastFind = now
			edgeFound = 0
			if edgeEnumCB == 0 {
				edgeEnumCB = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
					if edgeSearchFn(hwnd) {
						edgeFound = hwnd
						return 0
					}
					return 1
				})
			}
			call(pEnumWindows, edgeEnumCB, 0)
			if edgeFound != 0 {
				e.hwnd = edgeFound
				logf("video window found after %.1fs", now.Sub(e.started).Seconds())
			} else if now.Sub(e.started) > 25*time.Second {
				logf("video: Edge's window never appeared — full screen keeps its normal background")
				e.failed = true
				f.edgeStop()
				e.failed = true
			}
		}
	} else if call(pIsWindow, e.hwnd) == 0 { // closed by someone: open it again once
		logf("video window closed — reopening")
		e.hwnd, e.placed = 0, false
		f.edgeStop()
		e.launchTime++
		if e.launchTime > 2 {
			e.failed = true
		}
	} else if now.Sub(e.lastPlace) > 500*time.Millisecond {
		e.lastPlace = now
		f.edgePlace()
	}
	// see-through only while the video really plays (otherwise the normal background stays)
	f.videoOn = e.hwnd != 0 && e.placed && now.Sub(seen) < 2500*time.Millisecond
}

func (f *FullView) edgeStart() {
	e := &f.edge
	exe := edgePath()
	if exe == "" || settingsSrv == nil {
		logf("video: Microsoft Edge not found — no music videos")
		e.failed = true
		return
	}
	u := fmt.Sprintf("http://127.0.0.1:%d/video?t=%s", settingsSrv.port, settingsSrv.token)
	prof := filepath.Join(appDir(), "edge-video")
	_ = os.MkdirAll(prof, 0o755)
	cmd := exec.Command(exe,
		"--app="+u,
		"--user-data-dir="+prof,
		"--no-first-run", "--no-default-browser-check", "--noerrdialogs",
		"--autoplay-policy=no-user-gesture-required", "--mute-audio",
		"--disable-features=CalculateNativeWinOcclusion,Translate,msEdgeTranslate,msUndersideButton,msEdgeSidebarV2",
		"--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling",
		"--hide-crash-restore-bubble", "--disable-sync",
		fmt.Sprintf("--window-position=%d,%d", f.mon.Left, f.mon.Top),
		fmt.Sprintf("--window-size=%d,%d", f.winW, f.winH))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		logf("video: can't start Edge: %v", err)
		e.failed = true
		return
	}
	e.cmd, e.started, e.hwnd, e.placed = cmd, time.Now(), 0, false
	go func() { _ = cmd.Wait() }()
	logf("video: Edge started (%s)", exe)
}

// edgePlace: borderless, off the taskbar, exactly over the screen, right under full screen.
func (f *FullView) edgePlace() {
	e := &f.edge
	h := e.hwnd
	if !e.placed {
		gwlStyle := func() uintptr { v := int32(-16); return uintptr(v) }()
		call(pSetWindowLongPtrW, h, gwlStyle, WS_POPUP|0x10000000)
		ex := call(pGetWindowLongPtrW, h, gwlExStyle)
		ex = (ex &^ 0x40000) | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE // no taskbar button, never takes the focus
		call(pSetWindowLongPtrW, h, gwlExStyle, ex)
		call(pShowWindow, h, 0) // hide + show: the taskbar forgets it
		call(pSetWindowPos, h, f.hwnd, uintptr(f.mon.Left), uintptr(f.mon.Top), uintptr(f.winW), uintptr(f.winH), SWP_NOACTIVATE|0x0020|SWP_SHOWWINDOW)
		call(pShowWindow, h, SW_SHOWNOACTIVATE)
		e.placed = true
		f.refocus()
		return
	}
	if call(pGetForegroundWnd) != f.hwnd && call(pGetForegroundWnd) != h { // another app in front: not on top of it
		call(pSetWindowPos, h, fsHWND_NOTOPMOST, 0, 0, 0, 0, fsSWP_NOMOVESIZE)
	}
	call(pSetWindowPos, h, f.hwnd, uintptr(f.mon.Left), uintptr(f.mon.Top), uintptr(f.winW), uintptr(f.winH), SWP_NOACTIVATE)
	if call(pGetForegroundWnd) == h { // a click went to it: keys belong to full screen
		f.refocus()
	}
}

func (f *FullView) refocus() {
	call(pKeybdEvent, 0x12, 0, 0, 0)
	call(pSetForegroundWindow, f.hwnd)
	call(pKeybdEvent, 0x12, 0, 2, 0)
}

func (f *FullView) edgeStop() {
	e := &f.edge
	if e.hwnd != 0 && call(pIsWindow, e.hwnd) != 0 {
		call(pPostMessageW, e.hwnd, WM_CLOSE, 0, 0)
	}
	if cmd := e.cmd; cmd != nil && cmd.Process != nil {
		p := cmd.Process
		go func() { // a moment to close by itself (no "restore pages?" next time), then for sure
			time.Sleep(3 * time.Second)
			_ = p.Kill()
		}()
	}
	failed, n := e.failed, e.launchTime
	*e = edgeVideo{failed: failed, launchTime: n}
	f.videoOn = false
}
