package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Explanations: when the judge blocks something, the person can say why
// they need it. The judge decides whether that's a good reason, with the
// same bar as strict sites (it must be sure it's work). If it is:
//   - the window is allowed for the rest of the session (a page on strict
//     sites, the whole site elsewhere, or the app), and
//   - the reason becomes a note every later question includes, so the
//     judge learns more about the task as the session goes.
//
// Rules (block lists, placeholders) can't be explained away; only judge
// verdicts can.

// SessionNote is an accepted explanation. Kept in state.json so a
// restarted worker keeps it.
type SessionNote struct {
	Scope  string `json:"scope"` // "site:<host>", "page:<host+path>|<title>", "app:<exe>"
	Label  string `json:"label"` // what was allowed, for people: "hellointerview.com"
	Reason string `json:"reason"`
	At     int64  `json:"at"`
}

type ExplainResult struct {
	Accepted bool
	Label    string  // what was (or would have been) allowed
	Choice   string  // the judge's pick, e.g. "on_task"
	Prob     float64 // how sure it is that this is work
	Left     int     // explanations left this session
}

var errNoExplanationsLeft = errors.New("no explanations left this session")

// Restore loads explanations from an earlier run of this session.
func (c *Classifier) Restore(notes []SessionNote, used int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes = append([]SessionNote(nil), notes...)
	c.explainUsed = used
}

// SessionNotes returns the accepted explanations and how many were used.
func (c *Classifier) SessionNotes() ([]SessionNote, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]SessionNote(nil), c.notes...), c.explainUsed
}

func (c *Classifier) ExplanationsLeft() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.explanationsLeft()
}

func (c *Classifier) explanationsLeft() int {
	return max(0, c.cfg.Explain.MaxPerSession-c.explainUsed)
}

// explainBudget: explanations left, and whether one can be used now.
func (c *Classifier) explainBudget() (left int, ok bool) {
	left = c.ExplanationsLeft()
	return left, c.cfg.Explain.Enabled && left > 0
}

// SpendExplanation uses one explanation without asking the judge (opening
// a rationed app early). ok is false if none are left.
func (c *Classifier) SpendExplanation() (left int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.cfg.Explain.Enabled || c.explanationsLeft() == 0 {
		return c.explanationsLeft(), false
	}
	c.explainUsed++
	return c.explanationsLeft(), true
}

// CanExplain: is this block one the person can argue with?
func (c *Classifier) CanExplain(v Verdict) bool {
	if !c.cfg.Explain.Enabled || c.judge == nil || v.Source != c.judge.Name() {
		return false
	}
	return (v.Cat == CatEntertainment || v.Cat == CatOffTask) && c.ExplanationsLeft() > 0
}

// Explain asks the judge whether reason justifies the window. It blocks on
// the network; call it off the UI thread.
func (c *Classifier) Explain(ctx context.Context, proc, title, rawURL, reason string) (ExplainResult, error) {
	reason = strings.TrimSpace(reason)
	if c.judge == nil {
		return ExplainResult{}, errors.New("no judge")
	}
	host, path := normalizeURL(rawURL)
	scope, label := c.explainScope(proc, title, host, path)

	c.mu.Lock()
	if c.explanationsLeft() == 0 {
		c.mu.Unlock()
		return ExplainResult{Label: label}, errNoExplanationsLeft
	}
	c.explainUsed++
	q := c.question(proc, title, host, path)
	q.Notes = c.noteTexts()
	q.Explanation = reason
	c.mu.Unlock()

	a, err := c.judge.Ask(ctx, q)
	if err != nil {
		c.mu.Lock()
		c.explainUsed-- // the judge failing isn't the person's fault
		c.mu.Unlock()
		return ExplainResult{Label: label}, err
	}
	v := judgeVerdict(c.judge, a, true, c.cfg.Judge)

	c.mu.Lock()
	defer c.mu.Unlock()
	res := ExplainResult{Accepted: v.Cat == CatAllow, Label: label, Choice: a.Choice, Prob: v.Prob, Left: c.explanationsLeft()}
	if !res.Accepted {
		res.Prob = 1 - v.Prob // v.Prob is how sure it's NOT work
		logf("explained, not accepted: %s %q — %q [%s %.2f]", proc, title, reason, a.Choice, res.Prob)
		return res, nil
	}
	c.notes = append(c.notes, SessionNote{Scope: scope, Label: label, Reason: reason, At: time.Now().Unix()})
	// Earlier blocks were judged without this note; let them be asked again.
	for k, cv := range c.cache {
		if cv.Cat == CatEntertainment || cv.Cat == CatOffTask {
			delete(c.cache, k)
		}
	}
	logf("explained, accepted: %s for the rest of the session — %q [%s %.2f]", label, reason, a.Choice, v.Prob)
	return res, nil
}

// explainScope decides what an accepted explanation unlocks: one page on
// strict sites (a reason for one video isn't a reason for YouTube), the
// whole site elsewhere, or the app.
func (c *Classifier) explainScope(proc, title, host, path string) (scope, label string) {
	switch {
	case !contains(c.cfg.Browsers, proc):
		return "app:" + proc, proc
	case host == "":
		return "page:|" + title, shorten(trimBrowserSuffix(title), 60)
	case c.isStrict(proc, host, path):
		return "page:" + host + path + "|" + title, shorten(trimBrowserSuffix(title), 60)
	}
	return "site:" + host, host
}

// explained reports whether an accepted explanation covers this window.
// Caller holds c.mu.
func (c *Classifier) explained(proc, title, host, path string) bool {
	isBrowser := contains(c.cfg.Browsers, proc)
	for _, n := range c.notes {
		kind, val, _ := strings.Cut(n.Scope, ":")
		switch {
		case kind == "app" && !isBrowser && val == proc:
			return true
		case kind == "site" && isBrowser && host != "" && matchDomain(val, host, path):
			return true
		case kind == "page" && isBrowser && val == host+path+"|"+title:
			return true
		}
	}
	return false
}

// noteTexts are the notes as the judge sees them. Caller holds c.mu.
func (c *Classifier) noteTexts() []string {
	if len(c.notes) == 0 {
		return nil
	}
	out := make([]string, len(c.notes))
	for i, n := range c.notes {
		out[i] = "Allowed " + n.Label + " because: " + n.Reason
	}
	return out
}

// trimBrowserSuffix: "Page - Brave" -> "Page".
func trimBrowserSuffix(title string) string {
	t := strings.TrimSpace(title)
	lower := strings.ToLower(t)
	for _, sep := range []string{" - ", " — "} {
		if i := strings.LastIndex(lower, sep); i >= 0 && isBrowserName(lower[i+len(sep):]) {
			return strings.TrimSpace(t[:i])
		}
	}
	return t
}
