package cue

import (
	"strings"
	"testing"
	"time"

	"focuslock/internal/config"
)

func cfg() config.CueConfig {
	c := config.Default()
	c.Normalize()
	return c.Cue
}

// at is a Monday in November 2026, at h:m.
func at(h, m int) time.Time { return time.Date(2026, 11, 2, h, m, 0, 0, time.Local) }

func TestFirstReminderAfterLogin(t *testing.T) {
	s := State{Started: at(9, 30)}
	if d := Decide(at(9, 30).Add(30*time.Second), s, cfg()); d.RemindNow || !d.ShowLine {
		t.Errorf("too soon after login: %+v", d)
	}
	d := Decide(at(9, 31), s, cfg())
	if !d.RemindNow || !strings.Contains(d.Message, "Good morning") {
		t.Errorf("a minute after login: %+v", d)
	}
}

func TestRemindsEvery45MinutesWhileIdle(t *testing.T) {
	s := State{Started: at(9, 0), LastRemind: at(10, 0)}
	if d := Decide(at(10, 44), s, cfg()); d.RemindNow {
		t.Errorf("44 min after the last one: %+v", d)
	}
	d := Decide(at(10, 45), s, cfg())
	if !d.RemindNow || d.Message != "No focus session for 1h 45m. Plan one?" {
		t.Errorf("45 min after the last one: %+v", d)
	}
}

func TestQuietDuringASession(t *testing.T) {
	s := State{Started: at(9, 0), SessionActive: true}
	if d := Decide(at(11, 0), s, cfg()); d.RemindNow || d.ShowLine {
		t.Errorf("session running: %+v", d)
	}
}

func TestBreakAfterSessionThenNudge(t *testing.T) {
	s := State{Started: at(9, 0), LastRemind: at(9, 1), LastSessionEnd: at(10, 0)}
	if d := Decide(at(10, 4), s, cfg()); d.RemindNow || !d.ShowLine {
		t.Errorf("during the 5-min break: %+v", d)
	}
	d := Decide(at(10, 5), s, cfg())
	if !d.RemindNow || !strings.Contains(d.Message, "Break's over") {
		t.Errorf("after the break: %+v", d)
	}
	// Once reminded, the normal interval applies again.
	s.LastRemind = at(10, 5)
	if d := Decide(at(10, 30), s, cfg()); d.RemindNow {
		t.Errorf("right after the post-break reminder: %+v", d)
	}
}

func TestWorkHours(t *testing.T) {
	c := cfg()
	s := State{Started: at(7, 0)}
	c.WorkHours = "09:00-19:00"
	if d := Decide(at(8, 30), s, c); d.RemindNow || d.ShowLine {
		t.Errorf("before work hours: %+v", d)
	}
	if d := Decide(at(19, 0), s, c); d.RemindNow || d.ShowLine {
		t.Errorf("at the end of work hours: %+v", d)
	}
	c.WorkHours = "22:00-02:00"
	if !InWorkHours(at(23, 30), c) || !InWorkHours(at(1, 0), c) || InWorkHours(at(3, 0), c) {
		t.Error("hours past midnight")
	}
	c.WorkHours = ""
	if !InWorkHours(at(3, 0), c) {
		t.Error("empty work hours should mean any time")
	}
	c.WeekdaysOnly = true
	saturday := time.Date(2026, 11, 7, 11, 0, 0, 0, time.Local)
	if InWorkHours(saturday, c) {
		t.Error("weekdaysOnly on a Saturday")
	}
}

func TestParseHours(t *testing.T) {
	for in, want := range map[string][2]int{"09:00-19:00": {540, 1140}, " 9 - 17 ": {540, 1020}, "22:30-02:00": {1350, 120}} {
		from, to, ok := ParseHours(in)
		if !ok || from != want[0] || to != want[1] {
			t.Errorf("%q: %d %d %v", in, from, to, ok)
		}
	}
	for _, bad := range []string{"", "9", "25:00-10:00", "09:00-09:00", "nine-five"} {
		if _, _, ok := ParseHours(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestDisabled(t *testing.T) {
	c := cfg()
	c.Enabled = false
	if d := Decide(at(11, 0), State{Started: at(9, 0)}, c); d.RemindNow || d.ShowLine {
		t.Errorf("disabled: %+v", d)
	}
	c = cfg()
	c.Line = false
	if d := Decide(at(9, 0).Add(10*time.Second), State{Started: at(9, 0)}, c); d.ShowLine {
		t.Errorf("line off: %+v", d)
	}
}
