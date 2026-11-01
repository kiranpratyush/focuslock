package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// judgeCheck asks the configured judge a few sample questions and prints
// what it says and how long it took: "focuslock judge-check [task]".
func judgeCheck(cfg Config, intent string, w io.Writer) {
	fmt.Fprintf(w, "Judge: %s\n", judgeStatus(cfg))
	j := newJudge(cfg)
	if j == nil {
		fmt.Fprintln(w, "No judge to check — rules only.")
		return
	}
	if c, ok := j.(closingJudge); ok {
		defer c.Close()
	}
	if intent == "" {
		intent = "Learn Go channels and select: buffered vs unbuffered, closing channels, worker pools"
	}
	fmt.Fprintf(w, "Task: %s\n", intent)
	asked := intent
	if detail, err := describeTask(cfg, j, intent); err != nil {
		fmt.Fprintf(w, "Couldn't spell out the task: %v\n", err)
	} else if detail != "" {
		fmt.Fprintf(w, "As the judge spelled it out:\n  %s\n", strings.ReplaceAll(detail, "\n", "\n  "))
		asked = detail
	}
	fmt.Fprintln(w)

	if m, ok := j.(interface {
		Models(context.Context) ([]string, error)
	}); ok {
		ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout(j, cfg))
		models, err := m.Models(ctx)
		cancel()
		if err != nil {
			fmt.Fprintf(w, "Couldn't list models: %v\n\n", err)
		} else {
			fmt.Fprintln(w, "Models you can use (set one as \"model\" in focus.json):")
			for _, s := range models {
				fmt.Fprintln(w, "  "+s)
			}
			fmt.Fprintln(w)
		}
	}

	samples := []Question{
		{App: "brave.exe", Title: "Go by Example: Channels - Brave", Page: "gobyexample.com/channels"},
		{App: "brave.exe", Title: "Funniest fails 2026 - YouTube - Brave", Page: "youtube.com/watch"},
		{App: "brave.exe", Title: "System Design Tutorial - GeeksforGeeks - Brave", Page: "geeksforgeeks.org/system-design/system-design-tutorial/"},
		{App: "brave.exe", Title: "System Design Tutorial - GeeksforGeeks - Brave", Page: "geeksforgeeks.org/system-design/system-design-tutorial/",
			Explanation: "research"},
	}
	for _, q := range samples {
		q.Intent = asked
		label := q.Title
		if q.Explanation != "" {
			label += "  + explanation \"" + q.Explanation + "\""
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout(j, cfg))
		a, err := j.Ask(ctx, q)
		cancel()
		took := time.Since(start).Round(10 * time.Millisecond)
		if err != nil {
			fmt.Fprintf(w, "%-75s  ERROR after %s: %v\n", shorten(label, 75), took, err)
			continue
		}
		v := judgeVerdict(j, a, q.Explanation != "", cfg.Judge)
		fmt.Fprintf(w, "%-75s  %-8s [%s %.2f]  %s\n", shorten(label, 75), v.Cat, a.Choice, a.Confidence, took)
	}
}
