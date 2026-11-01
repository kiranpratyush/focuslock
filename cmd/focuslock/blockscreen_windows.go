//go:build windows

package main

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"focuslock/internal/classify"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/session"
	"focuslock/internal/textutil"
)

// The block screen: what the overlay says about a blocked window, the
// "why do you need this?" box, and drawing it all with plain Win32 GDI.

// showRationBlock covers a rationed window between its check windows.
// No countdown and no Ctrl+W: the page stays where you left it.
func showRationBlock(fg uintptr, proc, title, tabURL, key, host string, isBrowser bool, st classify.RationStatus, now time.Time) {
	if ui.blockedKey != key {
		ui.blockedKey, ui.blockedSince, ui.blockedHost = key, now, host
		ui.blocked = blockedWindow{hwnd: fg, proc: proc, title: title, url: tabURL, ration: st.Rule}
		ui.blockCount++
		ui.explainStatus = ""
		pSetWindowTextW.Call(ui.edit, uintptr(unsafe.Pointer(u16(""))))
		logx.Printf("rationed [%s, next %s, visit %d]: %s — %q %s", st.Rule.Name, st.NextAt.Format("15:04"), st.Visits, proc, title, tabURL)
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
	Accent  judge.Category
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
	host, _ := classify.NormalizeURL(b.url)
	view := overlayView{Accent: v.Cat, Title: classify.TrimBrowserSuffix(b.title)}
	if view.Title == "" {
		view.Title = b.proc
	}
	view.Where = b.proc
	if host != "" {
		view.Where = host + "  ·  " + b.proc
	}

	who := judgeDisplayName(v.Source)
	switch {
	case musicOver:
		view.Kicker = "MUSIC IS FINE — IN THE BACKGROUND"
		view.Why = fmt.Sprintf("Music can stay in front for %d seconds. It keeps playing if you switch away.", ui.cfg.Music.ForegroundGraceSeconds)
	case v.Cat == judge.CatMusic:
		view.Kicker = "MUSIC IS OFF THIS SESSION"
		view.Why = `"music": {"allowed": false} in focus.json`
	case v.Source == "url":
		view.Kicker = "ON YOUR BLOCK LIST"
		view.Why = host + " is in blockDomains in focus.json."
	case v.Source == "rule":
		view.Kicker = "ON YOUR BLOCK LIST"
		view.Why = "The title matches a keyword in blockTitleKeywords in focus.json."
	case v.Source == "unjudged":
		view.Kicker = "THE JUDGE ISN'T ANSWERING"
		view.Why = "Pages on this site stay covered until it's back. Other sites are allowed meanwhile."
	case v.Source == "placeholder":
		view.Kicker = "HOME PAGES AND FEEDS ARE BLOCKED"
		view.Why = "Open a specific page instead. Pages on this site are checked one by one."
	case v.Cat == judge.CatOffTask:
		view.Kicker = "NOT PART OF YOUR TASK"
		view.Why = fmt.Sprintf("%s judged this off-task, %.0f%% sure.", who, v.Prob*100)
	default:
		view.Kicker = "THIS LOOKS LIKE ENTERTAINMENT"
		view.Why = fmt.Sprintf("%s judged this as entertainment, %.0f%% sure.", who, v.Prob*100)
	}

	view.Task, view.Stats = taskLine(), statsLine()
	view.Explain = ui.cls.CanExplain(v)
	view.Prompt = "Need this for your task? Say why — Enter to send, Esc to go back."
	return view
}

func rationView(b blockedWindow, st classify.RationStatus) overlayView {
	r := st.Rule
	host, _ := classify.NormalizeURL(b.url)
	view := overlayView{Accent: judge.CatMusic, Title: classify.TrimBrowserSuffix(b.title), Where: b.proc}
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
	_, can := ui.cls.ExplainBudget()
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
	stats := fmt.Sprintf("%s of focus left  ·  %d blocked this session", session.FmtRemaining(ui.sess.Remaining()), ui.blockCount)
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
		event(fmt.Sprintf("%s open for %d min — %s", r.Name, r.ForMinutes, textutil.Shorten(reason, 40)))
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
	case o.err == classify.ErrNoExplanationsLeft:
		ui.explainStatus = "No explanations left this session."
	case o.err != nil:
		ui.explainStatus = "Couldn't check it (" + textutil.Shorten(o.err.Error(), 60) + "). That one didn't count — try again."
	case o.res.Accepted:
		ui.save()
		event("Allowed " + textutil.Shorten(o.res.Label, 40) + " — your reason was accepted")
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
		accent = ui.brushAccent[judge.CatEntertainment]
	}
	accentColor := map[judge.Category]uintptr{
		judge.CatEntertainment: rgb(244, 63, 94), judge.CatOffTask: rgb(250, 204, 21), judge.CatMusic: rgb(56, 189, 248),
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
		btn, label := ui.brushAccent[judge.CatOffTask], "Explain"
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
