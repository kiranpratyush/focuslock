package judge

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
