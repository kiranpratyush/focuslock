//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/engine"
	"focuslock/internal/logx"
	"focuslock/internal/session"
)

// The Linux side of the loop: Hyprland is the platform, and the frame goes
// to live.json, which "focuslock bar" (started and kept alive here) draws as
// a layer-shell bar and block screen.

const tickEvery = 500 * time.Millisecond

// hyprPlatform is engine.Platform on Hyprland. It remembers each window's
// geometry from ActiveWindow so the block screen can be placed over it.
type hyprPlatform struct {
	cfg  config.Config
	seen map[string]hyprWindow
}

func (p *hyprPlatform) ActiveWindow() (engine.Window, bool) {
	w, ok := activeWindow()
	if !ok {
		return engine.Window{}, false
	}
	p.seen = map[string]hyprWindow{w.Address: w}
	// Linux has no UI Automation to read the address bar, so tabs are
	// judged by title; allowTitleKeywords applies as when a URL can't be read.
	return engine.Window{ID: w.Address, Proc: processName(w.Pid), Title: w.Title, Own: w.Class == "focuslock"}, true
}

// OtherFrontWindows isn't done on Hyprland yet.
func (p *hyprPlatform) OtherFrontWindows(string) []engine.Window { return nil }

func (p *hyprPlatform) KillByName(names []string) []string { return killByName(names) }

func (p *hyprPlatform) CloseTab(w engine.Window) { closeTab(w.ID) }

// liveState is live.json: everything the Qt app draws.
type liveState struct {
	EndUnix int64        `json:"endUnix"`
	Bar     string       `json:"bar"`
	Overlay *liveOverlay `json:"overlay,omitempty"`
}

type liveOverlay struct {
	engine.View
	Accent  string `json:"accent"` // entertainment, off_task, music
	Monitor string `json:"monitor"`
	X       int    `json:"x"` // relative to the monitor, logical pixels
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
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
	bar := &barProcess{dir: dir}
	defer func() {
		writeLive(dir, liveState{EndUnix: sess.EndUnix})
		bar.stop()
	}()
	for {
		f := eng.Tick(time.Now())
		if f.Done {
			return
		}
		bar.keep()
		live := liveState{EndUnix: sess.EndUnix, Bar: f.Bar}
		if f.Overlay != nil {
			live.Overlay = plat.place(*f.Overlay)
		}
		writeLive(dir, live)
		time.Sleep(tickEvery)
	}
}

// place turns an overlay into a spot on a monitor. A browser keeps its tab
// strip uncovered (overlayTopOffset) so you can switch to a work tab;
// Hyprland windows have no title bar to leave free.
func (p *hyprPlatform) place(o engine.Overlay) *liveOverlay {
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
	return &liveOverlay{View: o.View, Accent: string(o.View.Accent), Monitor: m.Name,
		X: w.At[0] - m.X, Y: w.At[1] - m.Y + top, W: w.Size[0], H: w.Size[1] - top}
}

// barProcess starts the Qt bar and starts it again if it's closed.
type barProcess struct {
	dir  string
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
	cmd := exec.Command(exe, "bar", filepath.Join(b.dir, "live.json"))
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

func writeLive(dir string, live liveState) {
	b, _ := json.Marshal(live)
	if err := writeAtomic(filepath.Join(dir, "live.json"), b); err != nil {
		logx.Printf("write live.json: %v", err)
	}
}
