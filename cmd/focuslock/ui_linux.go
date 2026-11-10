//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/engine"
	"focuslock/internal/logx"
	"focuslock/internal/session"
)

// The Linux side of the loop: Hyprland is the platform, and each frame goes
// over engine.sock to "focuslock bar" (started and kept alive here), which
// draws it as a layer-shell bar and block screen and sends back what you
// type in the explanation box.

const tickEvery = 500 * time.Millisecond

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

// wireFrame is one frame as the Qt app reads it.
type wireFrame struct {
	Type      string       `json:"type"` // "frame"
	EndUnix   int64        `json:"endUnix"`
	Remaining int64        `json:"remaining"` // seconds
	Bar       string       `json:"bar"`
	Done      bool         `json:"done,omitempty"`
	Overlay   *wireOverlay `json:"overlay,omitempty"`
	Explain   wireExplain  `json:"explain"`
}

type wireOverlay struct {
	engine.View
	Accent  string `json:"accent"` // entertainment, off_task, music
	BlockID int    `json:"blockId"`
	Monitor string `json:"monitor"`
	X       int    `json:"x"` // relative to the monitor, logical pixels
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
}

type wireExplain struct {
	Busy     bool   `json:"busy"`
	Status   string `json:"status"`
	Released bool   `json:"released,omitempty"` // accepted: clear the box
}

func runUI(dir string, cfg config.Config, sess session.State, cls *classify.Classifier) {
	plat := &hyprPlatform{cfg: cfg}
	save := func() {
		st := sess
		st.Notes, st.ExplainUsed = cls.SessionNotes()
		if err := session.Write(dir, st); err != nil {
			logx.Printf("save explanations: %v", err)
		}
	}
	eng := engine.New(cfg, sess, cls, plat, save)
	sock := socketPath(dir)
	srv, err := listenFrames(sock)
	if err != nil {
		// Blocking still works without the app; only drawing doesn't.
		logx.Printf("listen %s: %v", sock, err)
	}
	bar := &barProcess{sock: sock}
	defer func() {
		if srv != nil {
			srv.send(wireFrame{Type: "frame", EndUnix: sess.EndUnix, Done: true})
			srv.close()
		}
		bar.stop()
	}()

	var commands <-chan command
	if srv != nil {
		commands = srv.commands
	}
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		f := eng.Tick(time.Now())
		if f.Done {
			return
		}
		bar.keep()
		if srv != nil {
			srv.send(plat.wire(sess, f))
		}
		select {
		case <-ticker.C:
		case c := <-commands:
			switch c.Type {
			case "explain":
				plat.typing = false
				eng.Explain(time.Now(), strings.TrimSpace(c.Reason))
			case "typing":
				plat.typing = c.On
			}
		}
	}
}

// socketPath is where the worker listens: the per-user runtime directory
// (private, and short enough for the 108-byte limit on socket paths), or
// the data directory without one.
func socketPath(dir string) string {
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "focuslock-engine.sock")
	}
	return filepath.Join(dir, "engine.sock")
}

func (p *hyprPlatform) wire(sess session.State, f engine.Frame) wireFrame {
	w := wireFrame{Type: "frame", EndUnix: sess.EndUnix, Remaining: int64(f.Remaining.Seconds()), Bar: f.Bar,
		Explain: wireExplain{Busy: f.Explain.Busy, Status: f.Explain.Status, Released: f.Explain.Released != nil}}
	if f.Overlay != nil {
		w.Overlay = p.place(*f.Overlay)
	}
	return w
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
	return &wireOverlay{View: o.View, Accent: string(o.View.Accent), BlockID: o.BlockID, Monitor: m.Name,
		X: w.At[0] - m.X, Y: w.At[1] - m.Y + top, W: w.Size[0], H: w.Size[1] - top}
}

// barProcess starts the Qt bar and starts it again if it's closed.
type barProcess struct {
	sock string
	cmd  *exec.Cmd
	done chan struct{}
}

func (b *barProcess) keep() {
	if b.done != nil {
		select {
		case <-b.done:
			logx.Printf("bar exited — restarting it")
		default:
			return
		}
	}
	exe, err := uiExe()
	if err != nil {
		logx.Printf("can't find the focuslock app: %v", err)
		return
	}
	cmd := exec.Command(exe, "bar", b.sock)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		logx.Printf("start bar: %v", err)
		return
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	b.cmd, b.done = cmd, done
}

func (b *barProcess) stop() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
}
