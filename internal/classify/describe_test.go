package classify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"focuslock/internal/config"
	"focuslock/internal/judge"
)

const goDetail = "Goal: learn Go channels and select.\n" +
	"Topics: buffered and unbuffered channels, closing channels, select, worker pools, goroutines.\n" +
	"Resources: go.dev docs, Go by Example, Go concurrency talks and tutorials.\n" +
	"Not part of it: system design, other languages, general backend roadmaps."

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

func TestDescribeTaskWithLLM(t *testing.T) {
	f := &fakeLLM{}
	cfg := llmConfig(f.server(t))
	j := judge.New(cfg)
	detail, err := judge.Describe(cfg, j, goTask)
	if err != nil || detail != goDetail {
		t.Fatalf("detail %q, err %v", detail, err)
	}
	if f.json[0] || !strings.Contains(f.prompts[0], "Task: "+goTask) || !strings.Contains(f.prompts[0], "Not part of it") {
		t.Errorf("describe request: json=%v prompt=%q", f.json[0], f.prompts[0])
	}

	// Later questions are judged against the spelled-out task...
	c := New(cfg, goTask)
	c.judge = j
	c.SetTaskDetail(detail)
	waitVerdict(t, c, "brave.exe", "System Design Tutorial - GeeksforGeeks - Brave", "https://www.geeksforgeeks.org/system-design/x")
	f.mu.Lock()
	last := f.prompts[len(f.prompts)-1]
	f.mu.Unlock()
	if !strings.Contains(last, "Current task:\n"+goDetail) {
		t.Errorf("question doesn't carry the detail:\n%s", last)
	}
	// ...but what you see keeps your wording.
	if c.Intent() != goTask {
		t.Errorf("Intent() = %q", c.Intent())
	}
}
