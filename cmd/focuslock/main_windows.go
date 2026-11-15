//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
)

const (
	guardExeName  = "focuslock-guard.exe"
	workerExeName = "focuslock-worker.exe"
	uiExeName     = "focuslock.exe" // the Qt app, beside this engine

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
		if len(args) > 1 && args[1] == "--json" {
			cmdStatusJSON(dir)
		} else {
			cmdStatus(dir)
		}
	case "last-setup":
		// For the app's start window: what to pre-fill.
		printJSON(config.LoadLastSetup(dir))
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

// openUI starts the app's start window and leaves it running.
func openUI(path string) error {
	c := exec.Command(path, "open")
	c.Env = os.Environ()
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

func lookPath(name string) (string, error) { return exec.LookPath(name) }

func printJSON(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
