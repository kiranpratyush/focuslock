package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/session"
)

// Shared by the Windows and Linux builds.

func cmdStatus(dir string) {
	if s, ok := session.ReadActive(dir); ok {
		fmt.Printf("%s left (ends %s).\n", session.FmtRemaining(s.Remaining()), s.End().Format("15:04"))
		if s.Intent != "" {
			fmt.Printf("Task: %s\n", s.Intent)
		}
		return
	}
	fmt.Println("No active session.")
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		s = strconv.Itoa(n) + "m"
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 50m, 1h30m, or 45)", s)
	}
	return d, nil
}

func dataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		if base, err = os.UserConfigDir(); err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "FocusLock")
	return dir, os.MkdirAll(dir, 0o755)
}

// spellOutTask asks a judge that can write (Copilot, an LLM) to describe
// the task in four lines, before the session starts. On any problem the
// task is used as written.
func spellOutTask(cfg config.Config, task string) string {
	if task == "" || !cfg.Judge.DescribeTask {
		return ""
	}
	j := judge.New(cfg)
	if _, ok := j.(judge.Describer); !ok {
		return ""
	}
	if c, ok := j.(judge.Closer); ok {
		defer c.Close()
	}
	fmt.Println("Asking the judge to spell out your task…")
	detail, err := judge.Describe(cfg, j, task)
	if err != nil {
		fmt.Printf("Couldn't (%v); judging against the task as written.\n", err)
		return ""
	}
	fmt.Println("The judge will check tabs and apps against:")
	for _, l := range strings.Split(detail, "\n") {
		fmt.Println("  " + l)
	}
	return detail
}

func setupLog(dir string) {
	f, err := os.OpenFile(filepath.Join(dir, "focuslock.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	l := log.New(f, "", log.LstdFlags)
	logx.Printf = func(format string, a ...any) { l.Printf(format, a...) }
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
