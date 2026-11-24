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

func mediaKey(vk uintptr) {
	call(pKeybdEvent, vk, 0, 0, 0)
	call(pKeybdEvent, vk, 0, 2, 0)
}

// openURL: a link in the default app/browser.
func openURL(u string) bool {
	return call(pShellExecuteW, 0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(u))), 0, 0, 1) > 32
}

// popupMenu: a small menu at the mouse; returns the picked item (-1: none).
func popupMenu(items []string, checked int, owner uintptr) int {
	m := call(pCreatePopupMenu)
	for i, n := range items {
		menuItem(m, 1+i, n, checked == i)
	}
	var pt POINT
	call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
	id := int(call(pTrackPopupMenu, m, TPM_RIGHTBUTTON|TPM_RETURNCMD|TPM_BOTTOMALIGN, uintptr(pt.X), uintptr(pt.Y), 0, owner, 0))
	call(pDestroyMenu, m)
	return id - 1
}

func fsCapture(hwnd uintptr, on bool) {
	if on {
		call(pSetCapture, hwnd)
	} else {
		call(pReleaseCapture)
	}
}

// fsInFront: the full-screen window is the active one.
func fsInFront(f *FullView) bool { return call(pGetForegroundWnd) == f.hwnd }

func showCursor(on bool) {
	if on {
		call(pSetCursor, call(pLoadCursorW, 0, 32512))
	} else {
		call(pSetCursor, 0)
	}
}

// fsRefocus: after the menu took the focus, full screen takes it back.
func fsRefocus() {
	if fs != nil {
		call(pSetForegroundWindow, fs.hwnd)
	}
}

// otherDesktopWidgets: widgets of other apps on the desktop the lyrics should keep clear of (Mac only)
func otherDesktopWidgets() []RECT { return nil }

// The soft layer and GPU-turned pictures are the Mac's (it draws on four times the pixels of a
// Retina screen); Windows draws everything on the one canvas.
func softBegin(g uintptr) bool                                            { return false }
func softEnd(g uintptr)                                                   {}
func softBlur(g uintptr, r float32)                                       {}
func softClear(g uintptr)                                                 {}
func (w *Widget) useSoft(k float64, only bool)                            {}
func (w *Widget) gpuLayers() bool                                         { return false }
func (w *Widget) setBase(b *Bitmap, ver int)                              {}
func (w *Widget) setTop(b *Bitmap, ver int)                               {}
func (w *Widget) gpuMotion(sx, sy, cx, cy, glow float64)                  {}
func (w *Widget) rings(r, wd, a []float64, c uint32, cx, cy float64) bool { return false }
func (w *Widget) spin(b *Bitmap, ver int, cx, cy, deg float64)            {}
