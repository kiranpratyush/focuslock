//go:build windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"focuslock/internal/config"
	"focuslock/internal/engine"
	"focuslock/internal/textutil"
)

// Windows as the engine's platform: Win32 for windows and processes, UI
// Automation for the tab's URL. The Qt app draws; this only says where.

// appTitleBarPx is left uncovered on non-browser apps (at 100% scaling).
const appTitleBarPx = 32

type winPlatform struct {
	cfg    config.Config
	urls   *urlReader
	uiProc string // the app's process name: its windows count as ours
	typing bool
}

var (
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	enumWindowsCallback            = syscall.NewCallback(collectWindow)
)

func newDesk(cfg config.Config, ui string) desk {
	// Per-monitor DPI awareness: window rectangles in real pixels on every
	// monitor, which is what the app places its block screens by.
	if pSetProcessDpiAwarenessContext.Find() == nil {
		pSetProcessDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	} else {
		pSetProcessDPIAware.Call()
	}
	name := strings.ToLower(filepath.Base(ui))
	if name == "." || name == "" {
		name = uiExeName
	}
	return &winPlatform{cfg: cfg, urls: newURLReader(), uiProc: name}
}

func hwndID(h uintptr) string { return strconv.FormatUint(uint64(h), 16) }

func hwndOf(id string) uintptr {
	n, _ := strconv.ParseUint(id, 16, 64)
	return uintptr(n)
}

func (p *winPlatform) describe(h uintptr) engine.Window {
	w := engine.Window{ID: hwndID(h), Proc: processName(windowPID(h)), Title: windowTitle(h)}
	if textutil.Contains(p.cfg.Browsers, w.Proc) {
		w.URL = p.urls.URL(h)
	}
	return w
}

func (p *winPlatform) ActiveWindow() (engine.Window, bool) {
	fg := foregroundWindow()
	if fg == 0 {
		return engine.Window{}, false
	}
	if p.typing || processName(windowPID(fg)) == p.uiProc {
		// The block screen has the keyboard (you're explaining), or the
		// start window is open.
		return engine.Window{ID: hwndID(fg), Own: true}, true
	}
	return p.describe(fg), true
}

// OtherFrontWindows: the topmost app window on each monitor other than the
// one holding active.
func (p *winPlatform) OtherFrontWindows(active string) []engine.Window {
	a := hwndOf(active)
	if a == 0 {
		a = foregroundWindow()
	}
	aMon, _, _ := pMonitorFromWindow.Call(a, MONITOR_DEFAULTTONEAREST)
	var out []engine.Window
	for _, w := range p.frontWindows() {
		if w.monitor == aMon || w.hwnd == a {
			continue
		}
		out = append(out, p.describe(w.hwnd))
	}
	return out
}

func (p *winPlatform) KillByName(names []string) []string { return killByName(names) }

// CloseTab sends Ctrl+W, which reaches the focused window: the engine only
// closes the tab in front.
func (p *winPlatform) CloseTab(engine.Window) { sendCtrlW() }

func (p *winPlatform) setTyping(on bool) { p.typing = on }

// place covers the window in absolute physical pixels, leaving its top free
// (a browser's tabs and address bar, an app's title bar) scaled to its
// monitor's DPI.
func (p *winPlatform) place(o engine.Overlay) *wireOverlay {
	h := hwndOf(o.Window.ID)
	if h == 0 || isIconic(h) {
		return nil
	}
	r := windowRect(h)
	if r.Top < 0 {
		r.Top = 0 // maximized windows extend a few px above the screen
	}
	top := appTitleBarPx
	if o.Cover == engine.CoverBrowser {
		top = p.cfg.OverlayTopOffset
	}
	r.Top += int32(top) * windowDPI(h) / 96
	if r.Bottom-r.Top < 100 {
		return nil
	}
	out := placed(o)
	out.Absolute = true
	out.Monitor = monitorName(h)
	out.X, out.Y, out.W, out.H = int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top)
	return out
}

// monitorName is the device name of h's monitor (\\.\DISPLAY1), for logs.
func monitorName(h uintptr) string {
	m, _, _ := pMonitorFromWindow.Call(h, MONITOR_DEFAULTTONEAREST)
	var mi struct {
		Size          uint32
		Monitor, Work RECT
		Flags         uint32
		Device        [32]uint16
	}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(mi.Device[:])
}

// ---------- other monitors ----------

type frontWindow struct{ hwnd, monitor uintptr }

var enumWindows []uintptr

func collectWindow(h, _ uintptr) uintptr {
	enumWindows = append(enumWindows, h)
	return 1 // keep going
}

// frontWindows returns, per monitor, the topmost real app window on it.
// EnumWindows lists windows top to bottom, so the first one seen on a
// monitor is the one you can see there.
func (p *winPlatform) frontWindows() []frontWindow {
	enumWindows = enumWindows[:0]
	pEnumWindows.Call(enumWindowsCallback, 0)
	var out []frontWindow
	seen := map[uintptr]bool{}
	for _, h := range enumWindows {
		if !p.isAppWindow(h) {
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
func (p *winPlatform) isAppWindow(h uintptr) bool {
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
	if processName(windowPID(h)) == p.uiProc {
		return false
	}
	r := windowRect(h)
	return r.Right-r.Left >= 200 && r.Bottom-r.Top >= 150 && windowTitle(h) != ""
}

// ---------- the pipe ----------

var unsafePipeChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// socketAddress is a named pipe per user. The app opens it by its short
// name (Qt's QLocalSocket adds \\.\pipe\).
func socketAddress(string) (listen, app string) {
	name := "focuslock-engine-" + unsafePipeChars.ReplaceAllString(os.Getenv("USERNAME"), "_")
	return `\\.\pipe\` + name, name
}

// listenLocal opens the pipe to this user only.
func listenLocal(addr string) (net.Listener, error) {
	cfg := &winio.PipeConfig{}
	if tu, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		cfg.SecurityDescriptor = "D:P(A;;GA;;;" + tu.User.Sid.String() + ")"
	}
	return winio.ListenPipe(addr, cfg)
}
