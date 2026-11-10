//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/session"
)

// On Linux this binary is the engine (installed as focuslock-engine): it
// starts sessions, judges windows, and blocks. The Qt app "focuslock" asks
// for the session and draws the bar and the block screen.

const (
	guardExeName  = "focuslock-guard"
	workerExeName = "focuslock-worker"
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

  focuslock-engine start 50m "fix pagination bug" [--sites "a.com, b.com"] [--apps "postman"]
  focuslock-engine status [--json]             show time left and the task
  focuslock-engine config                      print the path of focus.json
  focuslock-engine judge-check ["task"]        try the judge on a few sample pages
  focuslock-engine judge-eval [make]           score the judge on your labelled cases

Run "focuslock" (the app) for the start window. There is no stop command.
The session ends when the timer runs out or you restart the computer.`)
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

	setup := config.LoadLastSetup(dir)
	var words []string
	sites, apps := setup.AllowDomains, setup.AllowApps
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--no-prompt":
		case (a == "--sites" || a == "--apps") && i+1 < len(args):
			i++
			if a == "--sites" {
				sites = config.ParseDomainList(args[i])
			} else {
				apps = config.ParseAppList(args[i])
			}
		default:
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		// No duration given: the app asks for one.
		if ui, err := uiExe(); err == nil {
			_ = syscallExec(ui)
		}
		fatal(fmt.Errorf("give a duration, e.g. focuslock-engine start 50m \"task\""))
	}
	if _, err := parseDuration(words[0]); err == nil {
		setup.Duration, words = words[0], words[1:]
	}
	if len(words) > 0 {
		setup.Task = strings.Join(words, " ")
	}
	setup.AllowDomains, setup.AllowApps = sites, apps
	d, err := parseDuration(setup.Duration)
	if err != nil {
		fatal(err)
	}
	config.SaveLastSetup(dir, setup)
	intent := strings.TrimSpace(setup.Task)
	detail := spellOutTask(cfg, intent)

	// Two copies under different names; each one restarts the other.
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	for _, name := range []string{guardExeName, workerExeName} {
		if err := copyExe(self, filepath.Join(dir, name)); err != nil {
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
	fmt.Printf("Judge: %s\nLog: %s\n", judge.Status(cfg), filepath.Join(dir, "focuslock.log"))
	if intent != "" && judge.New(cfg) == nil {
		fmt.Println("Note: without a judge, the task is shown but can't be checked — only the rules apply.")
	}
}

func cmdStatusJSON(dir string) {
	out := map[string]any{"active": false}
	if s, ok := session.ReadActive(dir); ok {
		out = map[string]any{"active": true, "endUnix": s.EndUnix, "task": s.Intent}
	}
	printJSON(out)
}

// uiExe finds the Qt app: $FOCUSLOCK_UI, else "focuslock" on PATH.
func uiExe() (string, error) {
	if p := os.Getenv("FOCUSLOCK_UI"); p != "" {
		return p, nil
	}
	return exec.LookPath("focuslock")
}
