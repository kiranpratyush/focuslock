//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/judge"
)

// On Linux this binary is the engine (installed as focuslock-engine): it
// starts sessions, judges windows, and blocks. The Qt app "focuslock" asks
// for the session and draws the bar and the block screen.

const (
	guardExeName  = "focuslock-guard"
	workerExeName = "focuslock-worker"
	uiExeName     = "focuslock"
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
	case "config":
		_, _ = config.Load(dir)
		fmt.Println(filepath.Join(dir, "focus.json"))
	case "last-setup":
		// For the Qt start window: what to pre-fill.
		printJSON(config.LoadLastSetup(dir))
	case "judge-check":
		cfg, err := config.Load(dir)
		if err != nil {
			fatal(fmt.Errorf("focus.json: %w", err))
		}
		judge.Check(cfg, strings.Join(args[1:], " "), os.Stdout)
	case "judge-eval":
		cfg, err := config.Load(dir)
		if err != nil {
			fatal(fmt.Errorf("focus.json: %w", err))
		}
		if err := classify.EvalCommand(cfg, filepath.Join(dir, "focuslock.log"), filepath.Join(dir, "eval-cases.jsonl"), args[1:], os.Stdout); err != nil {
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
	fmt.Println(`FocusLock engine — blocks entertainment until the timer ends (Linux, Hyprland).

  focuslock-engine start [50m "task"]          opens the app's start window, pre-filled
  focuslock-engine start 50m "task" --no-prompt [--sites "a.com, b.com"] [--apps "postman"]
  focuslock-engine status [--json]             show time left and the task
  focuslock-engine config                      print the path of focus.json
  focuslock-engine judge-check ["task"]        try the judge on a few sample pages
  focuslock-engine judge-eval [make]           score the judge on your labelled cases

Run "focuslock" (the app) for the start window. There is no stop command.
The session ends when the timer runs out or you restart the computer.`)
}
