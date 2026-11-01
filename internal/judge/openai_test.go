package judge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIJudge(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		// Small models often wrap JSON in a code fence.
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
			"message": map[string]string{"role": "assistant", "content": "```json\n{\"category\": \"entertainment\", \"confidence\": 0.9}\n```"},
		}}})
	}))
	defer srv.Close()
	cfg := testConfig()
	cfg.Judge.Provider = "openai"
	cfg.OpenAI.BaseURL = srv.URL + "/v1"
	j := New(cfg)
	a, err := j.Ask(context.Background(), Question{
		Intent: "write SQL migration", App: "chrome.exe", Title: "Funniest fails 2026 - YouTube", Page: "youtube.com/watch",
	})
	if err != nil || j.Name() != "llm" {
		t.Fatalf("ask: %v (judge %q)", err, j.Name())
	}
	if v := Decide(a, 0.75, 0.9, false); v.Cat != CatEntertainment {
		t.Fatalf("got %+v from %+v", v, a)
	}
	if got.Model != "qwen2.5:3b" || got.Temperature != 0 || got.ResponseFormat["type"] != "json_object" {
		t.Errorf("request: %+v", got)
	}
	user := got.Messages[1].Content
	for _, want := range []string{"Current task: write SQL migration", "Page: youtube.com/watch", "on_task:"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q:\n%s", want, user)
		}
	}
}

func TestParseLLMAnswer(t *testing.T) {
	opts := Options("a task")
	ok := []struct {
		in   string
		cat  string
		conf float64
	}{
		{`{"category":"on_task","confidence":0.7}`, "on_task", 0.7},
		{`Sure! {"category": "OFF_TASK", "confidence": 85}`, "off_task", 0.85},
		{`{"category":"supporting"}`, "supporting", 0.8},
	}
	for _, tc := range ok {
		a, err := parseLLMAnswer(tc.in, opts)
		if err != nil || a.Choice != tc.cat || a.Confidence != tc.conf {
			t.Errorf("%q: got %+v, %v", tc.in, a, err)
		}
	}
	for _, bad := range []string{"entertainment", `{"category":"gaming"}`, `{oops}`} {
		if _, err := parseLLMAnswer(bad, opts); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
