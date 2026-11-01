package judge

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"focuslock/internal/config"
)

// Works with Ollama (http://localhost:11434/v1), LM Studio (http://localhost:1234/v1),
// OpenAI, Groq, OpenRouter, and anything else that speaks /chat/completions.

func init() {
	registerJudge("openai", judgeProvider{
		build: func(cfg config.Config) (Judge, error) {
			return &openAIJudge{
				baseURL:  strings.TrimRight(cfg.OpenAI.BaseURL, "/"),
				model:    cfg.OpenAI.Model,
				key:      os.Getenv(cfg.OpenAI.APIKeyEnv),
				jsonMode: cfg.OpenAI.JSONMode,
				http:     &http.Client{Timeout: time.Duration(cfg.Judge.TimeoutMs) * time.Millisecond},
			}, nil
		},
		// Also checks a local server is up.
		status: func(cfg config.Config) string {
			st := cfg.OpenAI.Model + " at " + cfg.OpenAI.BaseURL
			c := &http.Client{Timeout: 2 * time.Second}
			if resp, err := c.Get(strings.TrimRight(cfg.OpenAI.BaseURL, "/") + "/models"); err != nil {
				st += "  (WARNING: not reachable — is Ollama/LM Studio running? Until it is, rules only)"
			} else {
				resp.Body.Close()
			}
			return st
		},
	})
}

type openAIJudge struct {
	baseURL, model, key string
	jsonMode            bool
	http                *http.Client
}

func (o *openAIJudge) Name() string { return "llm" }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	MaxTokens      int               `json:"max_tokens"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (o *openAIJudge) Ask(ctx context.Context, q Question) (Answer, error) {
	system, user := llmPrompt(q)
	reply, err := o.chat(ctx, system, user, 60, o.jsonMode)
	if err != nil {
		return Answer{}, err
	}
	return parseLLMAnswer(reply, Options(q.Intent))
}

// DescribeTask: see task_detail.go.
func (o *openAIJudge) DescribeTask(ctx context.Context, system, user string) (string, error) {
	return o.chat(ctx, system, user, 220, false)
}

func (o *openAIJudge) chat(ctx context.Context, system, user string, maxTokens int, jsonMode bool) (string, error) {
	req := chatRequest{
		Model:       o.model,
		Messages:    []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: 0,
		MaxTokens:   maxTokens,
	}
	if jsonMode {
		req.ResponseFormat = map[string]string{"type": "json_object"}
	}
	var cr chatResponse
	if err := postJSON(ctx, o.http, o.baseURL+"/chat/completions", o.key, req, &cr); err != nil {
		return "", err
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty reply")
	}
	return cr.Choices[0].Message.Content, nil
}
