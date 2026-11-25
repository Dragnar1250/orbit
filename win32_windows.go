package main

// Thin Win32 bindings (user32, gdi32, kernel32, shell32, advapi32, ole32, combase, shlwapi, shcore).
// Only the standard library is used, so the exe builds without any extra packages.

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	combase  = syscall.NewLazyDLL("combase.dll")
	shlwapi  = syscall.NewLazyDLL("shlwapi.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")

	pRegisterClassExW            = user32.NewProc("RegisterClassExW")
	pCreateWindowExW             = user32.NewProc("CreateWindowExW")
	pDefWindowProcW              = user32.NewProc("DefWindowProcW")
	pGetMessageW                 = user32.NewProc("GetMessageW")
	pTranslateMessage            = user32.NewProc("TranslateMessage")
	pDispatchMessageW            = user32.NewProc("DispatchMessageW")
	pPostQuitMessage             = user32.NewProc("PostQuitMessage")
	pPostMessageW                = user32.NewProc("PostMessageW")
	pShowWindow                  = user32.NewProc("ShowWindow")
	pSetWindowPos                = user32.NewProc("SetWindowPos")
	pGetWindowLongPtrW           = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW           = user32.NewProc("SetWindowLongPtrW")
	pUpdateLayeredWindow         = user32.NewProc("UpdateLayeredWindow")
	pUpdateLayeredWindowIndirect = user32.NewProc("UpdateLayeredWindowIndirect")
	pGetDC                       = user32.NewProc("GetDC")
	pReleaseDC                   = user32.NewProc("ReleaseDC")
	pSetTimer                    = user32.NewProc("SetTimer")
	pLoadCursorW                 = user32.NewProc("LoadCursorW")
	pCreatePopupMenu             = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                 = user32.NewProc("AppendMenuW")
	pTrackPopupMenu              = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                 = user32.NewProc("DestroyMenu")
	pGetCursorPos                = user32.NewProc("GetCursorPos")
	pSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	pSystemParametersInfoW       = user32.NewProc("SystemParametersInfoW")
	pGetSystemMetrics            = user32.NewProc("GetSystemMetrics")
	pEnumWindows                 = user32.NewProc("EnumWindows")
	pGetWindowTextW              = user32.NewProc("GetWindowTextW")
	pGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	pIsWindowVisible             = user32.NewProc("IsWindowVisible")
	pSetProcessDpiAwarenessCtx   = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware          = user32.NewProc("SetProcessDPIAware")
	pGetWindowRect               = user32.NewProc("GetWindowRect")
	pMessageBoxW                 = user32.NewProc("MessageBoxW")
	pFindWindowW                 = user32.NewProc("FindWindowW")
	pDestroyIcon                 = user32.NewProc("DestroyIcon")
	pRegisterWindowMessageW      = user32.NewProc("RegisterWindowMessageW")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pDeleteDC           = gdi32.NewProc("DeleteDC")

	pGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW               = kernel32.NewProc("CreateMutexW")
	pGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	pGetLastError               = kernel32.NewProc("GetLastError")
	pOpenProcess                = kernel32.NewProc("OpenProcess")
	pCloseHandle                = kernel32.NewProc("CloseHandle")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	pSetProcessWorkingSetSize   = kernel32.NewProc("SetProcessWorkingSetSize")
	pGetCurrentProcess          = kernel32.NewProc("GetCurrentProcess")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")

	pRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")

	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")

	pRoInitialize              = combase.NewProc("RoInitialize")
	pRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	pWindowsCreateString       = combase.NewProc("WindowsCreateString")
	pWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	pWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")

	pSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")

	pCreateStreamOverRandomAccessStream = shcore.NewProc("CreateStreamOverRandomAccessStream")
)

const (
	WS_POPUP          = 0x80000000
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_NOACTIVATE  = 0x08000000

	GWL_EXSTYLE = -20

	WM_DESTROY           = 0x0002
	WM_CLOSE             = 0x0010
	WM_TIMER             = 0x0113
	WM_COMMAND           = 0x0111
	WM_NCHITTEST         = 0x0084
	WM_MOUSEWHEEL        = 0x020A
	WM_WINDOWPOSCHANGING = 0x0046
	WM_EXITSIZEMOVE      = 0x0232
	WM_MOUSEACTIVATE     = 0x0021
	WM_LBUTTONUP         = 0x0202
	WM_RBUTTONUP         = 0x0205
	WM_CONTEXTMENU       = 0x007B
	WM_APP               = 0x8000
	WM_DISPLAYCHANGE     = 0x007E
	WM_DPICHANGED        = 0x02E0
	WM_SETTINGCHANGE     = 0x001A
	WM_NCLBUTTONDBLCLK   = 0x00A3

	HTCAPTION         = 2
	MA_NOACTIVATE     = 3
	SW_SHOWNOACTIVATE = 4
	SW_HIDE           = 0

	SWP_NOSIZE        = 0x0001
	SWP_NOMOVE        = 0x0002
	SWP_NOZORDER      = 0x0004
	SWP_NOACTIVATE    = 0x0010
	SWP_SHOWWINDOW    = 0x0040
	SWP_NOOWNERZORDER = 0x0200

	HWND_BOTTOM    = 1
	HWND_TOPMOST   = ^uintptr(0)     // -1
	HWND_NOTOPMOST = ^uintptr(0) - 1 // -2

	ULW_ALPHA    = 2
	AC_SRC_OVER  = 0
	AC_SRC_ALPHA = 1

	MF_STRING       = 0x0
	MF_SEPARATOR    = 0x800
	MF_POPUP        = 0x10
	MF_CHECKED      = 0x8
	MF_GRAYED       = 0x1
	MF_DISABLED     = 0x2
	TPM_RIGHTBUTTON = 0x2
	TPM_RETURNCMD   = 0x100
	TPM_BOTTOMALIGN = 0x20

	NIM_ADD     = 0
	NIM_MODIFY  = 1
	NIM_DELETE  = 2
	NIF_MESSAGE = 1
	NIF_ICON    = 2
	NIF_TIP     = 4
	NIF_INFO    = 0x10

	SPI_GETWORKAREA    = 0x0030
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79

	HKEY_CURRENT_USER = 0x80000001
	KEY_READ          = 0x20019
	KEY_SET_VALUE     = 0x0002
	REG_SZ            = 1

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	ERROR_ALREADY_EXISTS              = 183
)

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type WINDOWPOS struct {
	Hwnd            uintptr
	HwndInsertAfter uintptr
	X, Y, CX, CY    int32
	Flags           uint32
}

type BLENDFUNCTION struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         GUID
	HBalloonIcon     uintptr
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

// call invokes a Windows API. go:uintptrescapes keeps every uintptr(unsafe.Pointer(&x)) argument
// alive and off the movable stack until the call returns — without it, Go could move or free
// the variable while Windows is still writing to it (random crashes after 20–40 s in 1.0–1.5).
//
//go:uintptrescapes
func call(p *syscall.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

// comCall calls method #slot on a COM / WinRT interface pointer.
//
//go:uintptrescapes
func comCall(obj uintptr, slot int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{obj}, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

func workArea() RECT {
	var r RECT
	call(pSystemParametersInfoW, SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&r)), 0)
	if r.Right <= r.Left {
		r = RECT{0, 0, 1920, 1080}
	}
	return r
}

func virtualScreen() RECT {
	x := int32(call(pGetSystemMetrics, SM_XVIRTUALSCREEN))
	y := int32(call(pGetSystemMetrics, SM_YVIRTUALSCREEN))
	w := int32(call(pGetSystemMetrics, SM_CXVIRTUALSCREEN))
	h := int32(call(pGetSystemMetrics, SM_CYVIRTUALSCREEN))
	return RECT{x, y, x + w, y + h}
}

func utf16PtrToString(p uintptr, n int) string {
	if p == 0 || n <= 0 {
		return ""
	}
	s := unsafe.Slice((*uint16)(unsafe.Pointer(p)), n)
	return syscall.UTF16ToString(s)
}

func messageBox(text, title string) {
	call(pMessageBoxW, 0, uintptr(unsafe.Pointer(u16(T(text)))), uintptr(unsafe.Pointer(u16(title))), 0x40)
}

/* ---------- Registry: start with Windows ---------- */

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func startupEnabled() bool {
	var h uintptr
	if call(pRegOpenKeyExW, HKEY_CURRENT_USER, uintptr(unsafe.Pointer(u16(runKey))), 0, KEY_READ, uintptr(unsafe.Pointer(&h))) != 0 {
		return false
	}
	defer call(pRegCloseKey, h)
	var typ, size uint32
	r := call(pRegQueryValueExW, h, uintptr(unsafe.Pointer(u16("OrbitLyrics"))), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	return r == 0
}

func setStartup(on bool, exePath string) {
	var h uintptr
	if call(pRegOpenKeyExW, HKEY_CURRENT_USER, uintptr(unsafe.Pointer(u16(runKey))), 0, KEY_SET_VALUE, uintptr(unsafe.Pointer(&h))) != 0 {
		logf("startup: cannot open Run key")
		return
	}
	defer call(pRegCloseKey, h)
	name := u16("OrbitLyrics")
	if on {
		val, _ := syscall.UTF16FromString(`"` + exePath + `"`)
		call(pRegSetValueExW, h, uintptr(unsafe.Pointer(name)), 0, REG_SZ, uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2))
	} else {
		call(pRegDeleteValueW, h, uintptr(unsafe.Pointer(name)))
	}
}

/* ---------- Process name of a window ---------- */

func windowProcessName(hwnd uintptr) string {
	var pid uint32
	call(pGetWindowThreadProcessId, hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	h := call(pOpenProcess, PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer call(pCloseHandle, h)
	buf := make([]uint16, 520)
	n := uint32(len(buf))
	if call(pQueryFullProcessImageNameW, h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))) == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func windowText(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n := call(pGetWindowTextW, hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// trimWorkingSet asks Windows to page out memory we are not using (keeps Task Manager RAM low).
func trimWorkingSet() {
	h := call(pGetCurrentProcess)
	call(pSetProcessWorkingSetSize, h, ^uintptr(0), ^uintptr(0))
}

var gwlExStyle = func() uintptr { v := int32(GWL_EXSTYLE); return uintptr(v) }()

func init() {
	systemLang = func() string {
		id, _, _ := kernel32.NewProc("GetUserDefaultUILanguage").Call()
		switch id & 0x3ff {
		case 0x1f:
			return "tr"
		case 0x19:
			return "ru"
		}
		return "en"
	}
}

// setPriority: on the desktop Orbit is decoration — it runs below normal priority, so a
// busy moment of its own never takes time from the mouse, games or other apps. Full screen
// is what you are watching: normal priority.
func setPriority(fullScreen bool) {
	cls := uintptr(0x4000) // BELOW_NORMAL_PRIORITY_CLASS
	if fullScreen {
		cls = 0x20 // NORMAL_PRIORITY_CLASS
	}
	cur, _, _ := kernel32.NewProc("GetCurrentProcess").Call()
	kernel32.NewProc("SetPriorityClass").Call(cur, cls)
}
