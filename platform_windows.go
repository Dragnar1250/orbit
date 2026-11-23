package main

// Windows-only pieces the shared widgets use: dates in the user's language, the list of screens.

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
	n := call(pGetDateFormatEx, 0, 0, uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(u16(picture))), uintptr(unsafe.Pointer(&buf[0])), 128, 0)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func formatDate(t time.Time, long bool) string {
	pic := "ddd, d MMM"
	if long {
		pic = "dddd, d MMMM"
	}
	if s := localDate(t, pic); s != "" {
		return s
	}
	return englishDate(t, long)
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
