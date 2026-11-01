package classify

import (
	"context"
	"errors"
	"strings"
	"time"

	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/session"
	"focuslock/internal/textutil"
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

type ExplainResult struct {
	Accepted bool
	Label    string  // what was (or would have been) allowed
	Choice   string  // the judge's pick, e.g. "on_task"
	Prob     float64 // how sure it is that this is work
	Left     int     // explanations left this session
}

var ErrNoExplanationsLeft = errors.New("no explanations left this session")

// Restore loads explanations from an earlier run of this session.
func (c *Classifier) Restore(notes []session.Note, used int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes = append([]session.Note(nil), notes...)
	c.explainUsed = used
}

// SessionNotes returns the accepted explanations and how many were used.
func (c *Classifier) SessionNotes() ([]session.Note, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]session.Note(nil), c.notes...), c.explainUsed
}

func (c *Classifier) ExplanationsLeft() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.explanationsLeft()
}

func (c *Classifier) explanationsLeft() int {
	return max(0, c.cfg.Explain.MaxPerSession-c.explainUsed)
}

// ExplainBudget: explanations left, and whether one can be used now.
func (c *Classifier) ExplainBudget() (left int, ok bool) {
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
func (c *Classifier) CanExplain(v judge.Verdict) bool {
	if !c.cfg.Explain.Enabled || c.judge == nil || v.Source != c.judge.Name() {
		return false
	}
	return (v.Cat == judge.CatEntertainment || v.Cat == judge.CatOffTask) && c.ExplanationsLeft() > 0
}

// Explain asks the judge whether reason justifies the window. It blocks on
// the network; call it off the UI thread.
func (c *Classifier) Explain(ctx context.Context, proc, title, rawURL, reason string) (ExplainResult, error) {
	reason = strings.TrimSpace(reason)
	if c.judge == nil {
		return ExplainResult{}, errors.New("no judge")
	}
	host, path := NormalizeURL(rawURL)
	scope, label := c.explainScope(proc, title, host, path)

	c.mu.Lock()
	if c.explanationsLeft() == 0 {
		c.mu.Unlock()
		return ExplainResult{Label: label}, ErrNoExplanationsLeft
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
	v := judge.VerdictFor(c.judge, a, true, c.cfg.Judge)

	c.mu.Lock()
	defer c.mu.Unlock()
	res := ExplainResult{Accepted: v.Cat == judge.CatAllow, Label: label, Choice: a.Choice, Prob: v.Prob, Left: c.explanationsLeft()}
	if !res.Accepted {
		res.Prob = 1 - v.Prob // v.Prob is how sure it's NOT work
		logx.Printf("explained, not accepted: %s %q — %q [%s %.2f]", proc, title, reason, a.Choice, res.Prob)
		return res, nil
	}
	c.notes = append(c.notes, session.Note{Scope: scope, Label: label, Reason: reason, At: time.Now().Unix()})
	// Earlier blocks were judged without this note; let them be asked again.
	for k, cv := range c.cache {
		if cv.Cat == judge.CatEntertainment || cv.Cat == judge.CatOffTask {
			delete(c.cache, k)
		}
	}
	logx.Printf("explained, accepted: %s for the rest of the session — %q [%s %.2f]", label, reason, a.Choice, v.Prob)
	return res, nil
}

// explainScope decides what an accepted explanation unlocks: one page on
// strict sites (a reason for one video isn't a reason for YouTube), the
// whole site elsewhere, or the app.
func (c *Classifier) explainScope(proc, title, host, path string) (scope, label string) {
	switch {
	case !textutil.Contains(c.cfg.Browsers, proc):
		return "app:" + proc, proc
	case host == "":
		return "page:|" + title, textutil.Shorten(TrimBrowserSuffix(title), 60)
	case c.isStrict(proc, host, path):
		return "page:" + host + path + "|" + title, textutil.Shorten(TrimBrowserSuffix(title), 60)
	}
	return "site:" + host, host
}

// explained reports whether an accepted explanation covers this window.
// Caller holds c.mu.
func (c *Classifier) explained(proc, title, host, path string) bool {
	isBrowser := textutil.Contains(c.cfg.Browsers, proc)
	for _, n := range c.notes {
		kind, val, _ := strings.Cut(n.Scope, ":")
		switch {
		case kind == "app" && !isBrowser && val == proc:
			return true
		case kind == "site" && isBrowser && host != "" && MatchDomain(val, host, path):
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
