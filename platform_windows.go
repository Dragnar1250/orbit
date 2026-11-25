package main

// Windows-only parts of the shared widgets: dates, monitors, full-screen apps.

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	pGetDateFormatEx   = kernel32.NewProc("GetDateFormatEx")
	pEnumDisplayMons   = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	pMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	pGetForegroundWnd  = user32.NewProc("GetForegroundWindow")
	pGetClassNameW2    = user32.NewProc("GetClassNameW")
)

type systemTime struct{ Year, Month, DayOfWeek, Day, Hour, Minute, Second, Ms uint16 }

// localDate formats a date with Windows in the user's own language ("Pazartesi, 23 Kasım").
func localDate(t time.Time, picture string) string {
	st := systemTime{uint16(t.Year()), uint16(t.Month()), uint16(t.Weekday()), uint16(t.Day()), 0, 0, 0, 0}
	buf := make([]uint16, 128)
	n := call(pGetDateFormatEx, uintptr(unsafe.Pointer(u16(langLocale()))), 0, uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(u16(picture))), uintptr(unsafe.Pointer(&buf[0])), 128, 0)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

type monitorInfo struct {
	CbSize  uint32
	Monitor RECT
	Work    RECT
	Flags   uint32
}

var monList []RECT
var monPrimary []bool

var enumMonCB = syscall.NewCallback(func(hMon, hdc, rc, lp uintptr) uintptr {
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if call(pGetMonitorInfoW, hMon, uintptr(unsafe.Pointer(&mi))) != 0 {
		monList = append(monList, mi.Monitor)
		monPrimary = append(monPrimary, mi.Flags&1 != 0)
	}
	return 1
})

func monitors() ([]RECT, []bool) {
	monList, monPrimary = nil, nil
	call(pEnumDisplayMons, 0, 0, enumMonCB, 0)
	return monList, monPrimary
}

// fullscreenOn: is a full-screen app (game, video) in front on this monitor?
func fullscreenOn(m RECT) bool {
	fg := call(pGetForegroundWnd)
	if fg == 0 {
		return false
	}
	buf := make([]uint16, 64)
	call(pGetClassNameW2, fg, uintptr(unsafe.Pointer(&buf[0])), 64)
	if isDesktopClass(syscall.UTF16ToString(buf)) {
		return false
	}
	var r RECT
	call(pGetWindowRect, fg, uintptr(unsafe.Pointer(&r)))
	return r.Left <= m.Left && r.Top <= m.Top && r.Right >= m.Right && r.Bottom >= m.Bottom
}

func (w *Widget) capture(on bool) {
	if on {
		call(pSetCapture, w.hwnd)
	} else {
		call(pReleaseCapture)
	}
}
func releaseCapture() { call(pReleaseCapture) }

// raiseTop: above normal windows (the arranging bar, so "Done" can always be clicked)
func (w *Widget) raiseTop() {
	call(pSetWindowPos, w.hwnd, ^uintptr(0), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE) // HWND_TOPMOST
}

// ctrlDown: is a Ctrl key held?
func ctrlDown() bool { return call(pGetKeyState, 0x11)&0x8000 != 0 }

func mediaKey(vk uintptr) {
	call(pKeybdEvent, vk, 0, 0, 0)
	call(pKeybdEvent, vk, 0, 2, 0)
}

var pGetKeyState = user32.NewProc("GetKeyState")
