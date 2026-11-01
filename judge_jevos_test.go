package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeJevos answers each yes/no question from answers (by question key)
// and records the last request.
type fakeJevos struct {
	mu      sync.Mutex
	answers map[string]float64
	state   string
	qs      map[string]string
	ready   bool
}

func (f *fakeJevos) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/health" {
			if !f.ready {
				w.WriteHeader(503)
			}
			return
		}
		var req struct {
			State     string `json:"state"`
			Questions map[string]struct {
				Type, Instructions string
			} `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.state, f.qs = req.State, map[string]string{}
		ans := map[string]any{}
		for k, q := range req.Questions {
			if q.Type != "noul" {
				t.Errorf("question %s: type %q, want noul", k, q.Type)
			}
			f.qs[k] = q.Instructions
			ans[k] = map[string]any{"type": "noul", "noul": f.answers[k]}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "jevos-v5", "answers": ans})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func jevosClassifier(t *testing.T, f *fakeJevos, task string) *Classifier {
	srv := f.server(t)
	cfg := testConfig()
	cfg.Judge.Provider = "jevos"
	cfg.Jevos.Endpoint = srv.URL + "/v1/systemone"
	return NewClassifier(cfg, task)
}

func TestJevosAsksTopicQuestions(t *testing.T) {
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.95, "tool": 0.1, "fun": 0.05}}
	c := jevosClassifier(t, f, goTask)
	v := waitVerdict(t, c, "brave.exe", "Go by Example: Channels - Brave", "https://gobyexample.com/channels")
	if v.Cat != CatAllow || v.Source != "jevos" {
		t.Fatalf("got %+v", v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(f.qs["topic"], goTask) || f.qs["tool"] == "" || f.qs["fun"] == "" || len(f.qs) != 3 {
		t.Errorf("questions: %+v", f.qs)
	}
	if !strings.Contains(f.state, "Window title: Go by Example: Channels - Brave") || !strings.Contains(f.state, "Address: gobyexample.com/channels") {
		t.Errorf("state: %q", f.state)
	}
}

func TestJevosDecide(t *testing.T) {
	cases := []struct {
		name string
		p    map[string]float64
		want Category
	}{
		{"on topic", map[string]float64{"topic": 0.9, "tool": 0.1, "fun": 0.1}, CatAllow},
		{"work tool", map[string]float64{"topic": 0.1, "tool": 0.7, "fun": 0.1}, CatAllow},
		{"other topic", map[string]float64{"topic": 0.04, "tool": 0.2, "fun": 0.1}, CatOffTask},
		{"fun", map[string]float64{"topic": 0.04, "tool": 0.1, "fun": 0.9}, CatEntertainment},
		{"no task: learning", map[string]float64{"work": 0.8, "music": 0.0, "fun": 0.3}, CatAllow},
		{"no task: music", map[string]float64{"work": 0.1, "music": 0.8, "fun": 0.6}, CatMusic},
		{"no task: fun", map[string]float64{"work": 0.1, "music": 0.1, "fun": 0.9}, CatEntertainment},
		{"no task: unsure", map[string]float64{"work": 0.3, "music": 0.1, "fun": 0.3}, CatAllow},
		{"explanation ok", map[string]float64{"explanation": 0.9}, CatAllow},
		{"explanation vague", map[string]float64{"explanation": 0.05}, CatOffTask},
	}
	for _, tc := range cases {
		if got := jevosDecide(tc.p).Cat; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestJevosIgnoresStrictBar(t *testing.T) {
	// On YouTube, FocusLock's strict bar wants 0.75 sure it's work; jevos
	// was tested with plain 0.5 cut-offs and scores better with them.
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.62, "tool": 0.1, "fun": 0.2}}
	c := jevosClassifier(t, f, goTask)
	if v := waitVerdict(t, c, "brave.exe", "Go channels in 10 minutes - YouTube - Brave", "https://www.youtube.com/watch?v=a"); v.Cat != CatAllow {
		t.Errorf("got %+v", v)
	}
}

func TestJevosExplanationAskedAlone(t *testing.T) {
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.03, "tool": 0.1, "fun": 0.1, "explanation": 0.9}}
	c := jevosClassifier(t, f, goTask)
	const title, u = "Containers - Docker Desktop", ""
	waitVerdict(t, c, "docker desktop.exe", title, u)
	res, err := c.Explain(context.Background(), "docker desktop.exe", title, u, "my worker pool demo runs its jobs against a Postgres container")
	if err != nil || !res.Accepted {
		t.Fatalf("explain: %+v %v", res, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state != "Explanation: my worker pool demo runs its jobs against a Postgres container" || len(f.qs) != 1 || !strings.Contains(f.qs["explanation"], goTask) {
		t.Errorf("explanation request: state %q, questions %+v", f.state, f.qs)
	}
}

func TestJevosWaitsForServer(t *testing.T) {
	f := &fakeJevos{answers: map[string]float64{"work": 0.9}}
	srv := f.server(t)
	j := &jevosJudge{endpoint: srv.URL + "/v1/systemone", http: http.DefaultClient}
	go func() {
		time.Sleep(400 * time.Millisecond) // the model is loading
		f.mu.Lock()
		f.ready = true
		f.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := j.Ask(ctx, Question{App: "code.exe", Title: "x"}); err != nil {
		t.Fatalf("should wait for the server: %v", err)
	}
	// Not running at all: a clear error once the time is up.
	short, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	dead := &jevosJudge{endpoint: "http://127.0.0.1:1/v1/systemone", http: http.DefaultClient}
	if _, err := dead.Ask(short, Question{}); err == nil || !strings.Contains(err.Error(), "isn't running") {
		t.Errorf("dead server: %v", err)
	}
}

func TestSearchPagesAllowedForEveryJudge(t *testing.T) {
	calls, mk := fakeJudge(t, map[string]float64{"entertainment": 0.95, "on_task": 0.05})
	c := mk(goTask)
	for _, u := range []string{"https://duckduckgo.com/?q=go+channels", "https://www.google.com/search?q=go", "https://www.google.co.in/search?q=go", "https://www.bing.com/search?q=go"} {
		if v := c.Classify("brave.exe", "go channels at DuckDuckGo - Brave", u); v.Cat != CatAllow || v.Source != "url" {
			t.Errorf("%s: got %+v", u, v)
		}
	}
	// YouTube's results are a wall of videos: still judged.
	if v := waitVerdict(t, c, "brave.exe", "bollywood - YouTube - Brave", "https://www.youtube.com/results?search_query=bollywood"); v.Cat != CatEntertainment {
		t.Errorf("youtube results: got %+v", v)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("judge calls: %d, want 1", n)
	}
}

func TestDeadJudgeKeepsStrictSitesCovered(t *testing.T) {
	cfg := testConfig()
	cfg.Judge.Provider = "jevos"
	cfg.Jevos.Endpoint = "http://127.0.0.1:1/v1/systemone" // nothing listens here
	c := NewClassifier(cfg, goTask)
	c.judge = &jevosJudge{endpoint: cfg.Jevos.Endpoint, http: http.DefaultClient}
	c.cfg.Judge.TimeoutMs = 300
	c.judge = shortJudge{c.judge}
	strict := waitVerdict(t, c, "brave.exe", "Go channels in 10 minutes - YouTube - Brave", "https://www.youtube.com/watch?v=a")
	if strict.Cat != CatEntertainment || strict.Source != "unjudged" {
		t.Errorf("strict site with a dead judge: got %+v", strict)
	}
	other := waitVerdict(t, c, "brave.exe", "Go by Example: Channels - Brave", "https://gobyexample.com/channels")
	if other.Cat != CatAllow || other.Source != "fallback" {
		t.Errorf("other site with a dead judge: got %+v", other)
	}
}

// shortJudge drops the judge's own long timeout so the test is quick.
type shortJudge struct{ Judge }

func TestJevosLeavesSessionNotesOut(t *testing.T) {
	// From a real session: once the ngrok note was in every question,
	// system-design pages passed as "about the task".
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.2, "tool": 0.1, "fun": 0.1}}
	c := jevosClassifier(t, f, goTask)
	c.Restore([]SessionNote{{Scope: "site:ngrok.com", Label: "ngrok.com", Reason: "i am going to build nggrok so exploring this page"}}, 1)
	v := waitVerdict(t, c, "brave.exe", "System Design Tutorial - GeeksforGeeks - Brave", "https://www.geeksforgeeks.org/system-design/system-design-tutorial/")
	if v.Cat != CatOffTask {
		t.Errorf("got %+v", v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(f.state, "ngrok") || strings.Contains(f.qs["topic"], "ngrok") {
		t.Errorf("note leaked into the request: state %q, topic %q", f.state, f.qs["topic"])
	}
	// The explained site itself is still allowed.
	if v := c.Classify("brave.exe", "HTTP tunnels | ngrok - Brave", "https://ngrok.com/docs/http"); v.Cat != CatAllow || v.Source != "explained" {
		t.Errorf("explained site: got %+v", v)
	}
}
