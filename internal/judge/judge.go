package judge

import (
	"context"
	"time"

	"focuslock/internal/config"
	"focuslock/internal/logx"
)

// A Judge answers: "what is the person looking at, given their task?"
// It returns a choice from Options plus probabilities (or a single
// confidence for models that can't give probabilities).
//
// Judges only classify. Everything around them (rules, caching, strict
// sites, explanations) lives in the Classifier, so a new judge is one file
// that calls registerJudge — see judge_jev.go, judge_openai.go,
// judge_copilot.go.

type Question struct {
	Intent string // current task, "" if none
	App    string // exe name, e.g. "chrome.exe"
	Title  string // window title
	Page   string // host+path, "" if unknown
	// Notes are things the person explained earlier in this session
	// ("hellointerview.com: practising Go interview questions").
	Notes []string
	// Explanation is set when the person is explaining why they need this
	// window. The judge then answers whether that's a good reason.
	Explanation string
}

type Answer struct {
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
}

type Judge interface {
	Name() string
	Ask(ctx context.Context, q Question) (Answer, error)
}

// Optional judge capabilities.
type (
	// timeoutJudge: judges that need longer than judge.timeoutMs (Copilot
	// starts an agent runtime) say so.
	timeoutJudge interface{ Timeout() time.Duration }
	// Closer: judges holding a process or connection.
	Closer interface{ Close() }
	// verdictJudge: judges that turn their own answers into a verdict,
	// instead of FocusLock's thresholds (tuned for Jev's probabilities).
	verdictJudge interface {
		Verdict(a Answer, strict bool, cfg config.JudgeConfig) Verdict
	}
)

// VerdictFor turns an answer into a verdict, letting the judge Decide if
// it knows better.
func VerdictFor(j Judge, a Answer, strict bool, cfg config.JudgeConfig) Verdict {
	if vj, ok := j.(verdictJudge); ok {
		return vj.Verdict(a, strict, cfg)
	}
	return Decide(a, cfg.Threshold, cfg.MusicThreshold, strict)
}

// ---------- registry ----------

type judgeProvider struct {
	// build returns nil, nil when the judge isn't usable (no API key...).
	build func(cfg config.Config) (Judge, error)
	// status is a one-line description for "focuslock start".
	status func(cfg config.Config) string
}

var judgeProviders = map[string]judgeProvider{}

func registerJudge(name string, p judgeProvider) { judgeProviders[name] = p }

// New builds the configured judge, or nil for rules only.
func New(cfg config.Config) Judge {
	p, ok := judgeProviders[cfg.Judge.Provider]
	if !ok {
		return nil
	}
	j, err := p.build(cfg)
	if err != nil {
		logx.Printf("judge %s: %v", cfg.Judge.Provider, err)
		return nil
	}
	return j
}

// Status says which judge will run, for "focuslock start".
func Status(cfg config.Config) string {
	p, ok := judgeProviders[cfg.Judge.Provider]
	if !ok {
		return "none — rules only"
	}
	return p.status(cfg)
}

func Timeout(j Judge, cfg config.Config) time.Duration {
	d := time.Duration(cfg.Judge.TimeoutMs) * time.Millisecond
	if t, ok := j.(timeoutJudge); ok && t.Timeout() > d {
		d = t.Timeout()
	}
	return d
}
