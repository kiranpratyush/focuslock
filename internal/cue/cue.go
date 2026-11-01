// Package cue decides when to remind you to plan a focus session.
//
// It's only the timing: what to show and why, given the time, the
// settings and whether a session is running. The Windows window that shows
// it lives in cmd/focuslock (cue_windows.go). Keeping the decision a plain
// function makes it easy to read and to test.
package cue

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"focuslock/internal/config"
)

// State is what the cue window remembers between ticks.
type State struct {
	Started        time.Time // when the cue started (login)
	LastRemind     time.Time // when the pill last slid down; zero = not yet
	LastSessionEnd time.Time // when the last session ended; zero = none yet
	SessionActive  bool
}

// Decision is what the window should do right now.
type Decision struct {
	ShowLine  bool   // the faint breathing line along the top
	RemindNow bool   // slide the pill down
	Message   string // what the pill says
}

// Decide is the whole policy, from "stay out of the way" to "remind now".
func Decide(now time.Time, s State, cfg config.CueConfig) Decision {
	if !cfg.Enabled || s.SessionActive {
		return Decision{} // a session's own bar is showing
	}
	if !InWorkHours(now, cfg) {
		return Decision{} // evenings and weekends are yours
	}
	d := Decision{ShowLine: cfg.Line}
	every := time.Duration(cfg.RemindEveryMinutes) * time.Minute
	switch {
	case !s.LastSessionEnd.IsZero() && now.Sub(s.LastSessionEnd) < time.Duration(cfg.AfterSessionMinutes)*time.Minute:
		// Just finished a session: a short break before nudging again.
	case s.LastRemind.IsZero():
		if now.Sub(s.Started) >= time.Duration(cfg.StartupDelaySeconds)*time.Second {
			d.RemindNow, d.Message = true, firstMessage(now, s)
		}
	case !s.LastSessionEnd.IsZero() && s.LastSessionEnd.After(s.LastRemind):
		// The first reminder after a session, once the break is over.
		d.RemindNow, d.Message = true, "Break's over. Plan your next focus session"
	case now.Sub(s.LastRemind) >= every:
		d.RemindNow = true
		d.Message = fmt.Sprintf("No focus session for %s. Plan one?", idleFor(now, s))
	}
	return d
}

func firstMessage(now time.Time, s State) string {
	if !s.LastSessionEnd.IsZero() {
		return "Break's over. Plan your next focus session"
	}
	switch h := now.Hour(); {
	case h < 12:
		return "Good morning. Plan your first focus session"
	case h < 17:
		return "Plan your next focus session"
	}
	return "Evening. One more focus session?"
}

// idleFor says how long it's been since the last session (or since login).
func idleFor(now time.Time, s State) string {
	since := s.Started
	if s.LastSessionEnd.After(since) {
		since = s.LastSessionEnd
	}
	m := int(now.Sub(since).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// InWorkHours applies WorkHours and WeekdaysOnly.
func InWorkHours(now time.Time, cfg config.CueConfig) bool {
	if cfg.WeekdaysOnly && (now.Weekday() == time.Saturday || now.Weekday() == time.Sunday) {
		return false
	}
	from, to, ok := ParseHours(cfg.WorkHours)
	if !ok {
		return true // "" or unreadable: any time
	}
	m := now.Hour()*60 + now.Minute()
	if from <= to {
		return m >= from && m < to
	}
	return m >= from || m < to // wraps past midnight, like 22:00-02:00
}

// ParseHours reads "09:00-19:00" into minutes after midnight.
func ParseHours(s string) (from, to int, ok bool) {
	a, b, found := strings.Cut(strings.ReplaceAll(s, " ", ""), "-")
	if !found {
		return 0, 0, false
	}
	from, ok1 := hhmm(a)
	to, ok2 := hhmm(b)
	return from, to, ok1 && ok2 && from != to
}

func hhmm(s string) (int, bool) {
	h, m, found := strings.Cut(s, ":")
	if !found {
		m = "0"
	}
	hi, err1 := strconv.Atoi(h)
	mi, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hi < 0 || hi > 24 || mi < 0 || mi > 59 {
		return 0, false
	}
	return hi*60 + mi, true
}
