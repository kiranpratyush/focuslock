//go:build windows

package main

import (
	"focuslock/internal/engine"
	"unsafe"
)

// Other monitors: every tick, the window in front on each monitor you're
// not working on is checked too, and covered (never closed) if blocked.

// sideOverlay covers a blocked window on another monitor. It has no
// explain box: click it and the window comes to the front, where the main
// overlay (with the box) takes over.
type sideOverlay struct {
	hwnd, target uintptr
	view         engine.View
	lay          overlayLayout
	size         [2]int32
	shown        bool
}

// showSides covers the engine's blocked windows on other monitors, reusing
// overlay windows, and hides the ones left over.
func showSides(sides []engine.Overlay) {
	n := 0
	for _, o := range sides {
		if n == len(ui.side) {
			h := createWindow(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_LAYERED)
			pSetLayeredWindowAttributes.Call(h, 0, 248, LWA_ALPHA)
			ui.side = append(ui.side, &sideOverlay{hwnd: h})
		}
		showSide(ui.side[n], hwndOf(o.Window.ID), o.View, coverOffset(o.Cover))
		n++
	}
	for _, so := range ui.side[n:] {
		if so.shown {
			pShowWindow.Call(so.hwnd, SW_HIDE)
			so.shown = false
		}
	}
}

func showSide(so *sideOverlay, target uintptr, view engine.View, topOffset int) {
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
	if isOurs(h) {
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
