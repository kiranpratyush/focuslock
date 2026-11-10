package engine

import (
	"strings"
	"testing"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/session"
)

// fakePlatform plays the OS: the test sets what's in front, and it records
// what the engine asked it to kill and close.
type fakePlatform struct {
	active  *Window
	others  []Window
	running map[string]bool
	killed  []string
	closed  []Window
}

func (p *fakePlatform) ActiveWindow() (Window, bool) {
	if p.active == nil {
		return Window{}, false
	}
	return *p.active, true
}

func (p *fakePlatform) OtherFrontWindows(string) []Window { return p.others }

func (p *fakePlatform) KillByName(names []string) []string {
	var out []string
	for _, n := range names {
		if p.running[n] {
			delete(p.running, n)
			out = append(out, n)
		}
	}
	p.killed = append(p.killed, out...)
	return out
}

func (p *fakePlatform) CloseTab(w Window) { p.closed = append(p.closed, w) }

var t0 = time.Date(2026, 11, 10, 14, 0, 0, 0, time.Local)

// newEngine is a 50-minute session from t0 with rules only (no judge).
func newEngine(t *testing.T, edit func(*config.Config)) (*Engine, *fakePlatform) {
	t.Helper()
	cfg := config.Default()
	cfg.Judge.Provider = "none"
	cfg.Jev.Enabled = false
	if edit != nil {
		edit(&cfg)
	}
	cfg.Normalize()
	sess := session.State{EndUnix: t0.Add(50 * time.Minute).Unix(), Intent: "fix pagination bug"}
	p := &fakePlatform{running: map[string]bool{}}
	return New(cfg, sess, classify.New(cfg, sess.Intent), p, nil), p
}

func browser(title, url string) *Window {
	return &Window{ID: "w1", Proc: "chrome.exe", Title: title + " - Google Chrome", URL: url}
}

func TestAllowedWindowIsNotCovered(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Pull requests", "https://github.com/acme/api/pulls")
	f := e.Tick(t0)
	if f.Overlay != nil {
		t.Fatalf("github covered: %+v", f.Overlay.View)
	}
	if !strings.Contains(f.Bar, "fix pagination bug") || !strings.Contains(f.Bar, "rules only") {
		t.Errorf("bar = %q", f.Bar)
	}
	if f.Remaining != 50*time.Minute {
		t.Errorf("remaining = %v", f.Remaining)
	}
}

func TestBlockedTabIsCoveredThenClosed(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Home", "https://www.instagram.com/")
	f := e.Tick(t0)
	if f.Overlay == nil || f.Overlay.Cover != CoverBrowser || f.Overlay.View.Kicker != "ON YOUR BLOCK LIST" {
		t.Fatalf("overlay = %+v", f.Overlay)
	}
	if !strings.Contains(f.Overlay.View.Footer, "closes in 02:00") {
		t.Errorf("footer = %q", f.Overlay.View.Footer)
	}
	id := f.Overlay.BlockID

	f = e.Tick(t0.Add(time.Minute))
	if f.Overlay == nil || f.Overlay.BlockID != id || !strings.Contains(f.Overlay.View.Footer, "closes in 01:00") {
		t.Fatalf("a minute in: %+v", f.Overlay)
	}
	f = e.Tick(t0.Add(2 * time.Minute))
	if f.Overlay != nil || len(p.closed) != 1 || p.closed[0].ID != "w1" {
		t.Fatalf("not closed after 2 min: overlay=%v closed=%v", f.Overlay, p.closed)
	}
	if !strings.Contains(f.Bar, "Closed tab") {
		t.Errorf("bar = %q", f.Bar)
	}
}

func TestSwitchingAwayLiftsTheBlock(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Home", "https://www.instagram.com/")
	e.Tick(t0)
	p.active = browser("Pull requests", "https://github.com/acme/api/pulls")
	if f := e.Tick(t0.Add(time.Second)); f.Overlay != nil {
		t.Fatal("still covered after switching to a work tab")
	}
	// Coming back starts the countdown again.
	p.active = browser("Home", "https://www.instagram.com/")
	f := e.Tick(t0.Add(90 * time.Second))
	if f.Overlay == nil || !strings.Contains(f.Overlay.View.Footer, "closes in 02:00") {
		t.Fatalf("overlay = %+v", f.Overlay)
	}
}

func TestOwnWindowPausesTheCountdown(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Home", "https://www.instagram.com/")
	e.Tick(t0)
	// Typing an explanation for 3 minutes (in 1-second ticks, as frontends do).
	p.active = &Window{ID: "overlay", Own: true}
	var f Frame
	for s := 1; s <= 180; s++ {
		f = e.Tick(t0.Add(time.Duration(s) * time.Second))
	}
	if f.Overlay == nil {
		t.Fatal("block screen dropped while typing")
	}
	p.active = browser("Home", "https://www.instagram.com/")
	f = e.Tick(t0.Add(181 * time.Second))
	if len(p.closed) != 0 || f.Overlay == nil || !strings.Contains(f.Overlay.View.Footer, "closes in 01:59") {
		t.Fatalf("countdown ran while typing: closed=%v overlay=%+v", p.closed, f.Overlay)
	}
}

func TestBlockedAppsAreKilled(t *testing.T) {
	e, p := newEngine(t, nil)
	p.running["steam.exe"] = true
	f := e.Tick(t0)
	if len(p.killed) != 1 || p.killed[0] != "steam.exe" || !strings.Contains(f.Bar, "Closed steam.exe") {
		t.Fatalf("killed=%v bar=%q", p.killed, f.Bar)
	}
	// Only every KillEvery.
	p.running["steam.exe"] = true
	e.Tick(t0.Add(time.Second))
	if len(p.killed) != 1 {
		t.Fatalf("killed again within %v", KillEvery)
	}
	e.Tick(t0.Add(KillEvery))
	if len(p.killed) != 2 {
		t.Fatalf("not killed again after %v", KillEvery)
	}
}

func TestBlockedAppInFrontIsKilledNotCovered(t *testing.T) {
	e, p := newEngine(t, nil)
	e.Tick(t0) // the periodic sweep runs now; the next is KillEvery away
	p.active = &Window{ID: "a1", Proc: "steam.exe", Title: "Steam"}
	p.running["steam.exe"] = true
	f := e.Tick(t0.Add(time.Second))
	if f.Overlay != nil || len(p.killed) != 1 {
		t.Fatalf("overlay=%v killed=%v", f.Overlay, p.killed)
	}
}

func TestMusicGetsAGracePeriod(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Lofi beats", "https://music.youtube.com/watch?v=1")
	if f := e.Tick(t0); f.Overlay != nil {
		t.Fatal("music covered at once")
	}
	// Another song on the same site doesn't restart the grace period.
	p.active = browser("Lofi beats 2", "https://music.youtube.com/watch?v=2")
	f := e.Tick(t0.Add(60 * time.Second))
	if f.Overlay == nil || f.Overlay.View.Kicker != "MUSIC IS FINE — IN THE BACKGROUND" {
		t.Fatalf("overlay = %+v", f.Overlay)
	}
}

func TestMusicOffIsBlockedAndKilled(t *testing.T) {
	e, p := newEngine(t, func(c *config.Config) { c.Music.Allowed = false })
	p.running["spotify.exe"] = true
	e.Tick(t0)
	if len(p.killed) != 1 || p.killed[0] != "spotify.exe" {
		t.Fatalf("killed = %v", p.killed)
	}
}

func TestRationedAppIsCoveredBetweenChecks(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = &Window{ID: "o1", Proc: "outlook.exe", Title: "Inbox - Outlook"}
	// The first visit opens a check window.
	if f := e.Tick(t0); f.Overlay != nil {
		t.Fatalf("first visit covered: %+v", f.Overlay.View)
	}
	f := e.Tick(t0.Add(6 * time.Minute))
	if f.Overlay == nil || f.Overlay.View.Kicker != "EMAIL IS RATIONED" || f.Overlay.Cover != CoverApp {
		t.Fatalf("overlay = %+v", f.Overlay)
	}
}

func TestExplainOpensARationEarly(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = &Window{ID: "o1", Proc: "outlook.exe", Title: "Inbox - Outlook"}
	e.Tick(t0)
	now := t0.Add(6 * time.Minute)
	e.Tick(now)
	e.Explain(now, "too short")
	if f := e.Tick(now); f.Explain.Status == "" {
		t.Fatal("short reason accepted")
	}
	e.Explain(now, "the release manager is emailing the hotfix sign-off")
	f := e.Tick(now.Add(time.Second))
	if f.Overlay != nil {
		t.Fatalf("still covered: %+v", f.Overlay.View)
	}
	if f.Explain.Released == nil || f.Explain.Released.ID != "o1" {
		t.Fatalf("released = %+v", f.Explain.Released)
	}
	if f = e.Tick(now.Add(2 * time.Second)); f.Explain.Released != nil {
		t.Fatal("released handed over twice")
	}
}

func TestSideMonitorsAreCoveredNotClosed(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Pull requests", "https://github.com/acme/api/pulls")
	p.others = []Window{
		{ID: "s1", Proc: "chrome.exe", Title: "Live - Twitch", URL: "https://www.twitch.tv/x"},
		{ID: "s2", Proc: "code.exe", Title: "main.go - Visual Studio Code"},
	}
	f := e.Tick(t0)
	if len(f.Side) != 1 || f.Side[0].Window.ID != "s1" || f.Side[0].View.Explain {
		t.Fatalf("side = %+v", f.Side)
	}
	if len(p.closed) != 0 {
		t.Fatal("closed a tab on another monitor")
	}
}

func TestTitleOnlyBrowsersStillBlock(t *testing.T) {
	// Linux reads no URL yet: the title has to be enough.
	e, p := newEngine(t, func(c *config.Config) { c.Browsers = append(c.Browsers, "chromium") })
	p.active = &Window{ID: "0xabc", Proc: "chromium", Title: "Instagram — Chromium"}
	f := e.Tick(t0)
	if f.Overlay == nil || f.Overlay.Cover != CoverBrowser {
		t.Fatalf("overlay = %+v", f.Overlay)
	}
}

func TestSessionEnds(t *testing.T) {
	e, p := newEngine(t, nil)
	p.active = browser("Home", "https://www.instagram.com/")
	if f := e.Tick(t0.Add(50 * time.Minute)); !f.Done || f.Overlay != nil {
		t.Fatalf("frame = %+v", f)
	}
}
