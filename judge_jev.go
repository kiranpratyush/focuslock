package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Jev (TypeSafe): a classifier API that returns real probabilities.

func init() {
	registerJudge("jev", judgeProvider{
		build: func(cfg Config) (Judge, error) {
			if !cfg.Jev.Enabled {
				return nil, nil // old configs: "jev": {"enabled": false}
			}
			key := os.Getenv(cfg.Jev.APIKeyEnv)
			if key == "" {
				return nil, nil
			}
			client := &http.Client{Timeout: time.Duration(cfg.Judge.TimeoutMs) * time.Millisecond}
			return &jevJudge{endpoint: cfg.Jev.Endpoint, model: cfg.Jev.Model, key: key, http: client}, nil
		},
		status: func(cfg Config) string {
			switch {
			case !cfg.Jev.Enabled:
				return "none (jev.enabled is false) — rules only"
			case os.Getenv(cfg.Jev.APIKeyEnv) == "":
				return "none (" + cfg.Jev.APIKeyEnv + " not set) — rules only"
			}
			return "Jev (" + cfg.Jev.Model + ")"
		},
	})
}

type jevJudge struct {
	endpoint, model, key string
	http                 *http.Client
}

func (j *jevJudge) Name() string { return "jev" }

type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevRequest struct {
	Model     string                 `json:"model"`
	State     map[string]string      `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type jevResponse struct {
	Model   string               `json:"model"`
	Answers map[string]jevAnswer `json:"answers"`
}

func (j *jevJudge) Ask(ctx context.Context, q Question) (Answer, error) {
	state := map[string]string{"app": q.App, "window_title": q.Title}
	if q.Page != "" {
		state["page"] = q.Page
	}
	if q.Intent != "" {
		state["current_task"] = q.Intent
	}
	if len(q.Notes) > 0 {
		state["notes_from_this_session"] = strings.Join(q.Notes, "\n")
	}
	if q.Explanation != "" {
		state["their_explanation"] = q.Explanation
	}
	req := jevRequest{
		Model: j.model,
		State: state,
		Questions: map[string]jevQuestion{"category": {
			Type: "choice", Instructions: instructions(q), Criteria: Options(q.Intent),
		}},
	}
	var jr jevResponse
	if err := postJSON(ctx, j.http, j.endpoint, j.key, req, &jr); err != nil {
		return Answer{}, err
	}
	a, ok := jr.Answers["category"]
	if !ok {
		return Answer{}, fmt.Errorf("no 'category' answer in response")
	}
	return Answer{Choice: a.Choice, Confidence: a.Confidence, Probabilities: a.Probabilities}, nil
}
