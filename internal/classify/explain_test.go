package classify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"focuslock/internal/judge"
)

// explainJev answers off_task unless the request carries an explanation
// that isn't vague, then on_task. It records the last request.
type explainJev struct {
	mu   sync.Mutex
	last jevRequest
}

func (f *explainJev) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jevRequest
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.last = req
		f.mu.Unlock()
		probs := map[string]float64{"off_task": 0.95, "on_task": 0.05}
		if e := req.State["their_explanation"]; e != "" && e != "research" {
			probs = map[string]float64{"on_task": 0.9, "off_task": 0.1}
		}
		best := "off_task"
		if probs["on_task"] > probs["off_task"] {
			best = "on_task"
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{
			"type": "choice", "choice": best, "confidence": probs[best], "probabilities": probs,
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *explainJev) lastState() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last.State
}

func newExplainClassifier(t *testing.T) (*Classifier, *explainJev) {
	f := &explainJev{}
	srv := f.server(t)
	t.Setenv("TYPESAFE_API_KEY", "k")
	cfg := testConfig()
	cfg.Jev.Endpoint = srv.URL
	return New(cfg, goTask), f
}

func TestExplainUnlocksSiteAndBecomesContext(t *testing.T) {
	c, f := newExplainClassifier(t)
	const u = "https://www.hellointerview.com/learn/system-design/in-a-hurry/introduction"
	const title = "System Design in a Hurry | Hello Interview - Brave"
	v := waitVerdict(t, c, "brave.exe", title, u)
	if v.Cat != judge.CatOffTask || !c.CanExplain(v) {
		t.Fatalf("want an explainable off-task block, got %+v (can explain: %v)", v, c.CanExplain(v))
	}

	reason := "the Go worker pool interview question I'm practising is on this site"
	res, err := c.Explain(context.Background(), "brave.exe", title, u, reason)
	if err != nil || !res.Accepted || res.Label != "hellointerview.com" || res.Left != 2 {
		t.Fatalf("explain: %+v, %v", res, err)
	}
	if v := c.Classify("brave.exe", title, u); v.Cat != judge.CatAllow || v.Source != "explained" {
		t.Errorf("explained page: got %+v", v)
	}
	// Not a strict site, so the whole site is unlocked.
	if v := c.Classify("brave.exe", "Worker pools | Hello Interview - Brave", "https://www.hellointerview.com/learn/go/worker-pools"); v.Cat != judge.CatAllow {
		t.Errorf("same site: got %+v", v)
	}
	// The reason is context for every later question.
	waitVerdict(t, c, "brave.exe", "Something else - Brave", "https://example.com/x")
	if notes := f.lastState()["notes_from_this_session"]; !strings.Contains(notes, reason) {
		t.Errorf("later question missing the note: %q", notes)
	}
}

func TestExplainOnStrictSiteUnlocksOnlyThatPage(t *testing.T) {
	c, _ := newExplainClassifier(t)
	const title = "Go concurrency patterns at work - YouTube - Brave"
	const u = "https://www.youtube.com/watch?v=abc"
	waitVerdict(t, c, "brave.exe", title, u)
	if res, err := c.Explain(context.Background(), "brave.exe", title, u, "this talk covers select with worker pools"); err != nil || !res.Accepted {
		t.Fatalf("explain: %+v, %v", res, err)
	}
	if v := c.Classify("brave.exe", title, u); v.Cat != judge.CatAllow {
		t.Errorf("explained video: got %+v", v)
	}
	if v := waitVerdict(t, c, "brave.exe", "Funniest fails 2026 - YouTube - Brave", "https://www.youtube.com/watch?v=zzz"); v.Cat == judge.CatAllow {
		t.Errorf("one explained video mustn't unlock YouTube, got %+v", v)
	}
}

func TestVagueExplanationRejectedAndCapped(t *testing.T) {
	c, _ := newExplainClassifier(t)
	const title = "Wikipedia - Wikipedia - Brave"
	const u = "https://en.wikipedia.org/wiki/Wikipedia"
	v := waitVerdict(t, c, "brave.exe", title, u)
	for i := 0; i < 3; i++ {
		res, err := c.Explain(context.Background(), "brave.exe", title, u, "research")
		if err != nil || res.Accepted || res.Left != 2-i {
			t.Fatalf("attempt %d: %+v, %v", i, res, err)
		}
	}
	if _, err := c.Explain(context.Background(), "brave.exe", title, u, "research"); !errors.Is(err, ErrNoExplanationsLeft) {
		t.Errorf("4th attempt: got %v", err)
	}
	if c.CanExplain(v) {
		t.Error("no explanations left, but CanExplain is true")
	}
	if v := c.Classify("brave.exe", title, u); v.Cat != judge.CatOffTask {
		t.Errorf("still blocked: got %+v", v)
	}
}

func TestRulesCantBeExplained(t *testing.T) {
	c, _ := newExplainClassifier(t)
	for _, v := range []judge.Verdict{
		{Cat: judge.CatEntertainment, Source: "url"},
		{Cat: judge.CatEntertainment, Source: "rule"},
		{Cat: judge.CatEntertainment, Source: "placeholder"},
		{Cat: judge.CatMusic, Source: "jev"},
	} {
		if c.CanExplain(v) {
			t.Errorf("%+v shouldn't be explainable", v)
		}
	}
	cfg := testConfig()
	cfg.Explain.Enabled = false
	t.Setenv("TYPESAFE_API_KEY", "k")
	if New(cfg, goTask).CanExplain(judge.Verdict{Cat: judge.CatOffTask, Source: "jev"}) {
		t.Error("explain.enabled=false")
	}
}

func TestExplainedAppsAndRestore(t *testing.T) {
	c, _ := newExplainClassifier(t)
	waitVerdict(t, c, "docker desktop.exe", "Containers - Docker Desktop", "")
	if res, _ := c.Explain(context.Background(), "docker desktop.exe", "Containers - Docker Desktop", "", "my Go worker pool talks to a Postgres container"); !res.Accepted || res.Label != "docker desktop.exe" {
		t.Fatalf("app explain: %+v", res)
	}
	notes, used := c.SessionNotes()

	// A restarted worker gets the notes back from state.json.
	c2, _ := newExplainClassifier(t)
	c2.Restore(notes, used)
	if v := c2.Classify("docker desktop.exe", "Images - Docker Desktop", ""); v.Cat != judge.CatAllow || v.Source != "explained" {
		t.Errorf("restored app: got %+v", v)
	}
	if c2.ExplanationsLeft() != 2 {
		t.Errorf("left after restore: %d", c2.ExplanationsLeft())
	}
}

// ---- Copilot ----
