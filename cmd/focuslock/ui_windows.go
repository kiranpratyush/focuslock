//go:build windows

package main

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
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
	sess session.State
	cls  *classify.Classifier
	save func() // writes accepted explanations to state.json
	inst uintptr

	appBar  uintptr
	overlay uintptr
	edit    uintptr // the "why do you need this?" box on the overlay
	abd     APPBARDATA

	fontBar, fontKicker, fontTitle, fontBody, fontSmall uintptr
	brushBar, brushOverlay, brushCard, brushEdit        uintptr
	brushAccent                                         map[judge.Category]uintptr

	barText      string
	view         overlayView
	lay          overlayLayout
	overlayShown bool
	overlaySize  [2]int32

	blocked      blockedWindow
	blockedKey   string
	blockedSince time.Time
	blockCount   int
	// Site and view of the current block, so a strict site that's still
	// being judged stays covered (judge.Verdict.Hold) instead of flickering.
	blockedHost string
	musicKey    string
	musicSince  time.Time
	lastKill    time.Time
	lastTick    time.Time
	lastEvent   string
	lastEventAt time.Time

	explainBusy   bool
	explainStatus string
	explainDone   chan explainOutcome

	rations  *classify.Rations
	rationFg string // ration rule of the window in front last tick

	// Overlays for windows in front on your other monitors.
	side     []*sideOverlay
	sideLog  map[uintptr]string // last logged block per window, to log once
	enumProc uintptr
}

// blockedWindow is what's under the overlay right now.
type blockedWindow struct {
	hwnd             uintptr
	proc, title, url string
	verdict          judge.Verdict
	ration           *config.RationRule // set when it's covered by rationing, not the judge
}

type explainOutcome struct {
	res classify.ExplainResult
	err error
}

var ui uiState

func runUI(cfg config.Config, sess session.State, cls *classify.Classifier, save func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	pSetProcessDPIAware.Call()
	ui = uiState{cfg: cfg, sess: sess, cls: cls, save: save, explainDone: make(chan explainOutcome, 1),
		rations: classify.NewRations(cfg.Rations), sideLog: map[uintptr]string{}}
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

func tick() {
	now := time.Now()
	dt := now.Sub(ui.lastTick)
	if ui.lastTick.IsZero() || dt > 5*time.Second {
		dt = 0
	}
	ui.lastTick = now
	if ui.sess.Remaining() == 0 {
		hideOverlay()
		pPostQuitMessage.Call(0)
		return
	}

	// 1. Kill blocked apps wherever they are (not just in front).
	if now.Sub(ui.lastKill) >= killEvery {
		ui.lastKill = now
		list := ui.cfg.BlockedProcesses
		if !ui.cfg.Music.Allowed {
			list = append(append([]string{}, list...), ui.cfg.MusicProcesses...)
		}
		for _, name := range killByName(list) {
			event("Closed " + name)
		}
	}

	// Windows in front on your other monitors are checked too.
	defer checkOtherMonitors()

	// 2. An explanation came back from the judge.
	select {
	case o := <-ui.explainDone:
		explainFinished(o)
	default:
	}

	// 3. judge.Judge the foreground window.
	fg := foregroundWindow()
	// While you're typing an explanation, or the judge is reading it, the
	// tab's countdown stands still.
	if ui.blockedKey != "" && (fg == ui.overlay || ui.explainBusy) {
		ui.blockedSince = ui.blockedSince.Add(dt)
	}
	if fg == 0 || fg == ui.appBar || fg == ui.overlay {
		updateBar()
		return
	}
	proc := processName(windowPID(fg))
	title := windowTitle(fg)
	isBrowser := textutil.Contains(ui.cfg.Browsers, proc)
	tabURL := ""
	if isBrowser {
		tabURL = ui.urls.URL(fg)
	}
	key := proc + "|" + title + "|" + tabURL
	host, path := classify.NormalizeURL(tabURL)

	// Rationed (email, chat): covered between check windows. Switching to
	// it counts as a visit and opens a window if one is due.
	if rule := ui.rations.Match(proc, title, host, path); rule != nil {
		st := ui.rations.Visit(rule, ui.rationFg != rule.Name)
		ui.rationFg = rule.Name
		if st.Blocked {
			showRationBlock(fg, proc, title, tabURL, key, host, isBrowser, st, now)
			updateBar()
			return
		}
	} else {
		ui.rationFg = ""
	}

	v := ui.cls.Classify(proc, title, tabURL)

	block, holding := false, false
	musicOver := false
	switch v.Cat {
	case judge.CatEntertainment, judge.CatOffTask:
		block = true
		ui.musicKey = ""
	case judge.CatMusic:
		if !ui.cfg.Music.Allowed {
			block = true
			break
		}
		// Grace is per site, not per title: hopping from video to video
		// on the same site doesn't restart the 60 seconds.
		mk := proc + "|" + title
		if host != "" {
			mk = proc + "|" + host
		}
		if ui.musicKey != mk {
			ui.musicKey, ui.musicSince = mk, now
		}
		grace := time.Duration(ui.cfg.Music.ForegroundGraceSeconds) * time.Second
		if now.Sub(ui.musicSince) >= grace {
			block, musicOver = true, true
		}
	case judge.CatPending:
		if v.Hold && host != "" && host == ui.blockedHost {
			block, holding = true, true
		}
	default:
		ui.musicKey = ""
	}

	if !block {
		ui.blockedKey, ui.blockedHost = "", ""
		hideOverlay()
		updateBar()
		return
	}

	if !isBrowser && v.Source == "rule" {
		// An app on your blocked list: close it.
		for _, name := range killByName([]string{proc}) {
			event("Closed " + name)
		}
		hideOverlay()
		updateBar()
		return
	}

	if !holding && ui.blockedKey != key {
		ui.blockedKey, ui.blockedSince = key, now
		ui.blockedHost = host
		ui.blocked = blockedWindow{hwnd: fg, proc: proc, title: title, url: tabURL, verdict: v}
		ui.blockCount++
		ui.explainStatus = ""
		pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		logx.Printf("blocked [%s/%s %.2f]: %s — %q %s", v.Source, v.Cat, v.Prob, proc, title, tabURL)
	}
	ui.blocked.hwnd = fg

	view := blockView(ui.blocked, musicOver)
	if holding {
		view = ui.view
		view.Explain = false
		view.Footer = "Checking the new page…"
		showOverlay(fg, view, ui.cfg.OverlayTopOffset)
		updateBar()
		return
	}

	if !isBrowser {
		// An app the judge flagged: cover it but never kill it or send keys —
		// it might hold unsaved work. Its title bar stays free so you can
		// minimize or close it yourself.
		view.Footer = "Switch back to your task, or minimize this window."
		showOverlay(fg, view, appTitleBarPx)
		updateBar()
		return
	}

	view.Footer = "Switch to a work tab to continue."
	if closeAfter := time.Duration(ui.cfg.CloseTabAfterSeconds) * time.Second; closeAfter > 0 {
		left := closeAfter - now.Sub(ui.blockedSince)
		if left <= 0 && !ui.explainBusy {
			sendCtrlW()
			event("Closed tab: " + textutil.Shorten(title, 40))
			ui.blockedKey = ""
			hideOverlay()
			updateBar()
			return
		}
		view.Footer = "This tab closes in " + session.FmtRemaining(max(left, 0)) + ". Switch to a work tab to keep it open."
		if view.Explain {
			view.Footer = "This tab closes in " + session.FmtRemaining(max(left, 0)) + ". Typing an explanation pauses the countdown."
		}
	}
	showOverlay(fg, view, ui.cfg.OverlayTopOffset)
	updateBar()
}

func event(s string) {
	ui.lastEvent, ui.lastEventAt = s, time.Now()
	logx.Printf("%s", s)
}

func barText() string {
	t := "FOCUS  ·  " + session.FmtRemaining(ui.sess.Remaining()) + " left  ·  ends " + ui.sess.End().Format("15:04")
	if task := ui.cls.Intent(); task != "" {
		t += "  ·  " + textutil.Shorten(task, 50)
	}
	if ui.cls.JudgeName() == "" {
		t += "  ·  rules only"
	}
	if ui.lastEvent != "" && time.Since(ui.lastEventAt) < 10*time.Second {
		t += "  ·  " + ui.lastEvent
	}
	return t
}

func updateBar() {
	if t := barText(); t != ui.barText {
		ui.barText = t
		pInvalidateRect.Call(ui.appBar, 0, 1)
	}
}
