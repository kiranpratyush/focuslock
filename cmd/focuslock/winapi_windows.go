//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"focuslock/internal/textutil"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pGetForegroundWindow          = user32.NewProc("GetForegroundWindow")
	pGetWindowTextW               = user32.NewProc("GetWindowTextW")
	pGetWindowThreadProcessId     = user32.NewProc("GetWindowThreadProcessId")
	pGetWindowRect                = user32.NewProc("GetWindowRect")
	pIsIconic                     = user32.NewProc("IsIconic")
	pSetWindowPos                 = user32.NewProc("SetWindowPos")
	pShowWindow                   = user32.NewProc("ShowWindow")
	pRegisterClassExW             = user32.NewProc("RegisterClassExW")
	pCreateWindowExW              = user32.NewProc("CreateWindowExW")
	pDefWindowProcW               = user32.NewProc("DefWindowProcW")
	pGetMessageW                  = user32.NewProc("GetMessageW")
	pTranslateMessage             = user32.NewProc("TranslateMessage")
	pDispatchMessageW             = user32.NewProc("DispatchMessageW")
	pPostQuitMessage              = user32.NewProc("PostQuitMessage")
	pDestroyWindow                = user32.NewProc("DestroyWindow")
	pSetTimer                     = user32.NewProc("SetTimer")
	pInvalidateRect               = user32.NewProc("InvalidateRect")
	pBeginPaint                   = user32.NewProc("BeginPaint")
	pEndPaint                     = user32.NewProc("EndPaint")
	pGetClientRect                = user32.NewProc("GetClientRect")
	pFillRect                     = user32.NewProc("FillRect")
	pDrawTextW                    = user32.NewProc("DrawTextW")
	pSendInput                    = user32.NewProc("SendInput")
	pGetSystemMetrics             = user32.NewProc("GetSystemMetrics")
	pSetLayeredWindowAttributes   = user32.NewProc("SetLayeredWindowAttributes")
	pSetProcessDPIAware           = user32.NewProc("SetProcessDPIAware")
	pLoadCursorW                  = user32.NewProc("LoadCursorW")
	pMoveWindow                   = user32.NewProc("MoveWindow")
	pSetWindowTextW               = user32.NewProc("SetWindowTextW")
	pEnableWindow                 = user32.NewProc("EnableWindow")
	pSetFocus                     = user32.NewProc("SetFocus")
	pSetForegroundWindow          = user32.NewProc("SetForegroundWindow")
	pSendMessageW                 = user32.NewProc("SendMessageW")
	pSetBkColor                   = gdi32.NewProc("SetBkColor")
	pCreateRoundRectRgn           = gdi32.NewProc("CreateRoundRectRgn")
	pCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection             = gdi32.NewProc("CreateDIBSection")
	pGetDC                        = user32.NewProc("GetDC")
	pReleaseDC                    = user32.NewProc("ReleaseDC")
	pUpdateLayeredWindow          = user32.NewProc("UpdateLayeredWindow")
	pSetWindowRgn                 = user32.NewProc("SetWindowRgn")
	pAllowSetForegroundWindow     = user32.NewProc("AllowSetForegroundWindow")
	pFindWindowW                  = user32.NewProc("FindWindowW")
	pPostMessageW                 = user32.NewProc("PostMessageW")
	pSHQueryUserNotificationState = shell32.NewProc("SHQueryUserNotificationState")
	pGetWindowTextLengthW         = user32.NewProc("GetWindowTextLengthW")
	pGetKeyState                  = user32.NewProc("GetKeyState")
	pGetDpiForSystem              = user32.NewProc("GetDpiForSystem")
	pEnumWindows                  = user32.NewProc("EnumWindows")
	pIsWindowVisible              = user32.NewProc("IsWindowVisible")
	pMonitorFromWindow            = user32.NewProc("MonitorFromWindow")
	pGetWindowLongPtrW            = user32.NewProc("GetWindowLongPtrW")
	pGetWindow                    = user32.NewProc("GetWindow")
	pDwmGetWindowAttribute        = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
	pCreateSolidBrush             = gdi32.NewProc("CreateSolidBrush")
	pCreateFontW                  = gdi32.NewProc("CreateFontW")
	pSelectObject                 = gdi32.NewProc("SelectObject")
	pSetTextColor                 = gdi32.NewProc("SetTextColor")
	pSetBkMode                    = gdi32.NewProc("SetBkMode")
	pSHAppBarMessage              = shell32.NewProc("SHAppBarMessage")
	pQueryFullProcessImageNameW   = kernel32.NewProc("QueryFullProcessImageNameW")
	pCreateMutexW                 = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW             = kernel32.NewProc("GetModuleHandleW")
)

const (
	WS_POPUP                          = 0x80000000
	WS_CHILD                          = 0x40000000
	WS_EX_TRANSPARENT                 = 0x00000020 // clicks pass through to the window below
	SWP_NOSIZE                        = 0x0001
	ULW_ALPHA                         = 0x2
	AC_SRC_OVER                       = 0x0
	AC_SRC_ALPHA                      = 0x1
	ASFW_ANY                          = 0xFFFFFFFF
	WS_VISIBLE                        = 0x10000000
	WS_CAPTION                        = 0x00C00000
	WS_SYSMENU                        = 0x00080000
	WS_VSCROLL                        = 0x00200000
	ES_MULTILINE                      = 0x0004
	ES_AUTOVSCROLL                    = 0x0040
	ES_WANTRETURN                     = 0x1000
	EM_SETSEL                         = 0x00B1
	SM_CYSCREEN                       = 1
	VK_TAB                            = 0x09
	VK_SHIFT                          = 0x10
	GWL_EXSTYLE                       = ^uintptr(19) // -20
	GW_OWNER                          = 4
	MONITOR_DEFAULTTONEAREST          = 2
	DWMWA_CLOAKED                     = 14
	WS_BORDER                         = 0x00800000
	WS_TABSTOP                        = 0x00010000
	ES_AUTOHSCROLL                    = 0x0080
	SW_SHOW                           = 5
	WM_ACTIVATE                       = 0x0006
	WM_SETFONT                        = 0x0030
	WM_KEYDOWN                        = 0x0100
	WM_LBUTTONDOWN                    = 0x0201
	WM_CTLCOLOREDIT                   = 0x0133
	EM_LIMITTEXT                      = 0x00C5
	MA_ACTIVATE                       = 1
	VK_RETURN                         = 0x0D
	VK_ESCAPE                         = 0x1B
	DT_LEFT                           = 0x0
	DT_NOPREFIX                       = 0x800
	DT_EDITCONTROL                    = 0x2000
	DT_END_ELLIPSIS                   = 0x8000
	WS_EX_TOPMOST                     = 0x00000008
	WS_EX_TOOLWINDOW                  = 0x00000080
	WS_EX_LAYERED                     = 0x00080000
	WS_EX_NOACTIVATE                  = 0x08000000
	SW_HIDE                           = 0
	SW_SHOWNOACTIVATE                 = 4
	SWP_NOACTIVATE                    = 0x0010
	SWP_SHOWWINDOW                    = 0x0040
	HWND_TOPMOST                      = ^uintptr(0) // (HWND)-1
	WM_DESTROY                        = 0x0002
	WM_PAINT                          = 0x000F
	WM_CLOSE                          = 0x0010
	WM_TIMER                          = 0x0113
	WM_MOUSEACTIVATE                  = 0x0021
	MA_NOACTIVATE                     = 3
	WM_APP                            = 0x8000
	LWA_ALPHA                         = 0x2
	DT_CENTER                         = 0x1
	DT_VCENTER                        = 0x4
	DT_SINGLELINE                     = 0x20
	DT_WORDBREAK                      = 0x10
	TRANSPARENT                       = 1
	SM_CXSCREEN                       = 0
	ABM_NEW                           = 0
	ABM_REMOVE                        = 1
	ABM_QUERYPOS                      = 2
	ABM_SETPOS                        = 3
	ABE_TOP                           = 1
	INPUT_KEYBOARD                    = 1
	KEYEVENTF_KEYUP                   = 0x2
	VK_CONTROL                        = 0x11
	VK_W                              = 0x57
	PROCESS_TERMINATE                 = 0x0001
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	SYNCHRONIZE                       = 0x00100000
	ERROR_ALREADY_EXISTS              = 183
)

type RECT struct{ Left, Top, Right, Bottom int32 }

type POINT struct{ X, Y int32 }

type SIZE struct{ CX, CY int32 }

type BITMAPINFOHEADER struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// BLENDFUNCTION tells UpdateLayeredWindow to use each pixel's own alpha.
type BLENDFUNCTION struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type WNDCLASSEXW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     RECT
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type APPBARDATA struct {
	Size            uint32
	Hwnd            uintptr
	CallbackMessage uint32
	Edge            uint32
	Rc              RECT
	LParam          uintptr
}

type keybdInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type keyInput struct {
	Type uint32
	Ki   keybdInput
	_    [8]byte // INPUT union is 32 bytes on 64-bit
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func foregroundWindow() uintptr {
	h, _, _ := pGetForegroundWindow.Call()
	return h
}

func windowTitle(h uintptr) string {
	buf := make([]uint16, 512)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func windowPID(h uintptr) uint32 {
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func windowRect(h uintptr) RECT {
	var r RECT
	pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func isIconic(h uintptr) bool { r, _, _ := pIsIconic.Call(h); return r != 0 }

var procNameCache = map[uint32]string{}

// processName returns the lowercase exe name for a pid, e.g. "chrome.exe".
func processName(pid uint32) string {
	if n, ok := procNameCache[pid]; ok {
		return n
	}
	h, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := pQueryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}
	name := strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:size])))
	if len(procNameCache) > 500 {
		procNameCache = map[uint32]string{}
	}
	procNameCache[pid] = name
	return name
}

// killByName terminates every running process whose exe name is in names.
// Returns the names it killed.
func killByName(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var killed []string
	for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		name := strings.ToLower(syscall.UTF16ToString(e.ExeFile[:]))
		if !textutil.Contains(names, name) {
			continue
		}
		h, err := syscall.OpenProcess(PROCESS_TERMINATE, false, e.ProcessID)
		if err != nil {
			continue
		}
		if syscall.TerminateProcess(h, 1) == nil {
			killed = append(killed, name)
		}
		syscall.CloseHandle(h)
	}
	return killed
}

// sendCtrlW closes the active browser tab.
func sendCtrlW() {
	in := []keyInput{
		{Type: INPUT_KEYBOARD, Ki: keybdInput{Vk: VK_CONTROL}},
		{Type: INPUT_KEYBOARD, Ki: keybdInput{Vk: VK_W}},
		{Type: INPUT_KEYBOARD, Ki: keybdInput{Vk: VK_W, Flags: KEYEVENTF_KEYUP}},
		{Type: INPUT_KEYBOARD, Ki: keybdInput{Vk: VK_CONTROL, Flags: KEYEVENTF_KEYUP}},
	}
	pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// waitForExit blocks until pid exits (returns at once if it's already gone).
func waitForExit(pid uint32) {
	h, err := syscall.OpenProcess(SYNCHRONIZE, false, pid)
	if err != nil {
		return // already gone
	}
	defer syscall.CloseHandle(h)
	syscall.WaitForSingleObject(h, syscall.INFINITE)
}

// singleInstance returns false if another process holds the named mutex.
func singleInstance(name string) bool {
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16(name))))
	if h == 0 {
		return false
	}
	return err != syscall.Errno(ERROR_ALREADY_EXISTS)
}

var pGetDpiForWindow = user32.NewProc("GetDpiForWindow")

// windowDPI returns the window's DPI (96 = 100% scaling).
func windowDPI(h uintptr) int32 {
	if pGetDpiForWindow.Find() != nil {
		return 96
	}
	d, _, _ := pGetDpiForWindow.Call(h)
	if d == 0 {
		return 96
	}
	return int32(d)
}
