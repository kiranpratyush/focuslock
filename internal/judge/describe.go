package judge

import (
	"context"
	"errors"
	"strings"

	"focuslock/internal/config"
)

// Spelling out the task: "learn go channels" is easy for a person to apply
// and vague for a judge. When the judge can write (Copilot, an LLM), it's
// asked once per session to turn the task into four short lines, and every
// later question uses those. What you see (the bar, the block screen) keeps
// your own wording.

// Describer: judges that can write text, not just classify.
type Describer interface {
	DescribeTask(ctx context.Context, system, user string) (string, error)
}

func describeTaskPrompt(task string) (system, user string) {
	system = "You help a focus app understand what someone is working on. Reply with plain text only: no markdown, no headings, no bullets."
	user = "Spell out this focus-session task in exactly 4 short lines, for a judge that decides whether each browser tab or app belongs to the task.\n" +
		"Line 1: Goal: the goal in one sentence.\n" +
		"Line 2: Topics: related topics and concepts, comma-separated.\n" +
		"Line 3: Resources: the kinds of sites, docs, videos or tools someone would use for it.\n" +
		"Line 4: Not part of it: nearby topics that are easy to drift into but don't help this task.\n" +
		"Keep the person's meaning. Don't add goals they didn't mention.\n\n" +
		"Task: " + task
	return system, user
}

// Describe asks the judge to spell out task. "" with no error means the
// judge can't, or describing is turned off: use the task as written.
func Describe(cfg config.Config, j Judge, task string) (string, error) {
	task = strings.TrimSpace(task)
	d, ok := j.(Describer)
	if !ok || !cfg.Judge.DescribeTask || task == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout(j, cfg))
	defer cancel()
	system, user := describeTaskPrompt(task)
	reply, err := d.DescribeTask(ctx, system, user)
	if err != nil {
		return "", err
	}
	detail := cleanTaskDetail(reply)
	if detail == "" {
		return "", errors.New("the judge gave an empty description")
	}
	return detail, nil
}

// cleanTaskDetail keeps up to four non-empty lines, without markdown, and
// caps the length so every question stays small.
func cleanTaskDetail(s string) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-*#>•0123456789. "))
		l = strings.ReplaceAll(l, "**", "")
		if l != "" {
			lines = append(lines, l)
		}
		if len(lines) == 4 {
			break
		}
	}
	out := strings.Join(lines, "\n")
	if r := []rune(out); len(r) > 700 {
		out = string(r[:700])
	}
	return out
}
