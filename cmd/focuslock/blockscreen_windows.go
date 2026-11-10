//go:build windows

package main

import (
	"focuslock/internal/engine"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"focuslock/internal/judge"
)

// The block screen: what the overlay says about a blocked window, the
// "why do you need this?" box, and drawing it all with plain Win32 GDI.

// ---------- the block screen ----------

// ---------- explanations ----------

// submitExplanation hands the box's text to the engine; the verdict comes
// back in a later frame.
func submitExplanation() {
	if ui.frame.Explain.Busy || !ui.view.Explain {
		return
	}
	ui.eng.Explain(time.Now(), strings.TrimSpace(windowTitle(ui.edit)))
	tick()
}

// backToBlockedWindow gives focus back to the window under the overlay.
func backToBlockedWindow() {
	if ui.blockedHwnd != 0 {
		pSetForegroundWindow.Call(ui.blockedHwnd)
	}
}

// appTitleBarPx is left uncovered on non-browser apps (at 100% scaling).
const appTitleBarPx = 32

// showOverlay covers target, leaving topOffset px (at 100% scaling) free at the top.
func showOverlay(target uintptr, view engine.View, topOffset int) {
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

func layoutOverlay(w, h int32, v engine.View) overlayLayout {
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
		if ui.frame.Explain.Busy {
			btn, label = ui.brushEdit, "Checking…"
		}
		fill(l.button, btn)
		text(label, l.button, ui.fontBody, rgb(24, 24, 27), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		// Read live: it changes while the overlay has focus and tick isn't updating the view.
		status := ui.frame.Explain.Status
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
