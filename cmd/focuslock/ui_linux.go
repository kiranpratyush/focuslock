//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"syscall"

	"focuslock/internal/config"
	"focuslock/internal/engine"
)

// Hyprland as the engine's platform, and where its block screens go.

// hyprPlatform is engine.Platform on Hyprland. It remembers each window's
// geometry from ActiveWindow so the block screen can be placed over it.
type hyprPlatform struct {
	cfg  config.Config
	seen map[string]hyprWindow
	// typing: the explanation box on the block screen has keyboard focus.
	// It's a layer surface, not a window, so Hyprland still reports the
	// browser as active; the app says so instead.
	typing bool
}

func (p *hyprPlatform) ActiveWindow() (engine.Window, bool) {
	w, ok := activeWindow()
	if !ok {
		return engine.Window{}, false
	}
	p.seen = map[string]hyprWindow{w.Address: w}
	if p.typing {
		return engine.Window{ID: w.Address, Own: true}, true
	}
	// Linux has no UI Automation to read the address bar, so tabs are
	// judged by title; allowTitleKeywords applies as when a URL can't be read.
	return engine.Window{ID: w.Address, Proc: processName(w.Pid), Title: w.Title, Own: w.Class == "focuslock"}, true
}

// OtherFrontWindows isn't done on Hyprland yet.
func (p *hyprPlatform) OtherFrontWindows(string) []engine.Window { return nil }

func (p *hyprPlatform) KillByName(names []string) []string { return killByName(names) }

func (p *hyprPlatform) CloseTab(w engine.Window) { closeTab(w.ID) }

func (p *hyprPlatform) setTyping(on bool) { p.typing = on }

func newDesk(cfg config.Config, _ string) desk { return &hyprPlatform{cfg: cfg} }

// socketAddress is where the worker listens: the per-user runtime
// directory (private, and short enough for the 108-byte limit on socket
// paths), or the data directory without one. The app connects to the same
// path.
func socketAddress(dir string) (listen, app string) {
	p := filepath.Join(dir, "engine.sock")
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		p = filepath.Join(rt, "focuslock-engine.sock")
	}
	return p, p
}

func listenLocal(path string) (net.Listener, error) {
	_ = os.Remove(path)
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	return net.Listen("unix", path)
}

// place turns an overlay into a spot on a monitor. A browser keeps its tab
// strip uncovered (overlayTopOffset) so you can switch to a work tab;
// Hyprland windows have no title bar to leave free.
func (p *hyprPlatform) place(o engine.Overlay) *wireOverlay {
	w, ok := p.seen[o.Window.ID]
	if !ok {
		return nil
	}
	m, ok := monitorByID(w.Monitor)
	if !ok {
		return nil
	}
	top := 0
	if o.Cover == engine.CoverBrowser && w.Fullscreen == 0 {
		top = min(p.cfg.OverlayTopOffset, w.Size[1]/3)
	}
	out := placed(o)
	out.Monitor = m.Name
	out.X, out.Y, out.W, out.H = w.At[0]-m.X, w.At[1]-m.Y+top, w.Size[0], w.Size[1]-top
	return out
}
