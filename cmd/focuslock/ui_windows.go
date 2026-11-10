//go:build windows

package main

import (
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/engine"
	"focuslock/internal/judge"
	"focuslock/internal/session"
	"focuslock/internal/textutil"
)

// All window work happens on one OS thread (Win32 requirement).
// A 500ms timer drives the monitor loop from inside the message loop.

const (
	timerID     = 1
	tickMs      = 500
	appBarCbMsg = WM_APP + 1
	killEvery   = 2 * time.Second
	className   = "FocusLockWnd"
	editID      = 101
)

type uiState struct {
	urls *urlReader
	cfg  config.Config
	eng  *engine.Engine
	inst uintptr

	frame       engine.Frame // the last tick's
	blockedHwnd uintptr      // the window under the overlay
	lastBlockID int

	appBar  uintptr
	overlay uintptr
	edit    uintptr // the "why do you need this?" box on the overlay
	abd     APPBARDATA

	fontBar, fontKicker, fontTitle, fontBody, fontSmall uintptr
	brushBar, brushOverlay, brushCard, brushEdit        uintptr
	brushAccent                                         map[judge.Category]uintptr

	barText      string
	view         engine.View
	lay          overlayLayout
	overlayShown bool
	overlaySize  [2]int32

	// Overlays for windows in front on your other monitors.
	side     []*sideOverlay
	enumProc uintptr
}

var ui uiState

func runUI(cfg config.Config, sess session.State, cls *classify.Classifier, save func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	pSetProcessDPIAware.Call()
	ui = uiState{cfg: cfg}
	ui.eng = engine.New(cfg, sess, cls, winPlatform{}, save)
	ui.enumProc = syscall.NewCallback(collectWindow)
	ui.urls = newURLReader()
	ui.inst, _, _ = pGetModuleHandleW.Call(0)

	cursor, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc := WNDCLASSEXW{
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  ui.inst,
		Cursor:    cursor,
		ClassName: u16(className),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	ui.fontBar = mkFont(-16, 600)
	ui.fontKicker = mkFont(-14, 700)
	ui.fontTitle = mkFont(-30, 700)
	ui.fontBody = mkFont(-18, 400)
	ui.fontSmall = mkFont(-15, 400)
	ui.brushBar = brush(24, 24, 27)
	ui.brushOverlay = brush(12, 12, 16)
	ui.brushCard = brush(26, 26, 32)
	ui.brushEdit = brush(40, 40, 48)
	ui.brushAccent = map[judge.Category]uintptr{
		judge.CatEntertainment: brush(244, 63, 94),  // rose
		judge.CatOffTask:       brush(250, 204, 21), // amber
		judge.CatMusic:         brush(56, 189, 248), // sky
	}

	exStyle := uintptr(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE)
	ui.appBar = createWindow(exStyle)
	// The overlay can take focus (only when clicked) so you can type in it.
	ui.overlay = createWindow(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_LAYERED)
	pSetLayeredWindowAttributes.Call(ui.overlay, 0, 248, LWA_ALPHA)
	ui.edit, _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16("EDIT"))), 0,
		WS_CHILD|WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL,
		0, 0, 100, 30, ui.overlay, editID, ui.inst, 0)
	pSendMessageW.Call(ui.edit, WM_SETFONT, ui.fontBody, 1)
	pSendMessageW.Call(ui.edit, EM_LIMITTEXT, 300, 0)

	registerAppBar()
	ui.barText = barText()
	pShowWindow.Call(ui.appBar, SW_SHOWNOACTIVATE)

	pSetTimer.Call(ui.appBar, timerID, tickMs, 0)
	tick()

	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if msg.Hwnd == ui.edit && msg.Message == WM_KEYDOWN {
			switch msg.WParam {
			case VK_RETURN:
				submitExplanation()
				continue
			case VK_ESCAPE:
				backToBlockedWindow()
				continue
			}
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	pSHAppBarMessage.Call(ABM_REMOVE, uintptr(unsafe.Pointer(&ui.abd)))
	for _, so := range ui.side {
		pDestroyWindow.Call(so.hwnd)
	}
	pDestroyWindow.Call(ui.overlay)
	pDestroyWindow.Call(ui.appBar)
}

func brush(r, g, b byte) uintptr {
	h, _, _ := pCreateSolidBrush.Call(rgb(r, g, b))
	return h
}

func createWindow(exStyle uintptr) uintptr {
	h, _, _ := pCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(u16(className))),
		uintptr(unsafe.Pointer(u16("FocusLock"))),
		WS_POPUP,
		0, 0, 100, 100,
		0, 0, ui.inst, 0,
	)
	return h
}

// registerAppBar reserves a strip at the top of the screen, like the taskbar does.
func registerAppBar() {
	w, _, _ := pGetSystemMetrics.Call(SM_CXSCREEN)
	h := int32(ui.cfg.AppBarHeight)
	ui.abd = APPBARDATA{Hwnd: ui.appBar, CallbackMessage: appBarCbMsg, Edge: ABE_TOP}
	ui.abd.Size = uint32(unsafe.Sizeof(ui.abd))
	pSHAppBarMessage.Call(ABM_NEW, uintptr(unsafe.Pointer(&ui.abd)))

	ui.abd.Rc = RECT{Left: 0, Top: 0, Right: int32(w), Bottom: h}
	pSHAppBarMessage.Call(ABM_QUERYPOS, uintptr(unsafe.Pointer(&ui.abd)))
	ui.abd.Rc.Bottom = ui.abd.Rc.Top + h
	pSHAppBarMessage.Call(ABM_SETPOS, uintptr(unsafe.Pointer(&ui.abd)))

	rc := ui.abd.Rc
	pMoveWindow.Call(ui.appBar, uintptr(rc.Left), uintptr(rc.Top),
		uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top), 1)
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TIMER:
		tick()
		return 0
	case WM_PAINT:
		paint(hwnd)
		return 0
	case WM_MOUSEACTIVATE:
		if hwnd == ui.overlay && ui.view.Explain {
			return MA_ACTIVATE // click the overlay to type an explanation
		}
		return MA_NOACTIVATE // otherwise clicking our windows never steals focus
	case WM_ACTIVATE:
		if hwnd == ui.overlay && wParam&0xFFFF != 0 && ui.view.Explain {
			pSetFocus.Call(ui.edit)
		}
		return 0
	case WM_LBUTTONDOWN:
		if so := sideFor(hwnd); so != nil {
			pSetForegroundWindow.Call(so.target) // bring it to the front: the main overlay takes over
			return 0
		}
		if hwnd == ui.overlay {
			x, y := int32(int16(lParam&0xFFFF)), int32(int16(lParam>>16&0xFFFF))
			if ui.view.Explain && ui.lay.button.contains(x, y) {
				submitExplanation()
			} else if ui.view.Explain {
				pSetFocus.Call(ui.edit)
			}
		}
		return 0
	case WM_CTLCOLOREDIT:
		pSetTextColor.Call(wParam, rgb(244, 244, 245))
		pSetBkColor.Call(wParam, rgb(40, 40, 48))
		return ui.brushEdit
	case WM_CLOSE:
		return 0 // ignore Alt+F4 / close requests
	case WM_DESTROY:
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// ---------- the monitor loop ----------

// tick runs the engine and draws its frame.
func tick() {
	prev := ui.frame.Explain
	f := ui.eng.Tick(time.Now())
	ui.frame = f
	if f.Done {
		hideOverlay()
		showSides(nil)
		pPostQuitMessage.Call(0)
		return
	}

	if r := f.Explain.Released; r != nil {
		// The explanation was accepted: the block is gone.
		pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		hideOverlay()
		if h := hwndOf(r.ID); h != 0 {
			pSetForegroundWindow.Call(h)
		}
	}
	if f.Explain.Busy != prev.Busy {
		enable := uintptr(1)
		if f.Explain.Busy {
			enable = 0
		}
		pEnableWindow.Call(ui.edit, enable)
	}

	if o := f.Overlay; o != nil {
		if o.BlockID != ui.lastBlockID {
			ui.lastBlockID = o.BlockID
			pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		}
		ui.blockedHwnd = hwndOf(o.Window.ID)
		showOverlay(ui.blockedHwnd, o.View, coverOffset(o.Cover))
	} else {
		hideOverlay()
	}
	if ui.overlayShown && f.Explain != prev {
		pInvalidateRect.Call(ui.overlay, 0, 1)
	}
	showSides(f.Side)
	updateBar()
}

// coverOffset is how much of the window's top stays free (at 100% scaling):
// a browser's tab strip and address bar, an app's title bar.
func coverOffset(c engine.Cover) int {
	if c == engine.CoverBrowser {
		return ui.cfg.OverlayTopOffset
	}
	return appTitleBarPx
}

func barText() string {
	return "FOCUS  ·  " + session.FmtRemaining(ui.frame.Remaining) + " left  ·  " + ui.frame.Bar
}

func updateBar() {
	if t := barText(); t != ui.barText {
		ui.barText = t
		pInvalidateRect.Call(ui.appBar, 0, 1)
	}
}

// ---------- the platform ----------

// winPlatform is engine.Platform on Windows: Win32 for windows and
// processes, UI Automation for the tab's URL.
type winPlatform struct{}

func hwndID(h uintptr) string { return strconv.FormatUint(uint64(h), 16) }

func hwndOf(id string) uintptr {
	n, _ := strconv.ParseUint(id, 16, 64)
	return uintptr(n)
}

func isOurs(h uintptr) bool { return h == ui.appBar || h == ui.overlay || sideFor(h) != nil }

func describe(h uintptr) engine.Window {
	w := engine.Window{ID: hwndID(h), Proc: processName(windowPID(h)), Title: windowTitle(h)}
	if textutil.Contains(ui.cfg.Browsers, w.Proc) {
		w.URL = ui.urls.URL(h)
	}
	return w
}

func (winPlatform) ActiveWindow() (engine.Window, bool) {
	fg := foregroundWindow()
	if fg == 0 {
		return engine.Window{}, false
	}
	if isOurs(fg) {
		return engine.Window{ID: hwndID(fg), Own: true}, true
	}
	return describe(fg), true
}

// OtherFrontWindows: the topmost app window on each monitor other than the
// one holding active.
func (winPlatform) OtherFrontWindows(active string) []engine.Window {
	a := hwndOf(active)
	if a == 0 {
		a = foregroundWindow()
	}
	aMon, _, _ := pMonitorFromWindow.Call(a, MONITOR_DEFAULTTONEAREST)
	var out []engine.Window
	for _, w := range frontWindows() {
		if w.monitor == aMon || w.hwnd == a {
			continue
		}
		out = append(out, describe(w.hwnd))
	}
	return out
}

func (winPlatform) KillByName(names []string) []string { return killByName(names) }

// CloseTab sends Ctrl+W, which reaches the focused window: the engine only
// closes the tab in front.
func (winPlatform) CloseTab(engine.Window) { sendCtrlW() }
