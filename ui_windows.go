//go:build windows

package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
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
	cfg  Config
	sess State
	cls  *Classifier
	save func() // writes accepted explanations to state.json
	inst uintptr

	appBar  uintptr
	overlay uintptr
	edit    uintptr // the "why do you need this?" box on the overlay
	abd     APPBARDATA

	fontBar, fontKicker, fontTitle, fontBody, fontSmall uintptr
	brushBar, brushOverlay, brushCard, brushEdit        uintptr
	brushAccent                                         map[Category]uintptr

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
	// being judged stays covered (Verdict.Hold) instead of flickering.
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

	rations  *Rations
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
	verdict          Verdict
	ration           *RationRule // set when it's covered by rationing, not the judge
}

// sideOverlay covers a blocked window on another monitor. It has no
// explain box: click it and the window comes to the front, where the main
// overlay (with the box) takes over.
type sideOverlay struct {
	hwnd, target uintptr
	view         overlayView
	lay          overlayLayout
	size         [2]int32
	shown        bool
}

type explainOutcome struct {
	res ExplainResult
	err error
}

var ui uiState

func runUI(cfg Config, sess State, cls *Classifier, save func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	pSetProcessDPIAware.Call()
	ui = uiState{cfg: cfg, sess: sess, cls: cls, save: save, explainDone: make(chan explainOutcome, 1),
		rations: NewRations(cfg.Rations), sideLog: map[uintptr]string{}}
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
	ui.brushAccent = map[Category]uintptr{
		CatEntertainment: brush(244, 63, 94),  // rose
		CatOffTask:       brush(250, 204, 21), // amber
		CatMusic:         brush(56, 189, 248), // sky
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

	// 3. Judge the foreground window.
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
	isBrowser := contains(ui.cfg.Browsers, proc)
	tabURL := ""
	if isBrowser {
		tabURL = ui.urls.URL(fg)
	}
	key := proc + "|" + title + "|" + tabURL
	host, path := normalizeURL(tabURL)

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
	case CatEntertainment, CatOffTask:
		block = true
		ui.musicKey = ""
	case CatMusic:
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
	case CatPending:
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
		logf("blocked [%s/%s %.2f]: %s — %q %s", v.Source, v.Cat, v.Prob, proc, title, tabURL)
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
			event("Closed tab: " + shorten(title, 40))
			ui.blockedKey = ""
			hideOverlay()
			updateBar()
			return
		}
		view.Footer = "This tab closes in " + fmtRemaining(max(left, 0)) + ". Switch to a work tab to keep it open."
		if view.Explain {
			view.Footer = "This tab closes in " + fmtRemaining(max(left, 0)) + ". Typing an explanation pauses the countdown."
		}
	}
	showOverlay(fg, view, ui.cfg.OverlayTopOffset)
	updateBar()
}

// showRationBlock covers a rationed window between its check windows.
// No countdown and no Ctrl+W: the page stays where you left it.
func showRationBlock(fg uintptr, proc, title, tabURL, key, host string, isBrowser bool, st RationStatus, now time.Time) {
	if ui.blockedKey != key {
		ui.blockedKey, ui.blockedSince, ui.blockedHost = key, now, host
		ui.blocked = blockedWindow{hwnd: fg, proc: proc, title: title, url: tabURL, ration: st.Rule}
		ui.blockCount++
		ui.explainStatus = ""
		pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		logf("rationed [%s, next %s, visit %d]: %s — %q %s", st.Rule.Name, st.NextAt.Format("15:04"), st.Visits, proc, title, tabURL)
	}
	ui.blocked.hwnd = fg
	offset := appTitleBarPx
	if isBrowser {
		offset = ui.cfg.OverlayTopOffset
	}
	showOverlay(fg, rationView(ui.blocked, st), offset)
}

// ---------- the block screen ----------

// overlayView is everything the block screen shows.
type overlayView struct {
	Accent  Category
	Kicker  string // "NOT PART OF YOUR TASK"
	Title   string // the page or window
	Where   string // "youtube.com · brave.exe"
	Why     string // who decided, and how sure
	Task    string
	Explain bool   // show the "why do you need this?" box
	Prompt  string // the line above the box
	Footer  string
	Stats   string
}

func blockView(b blockedWindow, musicOver bool) overlayView {
	v := b.verdict
	host, _ := normalizeURL(b.url)
	view := overlayView{Accent: v.Cat, Title: trimBrowserSuffix(b.title)}
	if view.Title == "" {
		view.Title = b.proc
	}
	view.Where = b.proc
	if host != "" {
		view.Where = host + "  ·  " + b.proc
	}

	judge := judgeDisplayName(v.Source)
	switch {
	case musicOver:
		view.Kicker = "MUSIC IS FINE — IN THE BACKGROUND"
		view.Why = fmt.Sprintf("Music can stay in front for %d seconds. It keeps playing if you switch away.", ui.cfg.Music.ForegroundGraceSeconds)
	case v.Cat == CatMusic:
		view.Kicker = "MUSIC IS OFF THIS SESSION"
		view.Why = `"music": {"allowed": false} in focus.json`
	case v.Source == "url":
		view.Kicker = "ON YOUR BLOCK LIST"
		view.Why = host + " is in blockDomains in focus.json."
	case v.Source == "rule":
		view.Kicker = "ON YOUR BLOCK LIST"
		view.Why = "The title matches a keyword in blockTitleKeywords in focus.json."
	case v.Source == "placeholder":
		view.Kicker = "HOME PAGES AND FEEDS ARE BLOCKED"
		view.Why = "Open a specific page instead. Pages on this site are checked one by one."
	case v.Cat == CatOffTask:
		view.Kicker = "NOT PART OF YOUR TASK"
		view.Why = fmt.Sprintf("%s judged this off-task, %.0f%% sure.", judge, v.Prob*100)
	default:
		view.Kicker = "THIS LOOKS LIKE ENTERTAINMENT"
		view.Why = fmt.Sprintf("%s judged this as entertainment, %.0f%% sure.", judge, v.Prob*100)
	}

	view.Task, view.Stats = taskLine(), statsLine()
	view.Explain = ui.cls.CanExplain(v)
	view.Prompt = "Need this for your task? Say why — Enter to send, Esc to go back."
	return view
}

func rationView(b blockedWindow, st RationStatus) overlayView {
	r := st.Rule
	host, _ := normalizeURL(b.url)
	view := overlayView{Accent: CatMusic, Title: trimBrowserSuffix(b.title), Where: b.proc}
	if host != "" {
		view.Where = host + "  ·  " + b.proc
	}
	view.Kicker = strings.ToUpper(r.Name) + " IS RATIONED"
	visits := fmt.Sprintf("You've gone to it %d times this session.", st.Visits)
	if wait := time.Until(st.NextAt); wait > 0 {
		view.Why = fmt.Sprintf("Next check at %s (in %d min). %s", st.NextAt.Format("15:04"), int(wait.Minutes())+1, visits)
	} else {
		view.Why = "A check is due: switch to it to start one. " + visits
	}
	view.Task, view.Stats = taskLine(), statsLine()
	_, can := ui.cls.explainBudget()
	view.Explain = can
	view.Prompt = fmt.Sprintf("Need it now? Say why — Enter opens it for %d minutes, Esc goes back.", r.ForMinutes)
	view.Footer = fmt.Sprintf("You get %d minutes every %d. Switch back to your task.", r.ForMinutes, r.EveryMinutes)
	return view
}

func taskLine() string {
	if t := ui.cls.Intent(); t != "" {
		return "Your task: " + t
	}
	return "No task set — only entertainment is blocked."
}

func statsLine() string {
	stats := fmt.Sprintf("%s of focus left  ·  %d blocked this session", fmtRemaining(ui.sess.Remaining()), ui.blockCount)
	if ui.cfg.Explain.Enabled {
		stats += fmt.Sprintf("  ·  %d of %d explanations left", ui.cls.ExplanationsLeft(), ui.cfg.Explain.MaxPerSession)
	}
	return stats
}

func judgeDisplayName(source string) string {
	switch source {
	case "jev":
		return "Jev"
	case "copilot":
		return "Copilot"
	case "llm":
		return "The local model"
	case "":
		return "The judge"
	}
	return strings.ToUpper(source[:1]) + source[1:]
}

// ---------- explanations ----------

func submitExplanation() {
	if ui.explainBusy || !ui.view.Explain {
		return
	}
	reason := strings.TrimSpace(windowTitle(ui.edit))
	if len([]rune(reason)) < 10 {
		ui.explainStatus = "Say a bit more: how does this help your task?"
		pInvalidateRect.Call(ui.overlay, 0, 1)
		return
	}
	if r := ui.blocked.ration; r != nil {
		// Opening email or chat early needs a reason, not a verdict: the
		// friction is the point, and it works without a judge.
		if _, ok := ui.cls.SpendExplanation(); !ok {
			ui.explainStatus = "No explanations left this session."
			pInvalidateRect.Call(ui.overlay, 0, 1)
			return
		}
		ui.rations.OpenEarly(r, reason)
		ui.save()
		event(fmt.Sprintf("%s open for %d min — %s", r.Name, r.ForMinutes, shorten(reason, 40)))
		pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		ui.explainStatus = ""
		ui.blockedKey, ui.blockedHost = "", ""
		hideOverlay()
		backToBlockedWindow()
		return
	}
	ui.explainBusy = true
	ui.explainStatus = "Checking your reason…"
	pEnableWindow.Call(ui.edit, 0)
	pInvalidateRect.Call(ui.overlay, 0, 1)
	b := ui.blocked
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := ui.cls.Explain(ctx, b.proc, b.title, b.url, reason)
		ui.explainDone <- explainOutcome{res, err}
	}()
}

// explainFinished runs on the UI thread when the judge has answered.
func explainFinished(o explainOutcome) {
	ui.explainBusy = false
	pEnableWindow.Call(ui.edit, 1)
	switch {
	case o.err == errNoExplanationsLeft:
		ui.explainStatus = "No explanations left this session."
	case o.err != nil:
		ui.explainStatus = "Couldn't check it (" + shorten(o.err.Error(), 60) + "). That one didn't count — try again."
	case o.res.Accepted:
		ui.save()
		event("Allowed " + shorten(o.res.Label, 40) + " — your reason was accepted")
		pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		ui.explainStatus = ""
		ui.blockedKey, ui.blockedHost = "", ""
		hideOverlay()
		backToBlockedWindow()
	default:
		ui.explainStatus = fmt.Sprintf("Not convinced (%.0f%% sure it's for your task). %d left — be specific about how it helps.",
			o.res.Prob*100, o.res.Left)
		ui.save() // the attempt counts
	}
	pInvalidateRect.Call(ui.overlay, 0, 1)
}

// backToBlockedWindow gives focus back to the window under the overlay.
func backToBlockedWindow() {
	if ui.blocked.hwnd != 0 {
		pSetForegroundWindow.Call(ui.blocked.hwnd)
	}
}

func event(s string) {
	ui.lastEvent, ui.lastEventAt = s, time.Now()
	logf("%s", s)
}

func barText() string {
	t := "FOCUS  ·  " + fmtRemaining(ui.sess.Remaining()) + " left  ·  ends " + ui.sess.End().Format("15:04")
	if task := ui.cls.Intent(); task != "" {
		t += "  ·  " + shorten(task, 50)
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

// appTitleBarPx is left uncovered on non-browser apps (at 100% scaling).
const appTitleBarPx = 32

// showOverlay covers target, leaving topOffset px (at 100% scaling) free at the top.
func showOverlay(target uintptr, view overlayView, topOffset int) {
	if isIconic(target) {
		hideOverlay()
		return
	}
	r := windowRect(target)
	// Leave the tab strip + address bar uncovered (scaled for display DPI).
	if r.Top < 0 {
		r.Top = 0 // maximized windows extend a few px above the screen
	}
	r.Top += int32(topOffset) * windowDPI(target) / 96
	if r.Bottom-r.Top < 100 {
		hideOverlay()
		return
	}
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if view != ui.view || [2]int32{w, h} != ui.overlaySize {
		ui.view, ui.overlaySize = view, [2]int32{w, h}
		ui.lay = layoutOverlay(w, h, view)
		e := ui.lay.edit
		pMoveWindow.Call(ui.edit, uintptr(e.Left), uintptr(e.Top), uintptr(e.Right-e.Left), uintptr(e.Bottom-e.Top), 1)
		if view.Explain {
			pShowWindow.Call(ui.edit, SW_SHOW)
		} else {
			pShowWindow.Call(ui.edit, SW_HIDE)
		}
		pInvalidateRect.Call(ui.overlay, 0, 1)
	}
	// Re-asserted every tick: follows moves/resizes and stays on top.
	pSetWindowPos.Call(ui.overlay, HWND_TOPMOST,
		uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h),
		SWP_NOACTIVATE|SWP_SHOWWINDOW)
	ui.overlayShown = true
}

// ---------- other monitors ----------

// checkOtherMonitors covers blocked windows that are in front on the
// monitors you're not working on: a video on the second screen used to go
// unchecked because only the focused window was judged. They're covered,
// never closed (keys only reach the focused window).
func checkOtherMonitors() {
	if ui.sess.Remaining() == 0 {
		return
	}
	fg := foregroundWindow()
	if fg == ui.overlay {
		fg = ui.blocked.hwnd // typing an explanation: its window counts as in front
	}
	fgMon, _, _ := pMonitorFromWindow.Call(fg, MONITOR_DEFAULTTONEAREST)
	n := 0
	for _, w := range frontWindows() {
		if w.monitor == fgMon || w.hwnd == fg {
			continue
		}
		view, offset, ok := judgeSideWindow(w.hwnd)
		if !ok {
			delete(ui.sideLog, w.hwnd)
			continue
		}
		if n == len(ui.side) {
			h := createWindow(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_LAYERED)
			pSetLayeredWindowAttributes.Call(h, 0, 248, LWA_ALPHA)
			ui.side = append(ui.side, &sideOverlay{hwnd: h})
		}
		showSide(ui.side[n], w.hwnd, view, offset)
		n++
	}
	for _, so := range ui.side[n:] {
		if so.shown {
			pShowWindow.Call(so.hwnd, SW_HIDE)
			so.shown = false
		}
	}
}

// judgeSideWindow decides whether a window on another monitor is covered.
// Music is fine there (it's in the background), and nothing is counted as
// a visit.
func judgeSideWindow(h uintptr) (overlayView, int, bool) {
	proc := processName(windowPID(h))
	title := windowTitle(h)
	isBrowser := contains(ui.cfg.Browsers, proc)
	url := ""
	offset := appTitleBarPx
	if isBrowser {
		url = ui.urls.URL(h)
		offset = ui.cfg.OverlayTopOffset
	}
	host, path := normalizeURL(url)
	b := blockedWindow{hwnd: h, proc: proc, title: title, url: url}
	logOnce := func(what string) {
		if k := what + "|" + title; ui.sideLog[h] != k {
			ui.sideLog[h] = k
			logf("%s on another monitor: %s — %q %s", what, proc, title, url)
		}
	}

	if rule := ui.rations.Match(proc, title, host, path); rule != nil {
		// On a side screen, email is visible only during a check window.
		st := ui.rations.Status(rule)
		if st.Open {
			return overlayView{}, 0, false
		}
		b.ration = rule
		view := rationView(b, st)
		view.Explain = false
		view.Footer = "On your other screen. Click to bring it here; a check starts if one is due."
		logOnce("rationed [" + rule.Name + "]")
		return view, offset, true
	}

	v := ui.cls.Classify(proc, title, url)
	if v.Cat != CatEntertainment && v.Cat != CatOffTask {
		return overlayView{}, 0, false
	}
	b.verdict = v
	view := blockView(b, false)
	view.Explain = false
	view.Footer = "On your other screen. Click it to explain, or switch it to something for your task."
	logOnce(fmt.Sprintf("blocked [%s/%s %.2f]", v.Source, v.Cat, v.Prob))
	return view, offset, true
}

func showSide(so *sideOverlay, target uintptr, view overlayView, topOffset int) {
	r := windowRect(target)
	r.Top += int32(topOffset) * windowDPI(target) / 96
	if r.Bottom-r.Top < 100 {
		return
	}
	w, h := r.Right-r.Left, r.Bottom-r.Top
	so.target = target
	if view != so.view || [2]int32{w, h} != so.size {
		so.view, so.size = view, [2]int32{w, h}
		so.lay = layoutOverlay(w, h, view)
		pInvalidateRect.Call(so.hwnd, 0, 1)
	}
	pSetWindowPos.Call(so.hwnd, HWND_TOPMOST, uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h),
		SWP_NOACTIVATE|SWP_SHOWWINDOW)
	so.shown = true
}

func sideFor(hwnd uintptr) *sideOverlay {
	for _, so := range ui.side {
		if so.hwnd == hwnd {
			return so
		}
	}
	return nil
}

type frontWindow struct{ hwnd, monitor uintptr }

var enumWindows []uintptr

func collectWindow(h, _ uintptr) uintptr {
	enumWindows = append(enumWindows, h)
	return 1 // keep going
}

// frontWindows returns, per monitor, the topmost real app window on it.
// EnumWindows lists windows top to bottom, so the first one seen on a
// monitor is the one you can see there.
func frontWindows() []frontWindow {
	enumWindows = enumWindows[:0]
	pEnumWindows.Call(ui.enumProc, 0)
	var out []frontWindow
	seen := map[uintptr]bool{}
	for _, h := range enumWindows {
		if !isAppWindow(h) {
			continue
		}
		m, _, _ := pMonitorFromWindow.Call(h, MONITOR_DEFAULTTONEAREST)
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, frontWindow{h, m})
	}
	return out
}

// isAppWindow: visible, not minimized, not ours, not a tool window,
// pop-up or hidden (cloaked) Store-app window, and big enough to matter.
func isAppWindow(h uintptr) bool {
	if h == ui.appBar || h == ui.overlay || sideFor(h) != nil {
		return false
	}
	if v, _, _ := pIsWindowVisible.Call(h); v == 0 || isIconic(h) {
		return false
	}
	if owner, _, _ := pGetWindow.Call(h, GW_OWNER); owner != 0 {
		return false
	}
	if ex, _, _ := pGetWindowLongPtrW.Call(h, GWL_EXSTYLE); ex&WS_EX_TOOLWINDOW != 0 {
		return false
	}
	var cloaked uint32
	pDwmGetWindowAttribute.Call(h, DWMWA_CLOAKED, uintptr(unsafe.Pointer(&cloaked)), 4)
	if cloaked != 0 {
		return false
	}
	r := windowRect(h)
	return r.Right-r.Left >= 200 && r.Bottom-r.Top >= 150 && windowTitle(h) != ""
}

func hideOverlay() {
	if ui.overlayShown {
		pShowWindow.Call(ui.overlay, SW_HIDE)
		ui.overlayShown = false
	}
}

// ---------- painting ----------

type rectI RECT

func (r rectI) contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

// overlayLayout: a card centred on the covered window.
type overlayLayout struct {
	card, accent                    rectI
	kicker, title, where, why, task rectI
	prompt, edit, button, status    rectI
	footer, stats                   rectI
}

func layoutOverlay(w, h int32, v overlayView) overlayLayout {
	var l overlayLayout
	cw := min(int32(760), w-32)
	pad := int32(28)
	x0 := (w - cw) / 2
	inner := func(y, height int32) rectI { return rectI{x0 + pad, y, x0 + cw - pad, y + height} }

	height := int32(22 + 8 + 80 + 26 + 12 + 24 + 16 + 56 + 20 + 26 + 24)
	if v.Explain {
		height += 24 + 6 + 38 + 6 + 24 + 16
	}
	height += pad * 2
	y := max(int32(16), (h-height)/2)
	l.card = rectI{x0, y, x0 + cw, y + height}
	l.accent = rectI{x0, y, x0 + 5, y + height}

	y += pad
	l.kicker = inner(y, 22)
	y += 22 + 8
	l.title = inner(y, 80) // up to two lines
	y += 80
	l.where = inner(y, 26)
	y += 26 + 12
	l.why = inner(y, 24)
	y += 24 + 16
	l.task = inner(y, 56)
	y += 56 + 20
	if v.Explain {
		l.prompt = inner(y, 24)
		y += 24 + 6
		row := inner(y, 38)
		l.edit = rectI{row.Left, row.Top, row.Right - 140, row.Bottom}
		l.button = rectI{row.Right - 128, row.Top, row.Right, row.Bottom}
		y += 38 + 6
		l.status = inner(y, 24)
		y += 24 + 16
	}
	l.footer = inner(y, 26)
	y += 26
	l.stats = inner(y, 24)
	return l
}

func paint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	pSetBkMode.Call(hdc, TRANSPARENT)

	if hwnd == ui.appBar {
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), ui.brushBar)
		pSelectObject.Call(hdc, ui.fontBar)
		pSetTextColor.Call(hdc, rgb(250, 204, 21))
		drawText(hdc, ui.barText, rc, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		return
	}

	v, l, main := ui.view, ui.lay, true
	if so := sideFor(hwnd); so != nil {
		v, l, main = so.view, so.lay, false
	}
	fill := func(r rectI, b uintptr) { pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), b) }
	text := func(s string, r rectI, font, color, flags uintptr) {
		pSelectObject.Call(hdc, font)
		pSetTextColor.Call(hdc, color)
		drawText(hdc, s, RECT(r), flags|DT_NOPREFIX)
	}
	const one = DT_LEFT | DT_SINGLELINE | DT_VCENTER | DT_END_ELLIPSIS
	const wrap = DT_LEFT | DT_WORDBREAK | DT_EDITCONTROL | DT_END_ELLIPSIS

	accent, ok := ui.brushAccent[v.Accent]
	if !ok {
		accent = ui.brushAccent[CatEntertainment]
	}
	accentColor := map[Category]uintptr{
		CatEntertainment: rgb(244, 63, 94), CatOffTask: rgb(250, 204, 21), CatMusic: rgb(56, 189, 248),
	}[v.Accent]
	if accentColor == 0 {
		accentColor = rgb(244, 63, 94)
	}

	fill(rectI(rc), ui.brushOverlay)
	fill(l.card, ui.brushCard)
	fill(l.accent, accent)

	text(v.Kicker, l.kicker, ui.fontKicker, accentColor, one)
	text(v.Title, l.title, ui.fontTitle, rgb(244, 244, 245), wrap)
	text(v.Where, l.where, ui.fontSmall, rgb(161, 161, 170), one)
	text(v.Why, l.why, ui.fontBody, rgb(212, 212, 216), one)
	text(v.Task, l.task, ui.fontBody, rgb(250, 204, 21), wrap)

	if v.Explain && main {
		text(v.Prompt, l.prompt, ui.fontSmall, rgb(161, 161, 170), one)
		btn, label := ui.brushAccent[CatOffTask], "Explain"
		if ui.explainBusy {
			btn, label = ui.brushEdit, "Checking…"
		}
		fill(l.button, btn)
		text(label, l.button, ui.fontBody, rgb(24, 24, 27), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		// Read live: it changes while the overlay has focus and tick isn't updating the view.
		status := ui.explainStatus
		statusColor := rgb(161, 161, 170)
		if strings.HasPrefix(status, "Not convinced") || strings.HasPrefix(status, "Couldn't") {
			statusColor = rgb(251, 146, 60)
		}
		text(status, l.status, ui.fontSmall, statusColor, one)
	}
	text(v.Footer, l.footer, ui.fontBody, rgb(228, 228, 231), one)
	text(v.Stats, l.stats, ui.fontSmall, rgb(113, 113, 122), one)
}

func drawText(hdc uintptr, s string, r RECT, flags uintptr) {
	p, _ := syscall.UTF16PtrFromString(s)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
}

func mkFont(height int32, weight int) uintptr {
	h, _, _ := pCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight),
		0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0,
		uintptr(unsafe.Pointer(u16("Segoe UI"))),
	)
	return h
}
