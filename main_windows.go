//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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
		_, _ = loadConfig(dir)
		fmt.Println(filepath.Join(dir, "focus.json"))
	case "judge-check":
		cfg, err := loadConfig(dir)
		if err != nil {
			fatal(fmt.Errorf("focus.json: %w", err))
		}
		stop := superviseJevos(cfg, false)
		judgeCheck(cfg, strings.Join(args[1:], " "), os.Stdout)
		stop()
	case "judge-eval":
		cfg, err := loadConfig(dir)
		if err != nil {
			fatal(fmt.Errorf("focus.json: %w", err))
		}
		stop := superviseJevos(cfg, false)
		err = judgeEvalCommand(cfg, filepath.Join(dir, "focuslock.log"), filepath.Join(dir, "eval-cases.jsonl"), args[1:], os.Stdout)
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
	if s, ok := readActiveState(dir); ok {
		fmt.Printf("A session is already running: %s left.\n", fmtRemaining(s.Remaining()))
		return
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		fatal(fmt.Errorf("focus.json: %w", err))
	}

	// Pre-fill from the last session, then from the command line.
	setup := loadLastSetup(dir)
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
	saveLastSetup(dir, setup)
	intent := strings.TrimSpace(setup.Task)

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

	s := State{EndUnix: time.Now().Add(d).Unix(), BootUnix: bootTime().Unix(), Intent: intent,
		AllowDomains: setup.AllowDomains, AllowApps: setup.AllowApps}
	if err := writeState(dir, s); err != nil {
		fatal(err)
	}
	if _, err := spawn(filepath.Join(dir, guardExeName), "guard", "--peer", "0"); err != nil {
		fatal(err)
	}

	fmt.Printf("Focus session started: %s (until %s).\n", fmtRemaining(d), s.End().Format("15:04"))
	if intent != "" {
		fmt.Printf("Task: %s\n", intent)
	}
	if len(s.AllowDomains)+len(s.AllowApps) > 0 {
		fmt.Printf("Also allowed this session: %s\n", strings.Join(append(append([]string{}, s.AllowDomains...), s.AllowApps...), ", "))
	}
	fmt.Printf("Judge: %s\nLog: %s\n", judgeStatus(cfg), filepath.Join(dir, "focuslock.log"))
	if intent != "" && newJudge(cfg) == nil {
		fmt.Println("Note: without a judge, the task is shown but can't be checked — only the rules apply.")
	}
}

func cmdStatus(dir string) {
	if s, ok := readActiveState(dir); ok {
		fmt.Printf("%s left (ends %s).\n", fmtRemaining(s.Remaining()), s.End().Format("15:04"))
		if s.Intent != "" {
			fmt.Printf("Task: %s\n", s.Intent)
		}
		return
	}
	fmt.Println("No active session.")
}

// runGuard keeps the worker alive. It has no window.
func runGuard(dir string, peer uint32) {
	if !singleInstance(`Local\FocusLockGuard`) {
		return
	}
	logf("guard started (pid %d, peer %d)", os.Getpid(), peer)
	watchPeer(dir, peer, filepath.Join(dir, workerExeName), "worker")
	logf("guard exiting")
}

// runWorker does the actual blocking and keeps the guard alive.
func runWorker(dir string, peer uint32) {
	if !singleInstance(`Local\FocusLockWorker`) {
		return
	}
	s, ok := readActiveState(dir)
	if !ok {
		return
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		logf("config error, using defaults: %v", err)
		cfg = defaultConfig()
		cfg.normalize()
	}
	logf("worker started (pid %d, peer %d), %s left", os.Getpid(), peer, fmtRemaining(s.Remaining()))
	if s.Intent != "" {
		logf("task: %s", s.Intent)
	}
	if len(s.AllowDomains)+len(s.AllowApps) > 0 {
		logf("allowed this session: %s", strings.Join(append(append([]string{}, s.AllowDomains...), s.AllowApps...), ", "))
	}
	cfg.applySession(s)
	go watchPeer(dir, peer, filepath.Join(dir, guardExeName), "guard")
	cls := NewClassifier(cfg, s.Intent)
	cls.Restore(s.Notes, s.ExplainUsed)
	save := func() {
		st := s
		st.Notes, st.ExplainUsed = cls.SessionNotes()
		if err := writeState(dir, st); err != nil {
			logf("save explanations: %v", err)
		}
	}
	stopJevos := superviseJevos(cfg, true)
	runUI(cfg, s, cls, save)
	stopJevos()
	_ = os.Remove(statePath(dir))
	logf("worker exiting: session over")
}

// watchPeer waits for the peer process to die and restarts it,
// until the session ends.
func watchPeer(dir string, peer uint32, exe, role string) {
	for {
		if peer != 0 {
			waitForExit(peer)
		}
		if _, ok := readActiveState(dir); !ok {
			return
		}
		if peer != 0 {
			logf("%s (pid %d) died — restarting it", role, peer)
			time.Sleep(500 * time.Millisecond)
		}
		pid, err := spawn(exe, role, "--peer", strconv.Itoa(os.Getpid()))
		if err != nil {
			logf("restart %s failed: %v", role, err)
			time.Sleep(2 * time.Second)
			continue
		}
		peer = pid
	}
}

func spawn(exe string, args ...string) (uint32, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ() // carries TYPESAFE_API_KEY along
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := uint32(cmd.Process.Pid)
	_ = cmd.Process.Release()
	return pid, nil
}

func peerArg(args []string) uint32 {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--peer" {
			n, _ := strconv.ParseUint(args[i+1], 10, 32)
			return uint32(n)
		}
	}
	return 0
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

func copyFile(src, dst string) error {
	if strings.EqualFold(src, dst) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func setupLog(dir string) {
	f, err := os.OpenFile(filepath.Join(dir, "focuslock.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	l := log.New(f, "", log.LstdFlags)
	logf = func(format string, a ...any) { l.Printf(format, a...) }
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
