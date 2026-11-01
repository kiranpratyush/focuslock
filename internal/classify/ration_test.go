package classify

import (
	"testing"
	"time"
)

func testRations(t *testing.T) (*Rations, *time.Time) {
	t.Helper()
	cfg := testConfig()
	r := NewRations(cfg.Rations)
	clock := time.Date(2026, 11, 1, 14, 0, 0, 0, time.Local)
	r.now = func() time.Time { return clock }
	return r, &clock
}

func TestRationMatch(t *testing.T) {
	r, _ := testRations(t)
	cases := []struct {
		proc, title, host, want string
	}{
		{"outlook.exe", "Inbox - you@company.com - Outlook", "", "Email"},
		{"olk.exe", "Mail - Outlook", "", "Email"},
		{"brave.exe", "Mail - Outlook - Brave", "outlook.office.com", "Email"},
		{"ms-teams.exe", "Chat | Microsoft Teams", "", "Teams"},
		{"ms-teams.exe", "Daily standup meeting | Microsoft Teams", "", ""}, // never cover a meeting
		{"ms-teams.exe", "Call with Priya | Microsoft Teams", "", ""},
		{"brave.exe", "outlook.com alternatives - Brave", "duckduckgo.com", ""},
		{"code.exe", "main.go - Visual Studio Code", "", ""},
	}
	for _, tc := range cases {
		got := ""
		if rule := r.Match(tc.proc, tc.title, tc.host, "/"); rule != nil {
			got = rule.Name
		}
		if got != tc.want {
			t.Errorf("%s %q %s: got %q, want %q", tc.proc, tc.title, tc.host, got, tc.want)
		}
	}
}

func TestRationSchedule(t *testing.T) {
	r, clock := testRations(t)
	email := r.Match("outlook.exe", "Inbox - Outlook", "", "")

	// Never visited: not blocked, and the first visit opens a 5-minute window.
	if s := r.Status(email); s.Blocked {
		t.Fatalf("before first visit: %+v", s)
	}
	if s := r.Visit(email, true); s.Blocked || s.Until.Sub(*clock) != 5*time.Minute {
		t.Fatalf("first visit: %+v", s)
	}
	// Back again inside the window: free.
	*clock = clock.Add(3 * time.Minute)
	if s := r.Visit(email, true); s.Blocked {
		t.Fatalf("inside window: %+v", s)
	}
	// After it closes: covered until 14:30, and visits are counted.
	*clock = clock.Add(4 * time.Minute) // 14:07
	s := r.Visit(email, true)
	if !s.Blocked || s.NextAt.Format("15:04") != "14:30" || s.Visits != 3 {
		t.Fatalf("between windows: %+v", s)
	}
	// Staying in front doesn't count visits.
	if s := r.Visit(email, false); s.Visits != 3 {
		t.Errorf("staying in front counted a visit: %+v", s)
	}
	// At 14:30 the next visit opens a new window.
	*clock = time.Date(2026, 11, 1, 14, 30, 0, 0, time.Local)
	if s := r.Visit(email, true); s.Blocked || s.Until.Format("15:04") != "14:35" {
		t.Fatalf("next window: %+v", s)
	}
}

func TestRationOpenEarlyKeepsSchedule(t *testing.T) {
	r, clock := testRations(t)
	email := r.Match("outlook.exe", "Inbox - Outlook", "", "")
	r.Visit(email, true)                 // 14:00 window
	*clock = clock.Add(10 * time.Minute) // 14:10, covered
	if s := r.Visit(email, true); !s.Blocked {
		t.Fatal("should be covered at 14:10")
	}
	if s := r.OpenEarly(email, "waiting on the deploy approval email"); s.Blocked || s.Until.Format("15:04") != "14:15" {
		t.Fatalf("open early: %+v", s)
	}
	*clock = clock.Add(6 * time.Minute) // 14:16: early window over, next regular one still 14:30
	if s := r.Visit(email, true); !s.Blocked || s.NextAt.Format("15:04") != "14:30" {
		t.Fatalf("after early window: %+v", s)
	}
}

func TestRationsAreIndependent(t *testing.T) {
	r, clock := testRations(t)
	email := r.Match("outlook.exe", "Inbox - Outlook", "", "")
	teams := r.Match("ms-teams.exe", "Chat | Microsoft Teams", "", "")
	r.Visit(email, true)
	*clock = clock.Add(10 * time.Minute)
	if s := r.Visit(teams, true); s.Blocked {
		t.Errorf("Teams' first visit shouldn't depend on email: %+v", s)
	}
}
