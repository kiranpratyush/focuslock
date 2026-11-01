package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

// GitHub Copilot, through the Copilot CLI you're already signed in to.
// Meant for the work laptop: titles go to a tool the company approved.
//
// The SDK starts the CLI once and keeps it running; every question is a
// fresh session with all tools off and every permission refused, so the
// agent can only read the prompt and answer.

func init() {
	registerJudge("copilot", judgeProvider{
		build: func(cfg Config) (Judge, error) {
			path, err := copilotCLIPath(cfg)
			if err != nil {
				return nil, err
			}
			return newCopilotJudge(cfg.Copilot.Model, path), nil
		},
		status: func(cfg Config) string {
			path, err := copilotCLIPath(cfg)
			if err != nil {
				return "none (" + err.Error() + ") — rules only"
			}
			return "Copilot (" + cfg.Copilot.Model + ", via " + path + "). Check it with: focuslock judge-check"
		},
	})
}

func copilotCLIPath(cfg Config) (string, error) {
	if p := strings.TrimSpace(cfg.Copilot.CLIPath); p != "" {
		return p, nil
	}
	if p := os.Getenv("COPILOT_CLI_PATH"); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("copilot")
	if err != nil {
		return "", errors.New(`Copilot CLI not found on PATH; set "copilot": {"cliPath": ...} in focus.json`)
	}
	return p, nil
}

type copilotJudge struct {
	model string
	// send asks one question in a fresh session and returns the reply text.
	// A field so tests can replace the CLI.
	send func(ctx context.Context, system, user string) (string, error)

	mu      sync.Mutex
	client  *copilot.Client
	cliPath string
	failed  time.Time // last failed start; retried after a minute
}

func newCopilotJudge(model, cliPath string) *copilotJudge {
	j := &copilotJudge{model: model, cliPath: cliPath}
	j.send = j.sendViaCLI
	return j
}

func (j *copilotJudge) Name() string { return "copilot" }

// Timeout: the first question also starts the CLI, and an agent turn is
// slower than a plain classifier.
func (j *copilotJudge) Timeout() time.Duration { return 45 * time.Second }

func (j *copilotJudge) Ask(ctx context.Context, q Question) (Answer, error) {
	system, user := llmPrompt(q)
	reply, err := j.send(ctx, system, user)
	if err != nil {
		return Answer{}, err
	}
	return parseLLMAnswer(reply, Options(q.Intent))
}

func (j *copilotJudge) start(ctx context.Context) (*copilot.Client, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.client != nil {
		return j.client, nil
	}
	if time.Since(j.failed) < time.Minute {
		return nil, errors.New("copilot CLI failed to start recently; waiting before retrying")
	}
	c := copilot.NewClient(&copilot.ClientOptions{
		Connection: copilot.StdioConnection{Path: j.cliPath},
		LogLevel:   "error",
	})
	if err := c.Start(ctx); err != nil {
		j.failed = time.Now()
		return nil, fmt.Errorf("start copilot CLI (%s): %w", j.cliPath, err)
	}
	j.client = c
	return c, nil
}

func (j *copilotJudge) sendViaCLI(ctx context.Context, system, user string) (string, error) {
	c, err := j.start(ctx)
	if err != nil {
		return "", err
	}
	off := false
	session, err := c.CreateSession(ctx, &copilot.SessionConfig{
		ClientName:             "focuslock",
		Model:                  j.model,
		SystemMessage:          &copilot.SystemMessageConfig{Mode: "replace", Content: system},
		ExcludedTools:          copilot.NewToolSet().AddBuiltIn("*").AddMCP("*").ToSlice(),
		OnPermissionRequest:    refuseAll,
		EnableConfigDiscovery:  &off,
		EnableSkills:           &off,
		EnableSessionStore:     &off,
		SkipCustomInstructions: ptr(true),
		Streaming:              &off,
	})
	if err != nil {
		return "", fmt.Errorf("copilot session: %w", err)
	}
	defer session.Disconnect()
	ev, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: user})
	if err != nil {
		return "", err
	}
	if ev == nil {
		return "", errors.New("copilot gave no answer")
	}
	msg, ok := ev.Data.(*copilot.AssistantMessageData)
	if !ok {
		return "", fmt.Errorf("copilot: unexpected reply %T", ev.Data)
	}
	return msg.Content, nil
}

// refuseAll answers every tool permission request with no. With all tools
// excluded none should come, but if one does, it's not allowed.
func refuseAll(copilot.PermissionRequest, copilot.PermissionInvocation) (rpc.PermissionDecision, error) {
	why := "FocusLock only classifies; no tools allowed"
	return &rpc.PermissionDecisionReject{Feedback: &why}, nil
}

func (j *copilotJudge) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.client != nil {
		_ = j.client.Stop()
		j.client = nil
	}
}

// Models lists what this Copilot account can use, cheapest first, for
// "focuslock judge-check".
func (j *copilotJudge) Models(ctx context.Context) ([]string, error) {
	c, err := j.start(ctx)
	if err != nil {
		return nil, err
	}
	ms, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	mult := func(m copilot.ModelInfo) float64 {
		if m.Billing != nil && m.Billing.Multiplier != nil {
			return *m.Billing.Multiplier
		}
		return -1
	}
	sort.SliceStable(ms, func(a, b int) bool { return mult(ms[a]) < mult(ms[b]) })
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		line := m.ID
		if x := mult(m); x >= 0 {
			line += fmt.Sprintf("  (%gx premium requests)", x)
		}
		if m.Capabilities.Supports.Vision {
			line += "  [images]"
		}
		out = append(out, line)
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }
