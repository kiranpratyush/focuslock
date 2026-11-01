//go:build windows

package main

// The cue: while no session runs, a thin line breathes along the top of the
// screen with a few faint sparks drifting down from it, and every so often a
// pill slides down from the top edge: "Plan your next focus session".
// Clicking it opens the start window.
//
// When to remind is package cue's decision (internal/cue), and the sparks
// are package particles. This file only puts them on screen. Nothing here
// blocks anything: it's a nudge, not a lock.
//
// The line and sparks are one click-through window drawn pixel by pixel:
// each frame is painted into a buffer with its own transparency per pixel
// and handed to Windows with UpdateLayeredWindow. That's what lets each
// spark fade on its own.

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"focuslock/internal/config"
	"focuslock/internal/cue"
	"focuslock/internal/logx"
	"focuslock/internal/particles"
	"focuslock/internal/session"
)

const (
	cueClass      = "FocusLockCue"
	cueAnimTimer  = 1 // ~25 frames a second: the breathing line, the sliding pill
	cueCheckTimer = 2 // once a second: is a session running, is a reminder due?
	cuePillFor    = 20 * time.Second
)

type cueWindow struct {
	dir string
	cfg config.CueConfig
	st  cue.State

	strip, pill       uintptr // strip: the line and its sparks
	lineShown         bool
	stripW, stripH    int32
	lineH             int32
	memDC             uintptr  // the strip's frame buffer, as a Windows bitmap...
	pixels            []uint32 // ...and as Go sees it: BGRA, premultiplied
	sparks            *particles.Field
	lastFrame         time.Time
	pillW, pillH      int32
	pillY, pillTarget int32 // the slide: current and target top edge
	pillShownAt       time.Time
	pillVisible       bool
	pillText          string
	closeBox          rectI // the × on the pill
	font, fontClose   uintptr
	brushP            uintptr
	t0                time.Time // the breathing starts here
	cfgLoaded         time.Time
	wasActive         bool
}

var cw cueWindow

// runCue shows the cue until you sign out ("focuslock startup off" or
// closing it with WM_CLOSE ends it sooner).
// hello shows a pill right away ("startup on" uses it to show it works).
func runCue(dir string, cfg config.Config, hello bool) {
	if !singleInstance(`Local\FocusLockCue`) {
		return // already running
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pSetProcessDPIAware.Call()
	dpi := int32(96)
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d != 0 {
			dpi = int32(d)
		}
	}
	scale := func(v int32) int32 { return v * dpi / 96 }

	now := time.Now()
	cw = cueWindow{dir: dir, cfg: cfg.Cue, st: cue.State{Started: now}, t0: now, cfgLoaded: now}
	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, 32649) // IDC_HAND: the pill is clickable
	wc := WNDCLASSEXW{WndProc: syscall.NewCallback(cueProc), Instance: inst, Cursor: cursor, ClassName: u16(cueClass)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	cw.font = mkFont(-scale(16), 600)
	cw.fontClose = mkFont(-scale(18), 400)
	cw.brushP = brush(250, 204, 21) // the same amber as the session bar

	screenW, _, _ := pGetSystemMetrics.Call(SM_CXSCREEN)
	mk := func(exStyle uintptr, x, y, w, h int32) uintptr {
		hwnd, _, _ := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(u16(cueClass))), 0, WS_POPUP,
			uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, inst, 0)
		return hwnd
	}
	// The strip is click-through (WS_EX_TRANSPARENT): it never gets in the way.
	cw.stripW, cw.stripH, cw.lineH = int32(screenW), scale(56), scale(3)
	cw.strip = mk(WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE|WS_EX_LAYERED|WS_EX_TRANSPARENT,
		0, 0, cw.stripW, cw.stripH)
	cw.memDC, cw.pixels = newFrameBuffer(cw.stripW, cw.stripH)
	cw.sparks = particles.New(int(cw.stripW), int(cw.stripH), float64(dpi)/96, now.UnixNano())
	cw.pillW, cw.pillH = scale(400), scale(42)
	cw.pillY, cw.pillTarget = -cw.pillH, -cw.pillH
	cw.pill = mk(WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE|WS_EX_LAYERED,
		(int32(screenW)-cw.pillW)/2, cw.pillY, cw.pillW, cw.pillH)
	pSetLayeredWindowAttributes.Call(cw.pill, 0, 245, LWA_ALPHA)
	// Rounded ends: the window takes the shape of a pill.
	rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(cw.pillW+1), uintptr(cw.pillH+1), uintptr(cw.pillH), uintptr(cw.pillH))
	pSetWindowRgn.Call(cw.pill, rgn, 1)
	cw.closeBox = rectI{cw.pillW - scale(40), 0, cw.pillW, cw.pillH}

	pSetTimer.Call(cw.strip, cueAnimTimer, 40, 0)
	pSetTimer.Call(cw.strip, cueCheckTimer, 1000, 0)
	logx.Printf("cue started")
	cueCheck()
	if hello && !cw.st.SessionActive && !userBusy() {
		cueShowPill("The cue is on. I'll remind you to plan your sessions")
	}

	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	pDestroyWindow.Call(cw.pill)
	pDestroyWindow.Call(cw.strip)
	logx.Printf("cue stopped")
}

func cueProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TIMER:
		if wParam == cueCheckTimer {
			cueCheck()
		} else {
			cueAnimate()
		}
		return 0
	case WM_PAINT:
		cuePaint(hwnd)
		return 0
	case WM_MOUSEACTIVATE:
		return MA_NOACTIVATE // a click on the pill doesn't take focus from your work
	case WM_LBUTTONDOWN:
		if hwnd == cw.pill {
			x, y := int32(int16(lParam&0xFFFF)), int32(int16(lParam>>16&0xFFFF))
			cueHidePill()
			if !cw.closeBox.contains(x, y) {
				cueOpenStartWindow()
			}
		}
		return 0
	case WM_CLOSE:
		pPostQuitMessage.Call(0) // "focuslock startup off" sends this
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// cueCheck runs once a second: follow the session, ask package cue what to show.
func cueCheck() {
	now := time.Now()
	if now.Sub(cw.cfgLoaded) >= time.Minute {
		// Pick up edits to focus.json without restarting the cue.
		if cfg, err := config.Load(cw.dir); err == nil {
			cw.cfg = cfg.Cue
		}
		cw.cfgLoaded = now
	}
	_, active := session.ReadActive(cw.dir)
	if cw.wasActive && !active {
		cw.st.LastSessionEnd = now
	}
	cw.wasActive, cw.st.SessionActive = active, active

	d := cue.Decide(now, cw.st, cw.cfg)
	if userBusy() {
		// Presenting, a full-screen app, or Do Not Disturb: stay out of the
		// way. A due reminder waits until you're back (LastRemind isn't set).
		d = cue.Decision{}
	}
	if d.ShowLine != cw.lineShown {
		cw.lineShown = d.ShowLine
		if d.ShowLine {
			cw.lastFrame = time.Now()
			cueDrawStrip(0)
			pSetWindowPos.Call(cw.strip, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOACTIVATE|SWP_NOSIZE|SWP_SHOWWINDOW)
		} else {
			pShowWindow.Call(cw.strip, SW_HIDE)
		}
	}
	if d.RemindNow {
		cw.st.LastRemind = now
		cueShowPill(d.Message)
	}
	if cw.pillVisible && (active || now.Sub(cw.pillShownAt) > cuePillFor) {
		cueHidePill()
	}
}

func cueShowPill(text string) {
	cw.pillText, cw.pillVisible, cw.pillShownAt = text, true, time.Now()
	cw.pillTarget = 8 * cw.pillH / 42 // a little gap below the top edge
	pInvalidateRect.Call(cw.pill, 0, 1)
	logx.Printf("cue: %s", text)
}

func cueHidePill() {
	cw.pillVisible = false
	cw.pillTarget = -cw.pillH
}

// cueAnimate draws one frame: the line breathes, the pill eases toward its target.
func cueAnimate() {
	if cw.lineShown {
		now := time.Now()
		cueDrawStrip(now.Sub(cw.lastFrame).Seconds())
		cw.lastFrame = now
	}
	if cw.pillY != cw.pillTarget {
		step := (cw.pillTarget - cw.pillY) / 4 // ease out: big steps first, then small
		if step == 0 {
			step = cw.pillTarget - cw.pillY
		}
		cw.pillY += step
		var r RECT
		pGetWindowRect.Call(cw.pill, uintptr(unsafe.Pointer(&r)))
		pSetWindowPos.Call(cw.pill, HWND_TOPMOST, uintptr(r.Left), uintptr(cw.pillY), 0, 0,
			SWP_NOACTIVATE|SWP_NOSIZE|SWP_SHOWWINDOW)
		if cw.pillY == -cw.pillH {
			pShowWindow.Call(cw.pill, SW_HIDE) // fully slid away
		}
	}
}

// cueDrawStrip paints one frame of the line and sparks, dt seconds after
// the last one, and puts it on screen.
func cueDrawStrip(dt float64) {
	amber := particles.Color{R: 250, G: 204, B: 21}
	// The line breathes: a slow sine between faint and visible, every 4 seconds.
	t := time.Since(cw.t0).Seconds()
	alpha := (40 + 120*(0.5-0.5*math.Cos(2*math.Pi*t/4))) / 255
	particles.Clear(cw.pixels)
	particles.DrawLine(cw.pixels, int(cw.stripW), int(cw.lineH), amber, alpha)
	if cw.cfg.Particles {
		cw.sparks.Step(dt)
		cw.sparks.Draw(cw.pixels, amber)
	}
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	pos, src := POINT{0, 0}, POINT{0, 0}
	size := SIZE{cw.stripW, cw.stripH}
	blend := BLENDFUNCTION{BlendOp: AC_SRC_OVER, SourceConstantAlpha: 255, AlphaFormat: AC_SRC_ALPHA}
	pUpdateLayeredWindow.Call(cw.strip, screen, uintptr(unsafe.Pointer(&pos)), uintptr(unsafe.Pointer(&size)),
		cw.memDC, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ULW_ALPHA)
}

// newFrameBuffer makes a w×h 32-bit bitmap Go can draw into directly.
func newFrameBuffer(w, h int32) (memDC uintptr, pixels []uint32) {
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	memDC, _, _ = pCreateCompatibleDC.Call(screen)
	bi := BITMAPINFOHEADER{Width: w, Height: -h, Planes: 1, BitCount: 32} // negative height: rows top to bottom
	bi.Size = uint32(unsafe.Sizeof(bi))
	// Windows allocates the pixels and writes their address into bits; Go
	// then reads and writes that memory directly as a []uint32.
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(screen, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	pSelectObject.Call(memDC, bmp)
	return memDC, unsafe.Slice((*uint32)(bits), int(w*h))
}

func cuePaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), cw.brushP)
	pSetBkMode.Call(hdc, TRANSPARENT)
	pSetTextColor.Call(hdc, rgb(24, 24, 27))
	pSelectObject.Call(hdc, cw.font)
	text := RECT{rc.Left + cw.pillH/2, rc.Top, cw.closeBox.Left, rc.Bottom}
	drawText(hdc, "▶  "+cw.pillText, text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS)
	pSelectObject.Call(hdc, cw.fontClose)
	pSetTextColor.Call(hdc, rgb(90, 80, 30))
	drawText(hdc, "×", RECT(cw.closeBox), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

// userBusy asks Windows whether this is a bad moment to interrupt: the same
// check notifications use (presentation mode, full-screen apps and games,
// Do Not Disturb / quiet hours).
func userBusy() bool {
	var state uint32
	if r, _, _ := pSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state))); r != 0 {
		return false // can't tell: assume it's fine
	}
	const acceptsNotifications = 5 // QUNS_ACCEPTS_NOTIFICATIONS
	return state != acceptsNotifications
}

// cueOpenStartWindow runs "focuslock start", which shows the start window.
func cueOpenStartWindow() {
	exe, err := os.Executable()
	if err != nil {
		logx.Printf("cue: %v", err)
		return
	}
	// We just got the click, so Windows lets us hand focus on: without this
	// the start window would open behind whatever you were doing.
	pAllowSetForegroundWindow.Call(ASFW_ANY)
	c := exec.Command(exe, "start")
	c.Env = os.Environ()
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := c.Start(); err != nil {
		logx.Printf("cue: can't open the start window: %v", err)
		return
	}
	_ = c.Process.Release()
}
