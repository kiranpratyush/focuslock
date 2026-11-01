package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// jevos (github.com/feder-cr/jev): an open-source Jev that runs on your
// CPU. Nothing leaves the machine.
//
// It speaks Jev's API, but it judges very differently: tested on 86 real
// decisions, Jev's multiple-choice question scored 57% with it. Direct
// yes/no questions with the task written into the question scored 94%
// (Jev: 95%). So this judge asks those, and makes its own decision from
// them (Verdict) instead of FocusLock's Jev-tuned thresholds.

func init() {
	registerJudge("jevos", judgeProvider{
		build: func(cfg Config) (Judge, error) {
			return newJevosJudge(cfg), nil
		},
		status: func(cfg Config) string {
			st := "jevos at " + cfg.Jevos.Endpoint
			switch {
			case jevosHealthy(cfg.Jevos.Endpoint):
				st += " (running)"
			case cfg.Jevos.Exe != "":
				st += " (FocusLock starts " + cfg.Jevos.Exe + ")"
			default:
				st += `  (WARNING: not running, and "jevos": {"exe": ...} isn't set. Until it runs, rules only)`
			}
			return st
		},
	})
}

type jevosJudge struct {
	endpoint, key string
	http          *http.Client
	ready         atomic.Bool
}

func newJevosJudge(cfg Config) *jevosJudge {
	return &jevosJudge{
		endpoint: cfg.Jevos.Endpoint,
		key:      os.Getenv(cfg.Jevos.APIKeyEnv),
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

func (j *jevosJudge) Name() string { return "jevos" }

// Timeout: the first questions wait for the server to finish loading.
func (j *jevosJudge) Timeout() time.Duration { return 30 * time.Second }

// The yes/no questions. Keys double as Answer.Probabilities keys.
const (
	jqTopic   = "topic"
	jqTool    = "tool"
	jqFun     = "fun"
	jqWork    = "work"
	jqMusic   = "music"
	jqExplain = "explanation"
)

func jevosQuestions(q Question) (state string, qs map[string]string) {
	if q.Explanation != "" {
		// Judged on the reason alone: with the window in the text too, jevos
		// rated vague reasons higher than specific ones.
		topic := q.Intent
		if topic == "" {
			topic = "work or learning: coding, documentation, technical tutorials or talks"
		}
		return "Explanation: " + q.Explanation, map[string]string{
			jqExplain: "Is this explanation about " + topic + "?",
		}
	}
	var b strings.Builder
	b.WriteString("App: " + q.App + "\nWindow title: " + q.Title)
	if q.Page != "" {
		b.WriteString("\nAddress: " + q.Page)
	}
	for _, n := range q.Notes {
		b.WriteString("\nEarlier note: " + n)
	}
	qs = map[string]string{
		jqFun: "Is this entertainment or social media: videos for fun, music videos, trailers, shows, sports, news, memes, shopping, or a social feed?",
	}
	if q.Intent != "" {
		qs[jqTopic] = "Is this page about this topic: " + q.Intent + "?"
		qs[jqTool] = "Is this a general work tool: email, team chat, calendar, notes, a terminal, a file manager, an AI assistant, or a web search results page?"
	} else {
		qs[jqWork] = "Is this for work or learning: coding, documentation, technical tutorials, talks or courses, or work tools?"
		qs[jqMusic] = "Is this background music only: a song's audio, a playlist, or a lofi stream (not a music video)?"
	}
	return b.String(), qs
}

func (j *jevosJudge) Ask(ctx context.Context, q Question) (Answer, error) {
	if err := j.waitReady(ctx); err != nil {
		return Answer{}, err
	}
	state, qs := jevosQuestions(q)
	type noul struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
	}
	req := map[string]any{"model": "jev-latest", "state": state}
	questions := map[string]noul{}
	for k, s := range qs {
		questions[k] = noul{"noul", s}
	}
	req["questions"] = questions
	var resp struct {
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := postJSON(ctx, j.http, j.endpoint, j.key, req, &resp); err != nil {
		j.ready.Store(false) // the server may have died; wait for it next time
		return Answer{}, err
	}
	probs := map[string]float64{}
	for k := range qs {
		a, ok := resp.Answers[k]
		if !ok || a.Noul == nil {
			return Answer{}, fmt.Errorf("jevos: no answer for %q", k)
		}
		probs[k] = *a.Noul
	}
	v := jevosDecide(probs)
	return Answer{Choice: jevosChoice(probs, v), Confidence: v.Prob, Probabilities: probs}, nil
}

// Verdict: jevos makes its own call (see jevosDecide); FocusLock's strict
// bar is tuned for Jev's probabilities and cost jevos 8 points in testing.
func (j *jevosJudge) Verdict(a Answer, strict bool, cfg JudgeConfig) Verdict {
	return jevosDecide(a.Probabilities)
}

// jevosDecide: plain 0.5 cut-offs on the yes/no answers, as tested.
//   - Explanation: accepted if it's about the task.
//   - With a task: allowed if it's about the task or a general work tool.
//   - Without one: blocked if it's entertainment and not work or music.
func jevosDecide(p map[string]float64) Verdict {
	if e, ok := p[jqExplain]; ok {
		if e >= 0.5 {
			return Verdict{Cat: CatAllow, Prob: e}
		}
		return Verdict{Cat: CatOffTask, Prob: 1 - e}
	}
	if topic, ok := p[jqTopic]; ok {
		best := max(topic, p[jqTool])
		switch {
		case best >= 0.5:
			return Verdict{Cat: CatAllow, Prob: best}
		case p[jqFun] >= 0.5:
			return Verdict{Cat: CatEntertainment, Prob: p[jqFun]}
		}
		return Verdict{Cat: CatOffTask, Prob: 1 - best}
	}
	switch {
	case p[jqWork] >= 0.5:
		return Verdict{Cat: CatAllow, Prob: p[jqWork]}
	case p[jqMusic] >= 0.5:
		return Verdict{Cat: CatMusic, Prob: p[jqMusic]}
	case p[jqFun] >= 0.5:
		return Verdict{Cat: CatEntertainment, Prob: p[jqFun]}
	}
	return Verdict{Cat: CatAllow, Prob: 1 - p[jqFun]}
}

// jevosChoice names the answer for the log, like Jev's choice.
func jevosChoice(p map[string]float64, v Verdict) string {
	switch {
	case v.Cat == CatEntertainment:
		return "entertainment"
	case v.Cat == CatMusic:
		return "music"
	case v.Cat == CatOffTask:
		return "off_task"
	case p[jqExplain] > 0:
		return "explained"
	case p[jqTopic] >= p[jqTool] && p[jqTopic] > 0:
		return "on_task"
	case p[jqTool] > 0:
		return "supporting"
	}
	return "work"
}

// waitReady waits until the server reports ready (it takes a few seconds
// to load the model after FocusLock starts it).
func (j *jevosJudge) waitReady(ctx context.Context) error {
	if j.ready.Load() {
		return nil
	}
	for {
		if jevosHealthy(j.endpoint) {
			j.ready.Store(true)
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("jevos isn't running at " + j.endpoint)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// jevosHealthy asks GET /health on the server behind endpoint.
func jevosHealthy(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	u.Path, u.RawQuery = "/health", ""
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(u.String())
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
