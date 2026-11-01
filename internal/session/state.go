package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the active lock. It is tied to the current boot,
// so restarting the PC ends the lock (as agreed for v1).
type State struct {
	EndUnix  int64  `json:"endUnix"`
	BootUnix int64  `json:"bootUnix"`
	Intent   string `json:"intent,omitempty"` // the task, if given
	// The task as the judge spelled it out at start (task_detail.go).
	TaskDetail string `json:"taskDetail,omitempty"`
	// Accepted explanations and how many were tried (explain.go).
	Notes       []Note `json:"notes,omitempty"`
	ExplainUsed int    `json:"explainUsed,omitempty"`
	// Allowed for this session only, from the start pop-up.
	AllowDomains []string `json:"allowDomains,omitempty"`
	AllowApps    []string `json:"allowApps,omitempty"`
}

func (s State) End() time.Time { return time.Unix(s.EndUnix, 0) }

func (s State) Remaining() time.Duration {
	d := time.Until(s.End())
	if d < 0 {
		return 0
	}
	return d
}

func Path(dir string) string { return filepath.Join(dir, "state.json") }

// ReadActive returns the lock if it exists, hasn't expired,
// and belongs to this boot. Stale state is deleted.
func ReadActive(dir string) (State, bool) {
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		return State{}, false
	}
	var s State
	if json.Unmarshal(b, &s) != nil {
		return State{}, false
	}
	sameBoot := abs64(s.BootUnix-BootTime().Unix()) < 120
	if !sameBoot || s.Remaining() == 0 {
		_ = os.Remove(Path(dir))
		return State{}, false
	}
	return s, true
}

func Write(dir string, s State) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(Path(dir), b, 0o644)
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func FmtRemaining(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return pad2(h) + ":" + pad2(m) + ":" + pad2(s)
	}
	return pad2(m) + ":" + pad2(s)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Note is an explanation the judge accepted (see package classify). Kept in state.json so a
// restarted worker keeps it.
type Note struct {
	Scope  string `json:"scope"` // "site:<host>", "page:<host+path>|<title>", "app:<exe>"
	Label  string `json:"label"` // what was allowed, for people: "hellointerview.com"
	Reason string `json:"reason"`
	At     int64  `json:"at"`
}
