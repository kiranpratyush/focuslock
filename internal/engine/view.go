package engine

import (
	"fmt"
	"strings"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/judge"
	"focuslock/internal/session"
)

// What the block screen says about a blocked window.

// What the block screen says about a blocked window. Shared by the Win32
// overlay and the Linux one, which hands it to the Qt app as JSON.

func (e *Engine) blockView(b blocked, musicOver bool) View {
	v := b.verdict
	host, _ := classify.NormalizeURL(b.win.URL)
	view := View{Accent: v.Cat, Title: classify.TrimBrowserSuffix(b.win.Title)}
	if view.Title == "" {
		view.Title = b.win.Proc
	}
	view.Where = b.win.Proc
	if host != "" {
		view.Where = host + "  ·  " + b.win.Proc
	}

	who := JudgeDisplayName(v.Source)
	switch {
	case musicOver:
		view.Kicker = "MUSIC IS FINE — IN THE BACKGROUND"
		view.Why = fmt.Sprintf("Music can stay in front for %d seconds. It keeps playing if you switch away.", e.cfg.Music.ForegroundGraceSeconds)
	case v.Cat == judge.CatMusic:
		view.Kicker = "MUSIC IS OFF THIS SESSION"
		view.Why = `"music": {"allowed": false} in focus.json`
	case v.Source == "url":
		view.Kicker = "ON YOUR BLOCK LIST"
		view.Why = host + " is in blockDomains in focus.json."
	case v.Source == "rule":
		view.Kicker = "ON YOUR BLOCK LIST"
		view.Why = "The title matches a keyword in blockTitleKeywords in focus.json."
	case v.Source == "unjudged":
		view.Kicker = "THE JUDGE ISN'T ANSWERING"
		view.Why = "Pages on this site stay covered until it's back. Other sites are allowed meanwhile."
	case v.Source == "placeholder":
		view.Kicker = "HOME PAGES AND FEEDS ARE BLOCKED"
		view.Why = "Open a specific page instead. Pages on this site are checked one by one."
	case v.Cat == judge.CatOffTask:
		view.Kicker = "NOT PART OF YOUR TASK"
		view.Why = fmt.Sprintf("%s judged this off-task, %.0f%% sure.", who, v.Prob*100)
	default:
		view.Kicker = "THIS LOOKS LIKE ENTERTAINMENT"
		view.Why = fmt.Sprintf("%s judged this as entertainment, %.0f%% sure.", who, v.Prob*100)
	}

	view.Task, view.Stats = e.taskLine(), e.statsLine()
	view.Explain = e.cls.CanExplain(v)
	view.Prompt = "Need this for your task? Say why — Enter to send, Esc to go back."
	return view
}

func (e *Engine) rationView(b blocked, st classify.RationStatus) View {
	r := st.Rule
	host, _ := classify.NormalizeURL(b.win.URL)
	view := View{Accent: judge.CatMusic, Title: classify.TrimBrowserSuffix(b.win.Title), Where: b.win.Proc}
	if host != "" {
		view.Where = host + "  ·  " + b.win.Proc
	}
	view.Kicker = strings.ToUpper(r.Name) + " IS RATIONED"
	visits := fmt.Sprintf("You've gone to it %d times this session.", st.Visits)
	if wait := time.Until(st.NextAt); wait > 0 {
		view.Why = fmt.Sprintf("Next check at %s (in %d min). %s", st.NextAt.Format("15:04"), int(wait.Minutes())+1, visits)
	} else {
		view.Why = "A check is due: switch to it to start one. " + visits
	}
	view.Task, view.Stats = e.taskLine(), e.statsLine()
	_, can := e.cls.ExplainBudget()
	view.Explain = can
	view.Prompt = fmt.Sprintf("Need it now? Say why — Enter opens it for %d minutes, Esc goes back.", r.ForMinutes)
	view.Footer = fmt.Sprintf("You get %d minutes every %d. Switch back to your task.", r.ForMinutes, r.EveryMinutes)
	return view
}

func (e *Engine) taskLine() string {
	if t := e.cls.Intent(); t != "" {
		return "Your task: " + t
	}
	return "No task set — only entertainment is blocked."
}

func (e *Engine) statsLine() string {
	stats := fmt.Sprintf("%s of focus left  ·  %d blocked this session", session.FmtRemaining(e.remaining(e.lastTick)), e.blockCount)
	if e.cfg.Explain.Enabled {
		stats += fmt.Sprintf("  ·  %d of %d explanations left", e.cls.ExplanationsLeft(), e.cfg.Explain.MaxPerSession)
	}
	return stats
}

func JudgeDisplayName(source string) string {
	switch source {
	case "jev":
		return "Jev"
	case "copilot":
		return "Copilot"
	case "llm":
		return "The local model"
	case "":
		return "The judge"
	}
	return strings.ToUpper(source[:1]) + source[1:]
}

// overlayView is everything the block screen shows.
type View struct {
	Accent  judge.Category `json:"accent"`
	Kicker  string         `json:"kicker"` // "NOT PART OF YOUR TASK"
	Title   string         `json:"title"`  // the page or window
	Where   string         `json:"where"`  // "youtube.com · brave.exe"
	Why     string         `json:"why"`    // who decided, and how sure
	Task    string         `json:"task"`
	Explain bool           `json:"explain"` // show the "why do you need this?" box
	Prompt  string         `json:"prompt"`  // the line above the box
	Footer  string         `json:"footer"`
	Stats   string         `json:"stats"`
}
