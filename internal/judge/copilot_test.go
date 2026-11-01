package judge

import (
	"context"
	"strings"
	"testing"
)

func TestCopilotJudgePrompt(t *testing.T) {
	var system, user string
	j := newCopilotJudge("gpt-5-mini", "copilot")
	j.send = func(ctx context.Context, s, u string) (string, error) {
		system, user = s, u
		return "```json\n{\"category\": \"on_task\", \"confidence\": 0.88}\n```", nil
	}
	a, err := j.Ask(context.Background(), Question{
		Intent: goTask, App: "brave.exe", Title: "Go by Example: Channels - Brave", Page: "gobyexample.com/channels",
		Notes: []string{"Allowed hellointerview.com because: practising Go questions"}, Explanation: "channel examples",
	})
	if err != nil || a.Choice != "on_task" || a.Confidence != 0.88 {
		t.Fatalf("got %+v, %v", a, err)
	}
	if !strings.Contains(system, "JSON only") {
		t.Errorf("system prompt: %q", system)
	}
	for _, want := range []string{"Current task: " + goTask, "Page: gobyexample.com/channels", "practising Go questions", "Their explanation: channel examples", "on_task:"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q:\n%s", want, user)
		}
	}
}

func TestCopilotNeedsCLI(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("COPILOT_CLI_PATH", "")
	cfg := testConfig()
	cfg.Judge.Provider = "copilot"
	if New(cfg) != nil {
		t.Error("no CLI on PATH should mean rules only")
	}
	if st := Status(cfg); !strings.Contains(st, "not found") {
		t.Errorf("status: %q", st)
	}
	cfg.Copilot.CLIPath = `C:\tools\copilot.exe`
	if j := New(cfg); j == nil || j.Name() != "copilot" {
		t.Errorf("explicit cliPath: got %v", j)
	}
}
