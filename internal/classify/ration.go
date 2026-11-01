package classify

import (
	"strings"
	"sync"
	"time"

	"focuslock/internal/config"
	"focuslock/internal/logx"
)

// Rationing: things that are fine for work but break focus when you keep
// going back to them (email, chat). Each gets a check window every N
// minutes; outside it the window is covered with when the next one is.
//
// The first visit opens a check window right away. Coming back while it's
// open is free. After it closes, the next opens once everyMinutes have
// passed since the last one started — on your next visit, not on its own.

type rationState struct {
	openedAt  time.Time // last check window started; zero = none yet
	openUntil time.Time
	visits    int // times you switched to it this session
}

type Rations struct {
	rules []config.RationRule

	mu    sync.Mutex
	state map[string]*rationState // by rule name
	now   func() time.Time
}

func NewRations(rules []config.RationRule) *Rations {
	return &Rations{rules: rules, state: map[string]*rationState{}, now: time.Now}
}

// RationStatus says whether a window may be shown right now.
type RationStatus struct {
	Rule    *config.RationRule // nil = not rationed
	Blocked bool
	NextAt  time.Time // when the next check window can open
	Until   time.Time // end of the open check window
	Open    bool      // a check window is open now
	Visits  int
}

// Match finds the rule for a window, if any. host is "" for apps.
func (r *Rations) Match(proc, title, host, path string) *config.RationRule {
	t := strings.ToLower(title)
	for i := range r.rules {
		rule := &r.rules[i]
		hit := false
		for _, m := range rule.Match {
			if m == proc || (host != "" && strings.Contains(m, ".") && !strings.HasSuffix(m, ".exe") && MatchDomain(m, host, path)) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, k := range rule.ExceptTitleKeywords {
			if k != "" && strings.Contains(t, k) {
				return nil
			}
		}
		return rule
	}
	return nil
}

// Visit is called while a rationed window is in front. It opens a check
// window if one is due. newVisit counts it: you just switched to it.
func (r *Rations) Visit(rule *config.RationRule, newVisit bool) RationStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stateFor(rule)
	if newVisit {
		st.visits++
	}
	now := r.now()
	if now.Before(st.openUntil) {
		return r.status(rule, st, now)
	}
	every := time.Duration(rule.EveryMinutes) * time.Minute
	if st.openedAt.IsZero() || !now.Before(st.openedAt.Add(every)) {
		st.openedAt = now
		st.openUntil = now.Add(time.Duration(rule.ForMinutes) * time.Minute)
		logx.Printf("ration %s: check window open until %s (visit %d)", rule.Name, st.openUntil.Format("15:04"), st.visits)
	}
	return r.status(rule, st, now)
}

// Status is the current state without counting a visit: for windows that
// stay in front, or sit on another monitor.
func (r *Rations) Status(rule *config.RationRule) RationStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status(rule, r.stateFor(rule), r.now())
}

// OpenEarly opens a check window now, outside the schedule (you explained
// why). It doesn't move the schedule: the next regular window is still
// everyMinutes after the last regular one.
func (r *Rations) OpenEarly(rule *config.RationRule, reason string) RationStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stateFor(rule)
	now := r.now()
	st.openUntil = now.Add(time.Duration(rule.ForMinutes) * time.Minute)
	logx.Printf("ration %s: opened early until %s — %q", rule.Name, st.openUntil.Format("15:04"), reason)
	return r.status(rule, st, now)
}

func (r *Rations) stateFor(rule *config.RationRule) *rationState {
	st, ok := r.state[rule.Name]
	if !ok {
		st = &rationState{}
		r.state[rule.Name] = st
	}
	return st
}

func (r *Rations) status(rule *config.RationRule, st *rationState, now time.Time) RationStatus {
	s := RationStatus{Rule: rule, Visits: st.visits, Until: st.openUntil}
	s.Open = now.Before(st.openUntil)
	if s.Open {
		return s
	}
	if st.openedAt.IsZero() {
		return s // never visited: the first visit opens a window
	}
	s.NextAt = st.openedAt.Add(time.Duration(rule.EveryMinutes) * time.Minute)
	s.Blocked = now.Before(s.NextAt)
	return s
}
