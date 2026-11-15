package engine

import (
	"context"
	"fmt"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/session"
	"focuslock/internal/textutil"
)

// KillEvery is how often blocked apps are looked for and closed.
const KillEvery = 2 * time.Second

// Window is a window as the platform sees it.
type Window struct {
	ID    string // the platform's handle: an HWND, a Hyprland address
	Proc  string // lowercased process name ("chrome.exe", "chromium")
	Title string
	URL   string // the browser tab's URL, "" when it can't be read
	// Own: one of FocusLock's windows has focus (the block screen, while
	// you type an explanation). Nothing is judged, and a tab's countdown
	// stands still.
	Own bool
}

// Platform is what the engine needs from the OS.
type Platform interface {
	// ActiveWindow is the focused window; false when nothing is.
	ActiveWindow() (Window, bool)
	// OtherFrontWindows are the windows in front on the monitors that don't
	// hold the window with ID active. nil where that isn't supported.
	OtherFrontWindows(active string) []Window
	// KillByName ends processes with these names and returns those it ended.
	KillByName(names []string) []string
	// CloseTab closes the active tab of the browser window w.
	CloseTab(w Window)
}

// Cover says how much of a window the block screen covers: a browser keeps
// its tab strip free (config overlayTopOffset), an app its title bar.
type Cover string

const (
	CoverBrowser Cover = "browser"
	CoverApp     Cover = "app"
)

// Overlay is one block screen: what it says, and which window it covers.
type Overlay struct {
	Window Window
	View   View
	Cover  Cover
	// BlockID changes with every new block, so a frontend knows to clear
	// the explanation box.
	BlockID int
}

// Frame is everything a frontend draws after a tick.
type Frame struct {
	Remaining time.Duration
	Bar       string     // "ends 15:04 · task · Closed steam"
	Overlay   *Overlay   // over the focused window; nil = none
	Side      []Overlay  // over blocked windows on other monitors
	Explain   ExplainBox // the state of the explanation box
	Done      bool       // the session is over
}

// ExplainBox is the "why do you need this?" box under the block screen.
type ExplainBox struct {
	Busy   bool   // the judge is reading it
	Status string // the line under the box
	// Released: the last explanation was accepted, so the block is gone;
	// give focus back to the window that was covered.
	Released *Window
}

type blocked struct {
	win     Window
	verdict judge.Verdict
	ration  *config.RationRule
}

type explainOutcome struct {
	res classify.ExplainResult
	err error
}

// Engine is one session's monitor loop. Not safe for concurrent use: call
// Tick and Explain from one goroutine.
type Engine struct {
	cfg     config.Config
	sess    session.State
	cls     *classify.Classifier
	plat    Platform
	save    func()
	rations *classify.Rations

	blocked      blocked
	blockedKey   string
	blockedHost  string
	blockedSince time.Time
	blockCount   int
	lastView     View
	lastOverlay  *Overlay
	musicKey     string
	musicSince   time.Time
	lastKill     time.Time
	lastTick     time.Time
	lastEvent    string
	lastEventAt  time.Time
	rationFg     string

	explainBusy   bool
	explainStatus string
	explainDone   chan explainOutcome
	released      *Window
	sideLog       map[string]string
}

// New starts a session's engine. save writes accepted explanations to the
// session file; it may be nil.
func New(cfg config.Config, sess session.State, cls *classify.Classifier, plat Platform, save func()) *Engine {
	if save == nil {
		save = func() {}
	}
	e := &Engine{cfg: cfg, sess: sess, cls: cls, plat: plat, save: save,
		rations: classify.NewRations(cfg.Rations), explainDone: make(chan explainOutcome, 1), sideLog: map[string]string{}}
	// Rationing runs on the tick's clock, like everything else here.
	e.rations.SetClock(func() time.Time {
		if e.lastTick.IsZero() {
			return time.Now()
		}
		return e.lastTick
	})
	return e
}

// Tick runs the rules once and says what to draw.
func (e *Engine) Tick(now time.Time) Frame {
	dt := now.Sub(e.lastTick)
	if e.lastTick.IsZero() || dt > 5*time.Second || dt < 0 {
		dt = 0
	}
	e.lastTick = now
	if e.remaining(now) == 0 {
		return Frame{Done: true, Bar: e.barText(now)}
	}

	// 1. Kill blocked apps wherever they are, not just in front.
	if now.Sub(e.lastKill) >= KillEvery {
		e.lastKill = now
		list := e.cfg.BlockedProcesses
		if !e.cfg.Music.Allowed {
			list = append(append([]string{}, list...), e.cfg.MusicProcesses...)
		}
		for _, name := range e.plat.KillByName(list) {
			e.event(now, "Closed "+name)
		}
	}

	// 2. An explanation came back from the judge.
	select {
	case o := <-e.explainDone:
		e.explainFinished(now, o)
	default:
	}

	// 3. The focused window.
	ov, frontID := e.judgeFront(now, dt)
	e.lastOverlay = ov

	f := Frame{
		Remaining: e.remaining(now),
		Bar:       e.barText(now),
		Overlay:   ov,
		Side:      e.judgeSides(frontID),
		Explain:   ExplainBox{Busy: e.explainBusy, Status: e.explainStatus, Released: e.released},
	}
	e.released = nil // handed over once
	return f
}

func (e *Engine) remaining(now time.Time) time.Duration {
	return max(e.sess.End().Sub(now), 0)
}

// judgeFront decides about the focused window. It returns the block screen
// for it (nil for none) and the ID of the window that counts as in front.
func (e *Engine) judgeFront(now time.Time, dt time.Duration) (*Overlay, string) {
	w, ok := e.plat.ActiveWindow()
	if !ok {
		return nil, ""
	}
	if w.Own {
		// Typing an explanation, or the judge is reading it: the tab's
		// countdown stands still and the block screen stays.
		if e.blockedKey != "" {
			e.blockedSince = e.blockedSince.Add(dt)
		}
		return e.lastOverlay, e.blocked.win.ID
	}
	if e.blockedKey != "" && e.explainBusy {
		e.blockedSince = e.blockedSince.Add(dt)
	}

	isBrowser := textutil.Contains(e.cfg.Browsers, w.Proc)
	key := w.Proc + "|" + w.Title + "|" + w.URL
	host, path := classify.NormalizeURL(w.URL)
	cover := CoverApp
	if isBrowser {
		cover = CoverBrowser
	}

	// Rationed (email, chat): covered between check windows. Switching to
	// it counts as a visit and opens a window if one is due.
	if rule := e.rations.Match(w.Proc, w.Title, host, path); rule != nil {
		st := e.rations.Visit(rule, e.rationFg != rule.Name)
		e.rationFg = rule.Name
		if st.Blocked {
			if e.blockedKey != key {
				e.newBlock(now, key, host, blocked{win: w, ration: st.Rule})
				logx.Printf("rationed [%s, next %s, visit %d]: %s — %q %s", st.Rule.Name, st.NextAt.Format("15:04"), st.Visits, w.Proc, w.Title, w.URL)
			}
			e.blocked.win = w
			return e.show(w, e.rationView(e.blocked, st), cover), w.ID
		}
	} else {
		e.rationFg = ""
	}

	v := e.cls.Classify(w.Proc, w.Title, w.URL)
	block, holding, musicOver := false, false, false
	switch v.Cat {
	case judge.CatEntertainment, judge.CatOffTask:
		block = true
		e.musicKey = ""
	case judge.CatMusic:
		if !e.cfg.Music.Allowed {
			block = true
			break
		}
		// Grace is per site, not per title: hopping from video to video
		// on the same site doesn't restart it.
		mk := w.Proc + "|" + w.Title
		if host != "" {
			mk = w.Proc + "|" + host
		}
		if e.musicKey != mk {
			e.musicKey, e.musicSince = mk, now
		}
		if now.Sub(e.musicSince) >= time.Duration(e.cfg.Music.ForegroundGraceSeconds)*time.Second {
			block, musicOver = true, true
		}
	case judge.CatPending:
		// A strict site that's still being judged stays covered instead of
		// flickering. Without a URL, the same tab counts as the same site.
		if v.Hold && e.blockedKey != "" && ((host != "" && host == e.blockedHost) || (host == "" && key == e.blockedKey)) {
			block, holding = true, true
		}
	default:
		e.musicKey = ""
	}

	if !block {
		e.blockedKey, e.blockedHost = "", ""
		return nil, w.ID
	}

	if !isBrowser && v.Source == "rule" {
		// An app on your blocked list: close it.
		for _, name := range e.plat.KillByName([]string{w.Proc}) {
			e.event(now, "Closed "+name)
		}
		return nil, w.ID
	}

	if !holding && e.blockedKey != key {
		e.newBlock(now, key, host, blocked{win: w, verdict: v})
		logx.Printf("blocked [%s/%s %.2f]: %s — %q %s", v.Source, v.Cat, v.Prob, w.Proc, w.Title, w.URL)
	}
	e.blocked.win = w

	if holding {
		view := e.lastView
		view.Explain = false
		view.Footer = "Checking the new page…"
		return e.show(w, view, cover), w.ID
	}

	view := e.blockView(e.blocked, musicOver)
	if !isBrowser {
		// An app the judge flagged: cover it but never kill it or send keys,
		// it might hold unsaved work.
		view.Footer = "Switch back to your task, or minimize this window."
		return e.show(w, view, cover), w.ID
	}

	view.Footer = "Switch to a work tab to continue."
	if closeAfter := time.Duration(e.cfg.CloseTabAfterSeconds) * time.Second; closeAfter > 0 {
		left := closeAfter - now.Sub(e.blockedSince)
		if left <= 0 && !e.explainBusy {
			e.plat.CloseTab(w)
			e.event(now, "Closed tab: "+textutil.Shorten(w.Title, 40))
			e.blockedKey = ""
			return nil, w.ID
		}
		view.Footer = "This tab closes in " + session.FmtRemaining(max(left, 0)) + ". Switch to a work tab to keep it open."
		if view.Explain {
			view.Footer = "This tab closes in " + session.FmtRemaining(max(left, 0)) + ". Typing an explanation pauses the countdown."
		}
	}
	return e.show(w, view, cover), w.ID
}

func (e *Engine) newBlock(now time.Time, key, host string, b blocked) {
	e.blockedKey, e.blockedSince, e.blockedHost = key, now, host
	e.blocked = b
	e.blockCount++
	e.explainStatus = ""
}

func (e *Engine) show(w Window, view View, cover Cover) *Overlay {
	e.lastView = view
	return &Overlay{Window: w, View: view, Cover: cover, BlockID: e.blockCount}
}

// judgeSides covers blocked windows in front on the other monitors. They're
// covered, never closed (keys only reach the focused window), music is fine
// there, and nothing counts as a visit.
func (e *Engine) judgeSides(front string) []Overlay {
	var out []Overlay
	seen := map[string]bool{}
	for _, w := range e.plat.OtherFrontWindows(front) {
		if w.ID == front || w.Own {
			continue
		}
		seen[w.ID] = true
		isBrowser := textutil.Contains(e.cfg.Browsers, w.Proc)
		cover := CoverApp
		if isBrowser {
			cover = CoverBrowser
		}
		host, path := classify.NormalizeURL(w.URL)
		b := blocked{win: w}
		logOnce := func(what string) {
			if k := what + "|" + w.Title; e.sideLog[w.ID] != k {
				e.sideLog[w.ID] = k
				logx.Printf("%s on another monitor: %s — %q %s", what, w.Proc, w.Title, w.URL)
			}
		}
		if rule := e.rations.Match(w.Proc, w.Title, host, path); rule != nil {
			st := e.rations.Status(rule)
			if st.Open {
				delete(e.sideLog, w.ID)
				continue
			}
			b.ration = rule
			view := e.rationView(b, st)
			view.Explain = false
			view.Footer = "On your other screen. Click to bring it here; a check starts if one is due."
			logOnce("rationed [" + rule.Name + "]")
			out = append(out, Overlay{Window: w, View: view, Cover: cover})
			continue
		}
		v := e.cls.Classify(w.Proc, w.Title, w.URL)
		if v.Cat != judge.CatEntertainment && v.Cat != judge.CatOffTask {
			delete(e.sideLog, w.ID)
			continue
		}
		b.verdict = v
		view := e.blockView(b, false)
		view.Explain = false
		view.Footer = "On your other screen. Click it to explain, or switch it to something for your task."
		logOnce(fmt.Sprintf("blocked [%s/%s %.2f]", v.Source, v.Cat, v.Prob))
		out = append(out, Overlay{Window: w, View: view, Cover: cover})
	}
	for id := range e.sideLog {
		if !seen[id] {
			delete(e.sideLog, id)
		}
	}
	return out
}

// Explain hands in a reason for the window under the block screen. A
// rationed app opens at once; anything else goes to the judge, and the
// answer arrives in a later Tick's Frame.Explain.
func (e *Engine) Explain(now time.Time, reason string) {
	if e.explainBusy || e.blockedKey == "" || !e.lastView.Explain {
		return
	}
	if len([]rune(reason)) < 10 {
		e.explainStatus = "Say a bit more: how does this help your task?"
		return
	}
	if r := e.blocked.ration; r != nil {
		// Opening email or chat early needs a reason, not a verdict: the
		// friction is the point, and it works without a judge.
		if _, ok := e.cls.SpendExplanation(); !ok {
			e.explainStatus = "No explanations left this session."
			return
		}
		e.rations.OpenEarly(r, reason)
		e.save()
		e.event(now, fmt.Sprintf("%s open for %d min — %s", r.Name, r.ForMinutes, textutil.Shorten(reason, 40)))
		e.release()
		return
	}
	e.explainBusy = true
	e.explainStatus = "Checking your reason…"
	b := e.blocked.win
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := e.cls.Explain(ctx, b.Proc, b.Title, b.URL, reason)
		e.explainDone <- explainOutcome{res, err}
	}()
}

func (e *Engine) explainFinished(now time.Time, o explainOutcome) {
	e.explainBusy = false
	switch {
	case o.err == classify.ErrNoExplanationsLeft:
		e.explainStatus = "No explanations left this session."
	case o.err != nil:
		e.explainStatus = "Couldn't check it (" + textutil.Shorten(o.err.Error(), 60) + "). That one didn't count — try again."
	case o.res.Accepted:
		e.save()
		e.event(now, "Allowed "+textutil.Shorten(o.res.Label, 40)+" — your reason was accepted")
		e.release()
	default:
		e.explainStatus = fmt.Sprintf("Not convinced (%.0f%% sure it's for your task). %d left — be specific about how it helps.",
			o.res.Prob*100, o.res.Left)
		e.save() // the attempt counts
	}
}

// release lifts the current block after an accepted explanation.
func (e *Engine) release() {
	w := e.blocked.win
	e.released = &w
	e.explainStatus = ""
	e.blockedKey, e.blockedHost = "", ""
	e.lastOverlay = nil
}

func (e *Engine) event(now time.Time, s string) {
	e.lastEvent, e.lastEventAt = s, now
	logx.Printf("%s", s)
}

// barText is the bar's detail; frontends put the time left in front of it.
func (e *Engine) barText(now time.Time) string {
	t := "ends " + e.sess.End().Format("15:04")
	if task := e.cls.Intent(); task != "" {
		t += "  ·  " + textutil.Shorten(task, 50)
	}
	if e.cls.JudgeName() == "" {
		t += "  ·  rules only"
	}
	if e.lastEvent != "" && now.Sub(e.lastEventAt) < 10*time.Second {
		t += "  ·  " + e.lastEvent
	}
	return t
}
