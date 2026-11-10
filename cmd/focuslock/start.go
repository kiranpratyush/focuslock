//go:build windows || linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/session"
)

// cmdStart starts a session. Without --no-prompt it opens the app's start
// window instead, pre-filled with what was given here.
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
	prompt := true
	var words []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--no-prompt":
			prompt = false
		case (a == "--sites" || a == "--apps") && i+1 < len(args):
			i++
			if a == "--sites" {
				setup.AllowDomains = config.ParseDomainList(args[i])
			} else {
				setup.AllowApps = config.ParseAppList(args[i])
			}
		default:
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

	ui, uiErr := uiExe()
	if prompt {
		// The app's start window asks, pre-filled from last-session.json.
		if uiErr != nil {
			fatal(fmt.Errorf("can't find the FocusLock app (%v); add --no-prompt to start from here", uiErr))
		}
		config.SaveLastSetup(dir, setup)
		if err := openUI(ui); err != nil {
			fatal(err)
		}
		return
	}

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
	// The copies run from the data folder, so tell them where the app is.
	if uiErr == nil {
		os.Setenv("FOCUSLOCK_UI", ui)
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
	if uiErr != nil {
		fmt.Printf("Note: the FocusLock app wasn't found (%v), so there's no bar or block screen; blocking still works.\n", uiErr)
	}
}

func cmdStatusJSON(dir string) {
	out := map[string]any{"active": false}
	if s, ok := session.ReadActive(dir); ok {
		out = map[string]any{"active": true, "endUnix": s.EndUnix, "task": s.Intent}
	}
	printJSON(out)
}

// uiExe finds the Qt app: $FOCUSLOCK_UI, else beside the engine, else on PATH.
func uiExe() (string, error) {
	if p := os.Getenv("FOCUSLOCK_UI"); p != "" {
		return p, nil
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), uiExeName)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return lookPath(uiExeName)
}
