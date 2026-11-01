//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"focuslock/internal/classify"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/textutil"
)

// Other monitors: every tick, the window in front on each monitor you're
// not working on is checked too, and covered (never closed) if blocked.

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
	isBrowser := textutil.Contains(ui.cfg.Browsers, proc)
	url := ""
	offset := appTitleBarPx
	if isBrowser {
		url = ui.urls.URL(h)
		offset = ui.cfg.OverlayTopOffset
	}
	host, path := classify.NormalizeURL(url)
	b := blockedWindow{hwnd: h, proc: proc, title: title, url: url}
	logOnce := func(what string) {
		if k := what + "|" + title; ui.sideLog[h] != k {
			ui.sideLog[h] = k
			logx.Printf("%s on another monitor: %s — %q %s", what, proc, title, url)
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
	if v.Cat != judge.CatEntertainment && v.Cat != judge.CatOffTask {
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
