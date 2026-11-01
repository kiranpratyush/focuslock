//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/session"
)

const (
	guardExeName  = "focuslock-guard.exe"
	workerExeName = "focuslock-worker.exe"

	DETACHED_PROCESS         = 0x00000008
	CREATE_NEW_PROCESS_GROUP = 0x00000200
)

func main() {
	dir, err := dataDir()
	if err != nil {
		fatal(err)
	}
	setupLog(dir)

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		return
	}
	switch args[0] {
	case "start":
		cmdStart(dir, args[1:])
	case "status":
		cmdStatus(dir)
	case "config":
		_, _ = config.Load(dir)
		fmt.Println(filepath.Join(dir, "focus.json"))
	case "cue":
		cfg, err := config.Load(dir)
		if err != nil {
			logx.Printf("config error, using defaults: %v", err)
			cfg = config.Default()
			cfg.Normalize()
		}
		runCue(dir, cfg, len(args) > 1 && args[1] == "--hello")
	case "startup":
		cmdStartup(dir, args[1:])
	case "judge-check":
		cfg, err := config.Load(dir)
		if err != nil {
			fatal(fmt.Errorf("focus.json: %w", err))
		}
		stop := superviseJevos(cfg, false)
		judge.Check(cfg, strings.Join(args[1:], " "), os.Stdout)
		stop()
	case "judge-eval":
		cfg, err := config.Load(dir)
		if err != nil {
			fatal(fmt.Errorf("focus.json: %w", err))
		}
		stop := superviseJevos(cfg, false)
		err = classify.EvalCommand(cfg, filepath.Join(dir, "focuslock.log"), filepath.Join(dir, "eval-cases.jsonl"), args[1:], os.Stdout)
		stop()
		if err != nil {
			fatal(err)
		}
	case "guard":
		runGuard(dir, peerArg(args))
	case "worker":
		runWorker(dir, peerArg(args))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println(`FocusLock — blocks entertainment until the timer ends.

  focuslock start                              opens a window: how long, your task, extra sites/apps
  focuslock start 50m "fix pagination bug"     same window, pre-filled
  focuslock start 50m "task" --no-prompt       start without the window
  focuslock status                             show time left and the task
  focuslock config                             print the path of focus.json
  focuslock startup on | off | status          remind you to plan a session when you sign in
  focuslock judge-check ["task"]               try the judge on a few sample pages
  focuslock judge-eval make                    turn your log into test cases to label
  focuslock judge-eval                         score the judge on your labelled cases

Durations: 50m, 1h30m, or just 45 (minutes).
There is no stop command. The session ends when the timer runs out or you restart the PC.
Who judges unknown tabs/apps is set in focus.json -> "judge": "jev", "openai" (Ollama,
LM Studio, any OpenAI-compatible API), "copilot" (GitHub Copilot CLI), "jevos" (local
open-source Jev) or "none".`)
}

func cmdStart(dir string, args []string) {
	if s, ok := session.ReadActive(dir); ok {
		fmt.Printf("A session is already running: %s left.\n", session.FmtRemaining(s.Remaining()))
		return
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fatal(fmt.Errorf("focus.json: %w", err))
	}

	// Pre-fill from the last session, then from the command line.
	setup := config.LoadLastSetup(dir)
	prompt := true
	var words []string
	for _, a := range args {
		if a == "--no-prompt" {
			prompt = false
		} else {
			words = append(words, a)
		}
	}
	if len(words) > 0 {
		if _, err := parseDuration(words[0]); err == nil {
			setup.Duration, words = words[0], words[1:]
		}
	}
	if len(words) > 0 {
		setup.Task = strings.Join(words, " ")
	}
	if prompt {
		var ok bool
		if setup, ok = askSetup(setup, cfg); !ok {
			fmt.Println("Cancelled — no session started.")
			return
		}
	}
	d, err := parseDuration(setup.Duration)
	if err != nil {
		fatal(err)
	}
	config.SaveLastSetup(dir, setup)
	intent := strings.TrimSpace(setup.Task)
	detail := spellOutTask(cfg, intent)

	// Two copies under different names: killing "focuslock.exe" by name
	// doesn't touch them, and each one restarts the other.
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	for _, name := range []string{guardExeName, workerExeName} {
		if err := copyFile(self, filepath.Join(dir, name)); err != nil {
			fatal(fmt.Errorf("copy %s: %w", name, err))
		}
	}

	s := session.State{EndUnix: time.Now().Add(d).Unix(), BootUnix: session.BootTime().Unix(), Intent: intent, TaskDetail: detail,
		AllowDomains: setup.AllowDomains, AllowApps: setup.AllowApps}
	if err := session.Write(dir, s); err != nil {
		fatal(err)
	}
	if _, err := spawn(filepath.Join(dir, guardExeName), "guard", "--peer", "0"); err != nil {
		fatal(err)
	}

	fmt.Printf("Focus session started: %s (until %s).\n", session.FmtRemaining(d), s.End().Format("15:04"))
	if intent != "" {
		fmt.Printf("Task: %s\n", intent)
	}
	if len(s.AllowDomains)+len(s.AllowApps) > 0 {
		fmt.Printf("Also allowed this session: %s\n", strings.Join(append(append([]string{}, s.AllowDomains...), s.AllowApps...), ", "))
	}
	fmt.Printf("Judge: %s\nLog: %s\n", judge.Status(cfg), filepath.Join(dir, "focuslock.log"))
	if intent != "" && judge.New(cfg) == nil {
		fmt.Println("Note: without a judge, the task is shown but can't be checked — only the rules apply.")
	}
}

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
