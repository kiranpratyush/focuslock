package judge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"focuslock/internal/config"
)

func TestCleanTaskDetail(t *testing.T) {
	in := "**Goal:** learn Go channels.\n\n- Topics: select, worker pools\n2. Resources: Go by Example\n# Not part of it: system design\nExtra line that should go"
	want := "Goal: learn Go channels.\nTopics: select, worker pools\nResources: Go by Example\nNot part of it: system design"
	if got := cleanTaskDetail(in); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := cleanTaskDetail(strings.Repeat("x", 2000)); len([]rune(got)) != 700 {
		t.Errorf("not capped: %d", len([]rune(got)))
	}
}

func TestDescribeTaskOnlyForWritingJudges(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "k")
	cfg := testConfig() // Jev: classifies only
	if d, err := Describe(cfg, New(cfg), goTask); d != "" || err != nil {
		t.Errorf("jev: %q %v", d, err)
	}
	cfg.Judge.Provider = "jevos"
	if d, err := Describe(cfg, New(cfg), goTask); d != "" || err != nil {
		t.Errorf("jevos: %q %v", d, err)
	}
	f := &fakeLLM{}
	cfg = llmConfig(f.server(t))
	cfg.Judge.DescribeTask = false
	if d, _ := Describe(cfg, New(cfg), goTask); d != "" || len(f.prompts) != 0 {
		t.Errorf("describeTask=false still asked: %q", d)
	}
	cfg.Judge.DescribeTask = true
	if d, _ := Describe(cfg, New(cfg), ""); d != "" {
		t.Errorf("no task: %q", d)
	}
}

func TestCopilotDescribesTask(t *testing.T) {
	j := newCopilotJudge("gpt-5-mini", "copilot")
	j.send = func(ctx context.Context, system, user string) (string, error) {
		if !strings.Contains(system, "plain text") || !strings.Contains(user, goTask) {
			t.Errorf("prompt: %q / %q", system, user)
		}
		return goDetail, nil
	}
	cfg := testConfig()
	if d, err := Describe(cfg, j, goTask); err != nil || d != goDetail {
		t.Errorf("got %q %v", d, err)
	}
	j.send = func(context.Context, string, string) (string, error) { return "", errors.New("CLI not signed in") }
	if d, err := Describe(cfg, j, goTask); d != "" || err == nil {
		t.Errorf("failure: %q %v", d, err)
	}
}

// fakeLLM serves an OpenAI-style API: plain-text replies get the
// description, JSON-mode replies a verdict. It records every prompt.
type fakeLLM struct {
	mu      sync.Mutex
	prompts []string
	json    []bool
}

func (f *fakeLLM) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.prompts = append(f.prompts, req.Messages[1].Content)
		f.json = append(f.json, req.ResponseFormat != nil)
		f.mu.Unlock()
		reply := `{"category": "off_task", "confidence": 0.9}`
		if req.ResponseFormat == nil {
			reply = "**Goal:** learn Go channels and select.\n" + strings.SplitN(goDetail, "\n", 2)[1]
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": reply}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func llmConfig(srv *httptest.Server) config.Config {
	cfg := testConfig()
	cfg.Judge.Provider = "openai"
	cfg.OpenAI.BaseURL = srv.URL + "/v1"
	return cfg
}
