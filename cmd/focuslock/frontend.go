//go:build windows || linux

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/engine"
	"focuslock/internal/logx"
	"focuslock/internal/session"
)

// The worker's loop on every OS: run the engine, send each frame to the
// Qt app ("focuslock bar", started and kept alive here) over a local
// socket, and take back what you type in the explanation box.

const tickEvery = 500 * time.Millisecond

// desk is the OS under the engine: its windows and processes
// (engine.Platform), and where on screen a block screen goes.
type desk interface {
	engine.Platform
	// place turns an overlay into a spot on screen; nil if its window is gone.
	place(o engine.Overlay) *wireOverlay
	// setTyping: the explanation box has the keyboard, so the countdown
	// stands still. Where the app's windows are real windows the platform
	// sees that too; on Wayland only the app knows.
	setTyping(on bool)
}

// wireFrame is one frame as the Qt app reads it.
type wireFrame struct {
	Type      string        `json:"type"` // "frame"
	EndUnix   int64         `json:"endUnix"`
	Remaining int64         `json:"remaining"` // seconds
	Bar       string        `json:"bar"`
	Done      bool          `json:"done,omitempty"`
	Overlay   *wireOverlay  `json:"overlay,omitempty"`
	Side      []wireOverlay `json:"side,omitempty"` // blocked windows on other monitors
	Explain   wireExplain   `json:"explain"`
}

type wireOverlay struct {
	engine.View
	Accent   string `json:"accent"` // entertainment, off_task, music
	BlockID  int    `json:"blockId"`
	WindowID string `json:"windowId"` // the covered window, to give focus back to
	// Where: on Wayland, logical pixels relative to monitor; on Windows,
	// absolute physical pixels (Absolute), whatever the monitor's scaling.
	Monitor  string `json:"monitor,omitempty"`
	Absolute bool   `json:"absolute,omitempty"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	W        int    `json:"w"`
	H        int    `json:"h"`
}

type wireExplain struct {
	Busy     bool   `json:"busy"`
	Status   string `json:"status"`
	Released bool   `json:"released,omitempty"` // accepted: clear the box
}

func runUI(dir string, cfg config.Config, sess session.State, cls *classify.Classifier) {
	ui, uiErr := uiExe()
	if uiErr != nil {
		logx.Printf("can't find the FocusLock app: %v — blocking without a bar or block screen", uiErr)
	}
	d := newDesk(cfg, ui)
	save := func() {
		st := sess
		st.Notes, st.ExplainUsed = cls.SessionNotes()
		if err := session.Write(dir, st); err != nil {
			logx.Printf("save explanations: %v", err)
		}
	}
	eng := engine.New(cfg, sess, cls, d, save)
	listenAddr, appAddr := socketAddress(dir)
	srv, err := listenFrames(listenAddr)
	if err != nil {
		// Blocking still works without the app; only drawing doesn't.
		logx.Printf("listen %s: %v", listenAddr, err)
	}
	bar := &barProcess{exe: ui, addr: appAddr}
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
		if uiErr == nil && srv != nil {
			bar.keep()
		}
		if srv != nil {
			srv.send(wire(d, sess, f))
		}
		select {
		case <-ticker.C:
		case c := <-commands:
			switch c.Type {
			case "explain":
				d.setTyping(false)
				eng.Explain(time.Now(), strings.TrimSpace(c.Reason))
			case "typing":
				d.setTyping(c.On)
			}
		}
	}
}

func wire(d desk, sess session.State, f engine.Frame) wireFrame {
	w := wireFrame{Type: "frame", EndUnix: sess.EndUnix, Remaining: int64(f.Remaining.Seconds()), Bar: f.Bar,
		Explain: wireExplain{Busy: f.Explain.Busy, Status: f.Explain.Status, Released: f.Explain.Released != nil}}
	if f.Overlay != nil {
		w.Overlay = d.place(*f.Overlay)
	}
	for _, o := range f.Side {
		if p := d.place(o); p != nil {
			w.Side = append(w.Side, *p)
		}
	}
	return w
}

// placed fills in what every OS's place shares.
func placed(o engine.Overlay) *wireOverlay {
	return &wireOverlay{View: o.View, Accent: string(o.View.Accent), BlockID: o.BlockID, WindowID: o.Window.ID}
}

// barProcess starts the Qt bar and starts it again if it's closed. The bar
// gets this worker's pid and exits when it's gone, so a restarted worker
// doesn't end up with two.
type barProcess struct {
	exe, addr string
	cmd       *exec.Cmd
	done      chan struct{}
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
	cmd := exec.Command(b.exe, "bar", b.addr, strconv.Itoa(os.Getpid()))
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		logx.Printf("start bar: %v", err)
		b.done = nil
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
