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
	"focuslock/internal/session"
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
	return New(cfg, task)
}

func TestJevosAsksTopicQuestions(t *testing.T) {
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.95, "tool": 0.1, "fun": 0.05}}
	c := jevosClassifier(t, f, goTask)
	v := waitVerdict(t, c, "brave.exe", "Go by Example: Channels - Brave", "https://gobyexample.com/channels")
	if v.Cat != judge.CatAllow || v.Source != "jevos" {
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

func TestJevosIgnoresStrictBar(t *testing.T) {
	// On YouTube, FocusLock's strict bar wants 0.75 sure it's work; jevos
	// was tested with plain 0.5 cut-offs and scores better with them.
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.62, "tool": 0.1, "fun": 0.2}}
	c := jevosClassifier(t, f, goTask)
	if v := waitVerdict(t, c, "brave.exe", "Go channels in 10 minutes - YouTube - Brave", "https://www.youtube.com/watch?v=a"); v.Cat != judge.CatAllow {
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

func TestSearchPagesAllowedForEveryJudge(t *testing.T) {
	calls, mk := fakeJudge(t, map[string]float64{"entertainment": 0.95, "on_task": 0.05})
	c := mk(goTask)
	for _, u := range []string{"https://duckduckgo.com/?q=go+channels", "https://www.google.com/search?q=go", "https://www.google.co.in/search?q=go", "https://www.bing.com/search?q=go"} {
		if v := c.Classify("brave.exe", "go channels at DuckDuckGo - Brave", u); v.Cat != judge.CatAllow || v.Source != "url" {
			t.Errorf("%s: got %+v", u, v)
		}
	}
	// YouTube's results are a wall of videos: still judged.
	if v := waitVerdict(t, c, "brave.exe", "bollywood - YouTube - Brave", "https://www.youtube.com/results?search_query=bollywood"); v.Cat != judge.CatEntertainment {
		t.Errorf("youtube results: got %+v", v)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("judge calls: %d, want 1", n)
	}
}

func TestDeadJudgeKeepsStrictSitesCovered(t *testing.T) {
	cfg := testConfig()
	cfg.Judge.Provider = "jevos"
	c := New(cfg, goTask)
	c.judge = failingJudge{} // as if jev.exe were killed
	strict := waitVerdict(t, c, "brave.exe", "Go channels in 10 minutes - YouTube - Brave", "https://www.youtube.com/watch?v=a")
	if strict.Cat != judge.CatEntertainment || strict.Source != "unjudged" {
		t.Errorf("strict site with a dead judge: got %+v", strict)
	}
	other := waitVerdict(t, c, "brave.exe", "Go by Example: Channels - Brave", "https://gobyexample.com/channels")
	if other.Cat != judge.CatAllow || other.Source != "fallback" {
		t.Errorf("other site with a dead judge: got %+v", other)
	}
}

func TestJevosLeavesSessionNotesOut(t *testing.T) {
	// From a real session: once the ngrok note was in every question,
	// system-design pages passed as "about the task".
	f := &fakeJevos{ready: true, answers: map[string]float64{"topic": 0.2, "tool": 0.1, "fun": 0.1}}
	c := jevosClassifier(t, f, goTask)
	c.Restore([]session.Note{{Scope: "site:ngrok.com", Label: "ngrok.com", Reason: "i am going to build nggrok so exploring this page"}}, 1)
	v := waitVerdict(t, c, "brave.exe", "System Design Tutorial - GeeksforGeeks - Brave", "https://www.geeksforgeeks.org/system-design/system-design-tutorial/")
	if v.Cat != judge.CatOffTask {
		t.Errorf("got %+v", v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(f.state, "ngrok") || strings.Contains(f.qs["topic"], "ngrok") {
		t.Errorf("note leaked into the request: state %q, topic %q", f.state, f.qs["topic"])
	}
	// The explained site itself is still allowed.
	if v := c.Classify("brave.exe", "HTTP tunnels | ngrok - Brave", "https://ngrok.com/docs/http"); v.Cat != judge.CatAllow || v.Source != "explained" {
		t.Errorf("explained site: got %+v", v)
	}
}

// failingJudge never answers, like a judge whose server is down.
type failingJudge struct{}

func (failingJudge) Name() string { return "jevos" }
func (failingJudge) Ask(context.Context, judge.Question) (judge.Answer, error) {
	return judge.Answer{}, errors.New("jevos isn't running")
}
