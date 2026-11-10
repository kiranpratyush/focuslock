//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"focuslock/internal/classify"
	"focuslock/internal/config"
	"focuslock/internal/logx"
	"focuslock/internal/session"
)

// The guard and the worker, as on Windows: two copies of the engine under
// different names. The worker does the blocking; each restarts the other.

func runGuard(dir string, peer uint32) {
	if !singleInstance(dir, "guard") {
		return
	}
	logx.Printf("guard started (pid %d, peer %d)", os.Getpid(), peer)
	watchPeer(dir, peer, filepath.Join(dir, workerExeName), "worker")
	logx.Printf("guard exiting")
}

func runWorker(dir string, peer uint32) {
	if !singleInstance(dir, "worker") {
		return
	}
	s, ok := session.ReadActive(dir)
	if !ok {
		return
	}
	cfg, err := config.Load(dir)
	if err != nil {
		logx.Printf("config error, using defaults: %v", err)
		cfg = config.Default()
		cfg.Normalize()
	}
	logx.Printf("worker started (pid %d, peer %d), %s left", os.Getpid(), peer, session.FmtRemaining(s.Remaining()))
	cfg.ApplySession(s.AllowDomains, s.AllowApps)
	linuxNames(&cfg)
	go watchPeer(dir, peer, filepath.Join(dir, guardExeName), "guard")
	cls := classify.New(cfg, s.Intent)
	cls.SetTaskDetail(s.TaskDetail)
	cls.Restore(s.Notes, s.ExplainUsed)
	runUI(dir, cfg, s, cls)
	_ = os.Remove(session.Path(dir))
	logx.Printf("worker exiting: session over")
}

// linuxNames makes focus.json's Windows process names ("steam.exe") match
// Linux ones ("steam") by adding each name without ".exe", and adds the
// browsers Linux users run.
func linuxNames(c *config.Config) {
	twin := func(xs []string) []string {
		out := append([]string(nil), xs...)
		for _, x := range xs {
			if n, ok := strings.CutSuffix(x, ".exe"); ok {
				out = append(out, n)
			}
		}
		return out
	}
	c.Browsers = append(twin(c.Browsers), "chromium", "zen", "zen-bin", "librewolf", "firefox-bin", "vivaldi-bin", "google-chrome")
	c.BlockedProcesses = twin(c.BlockedProcesses)
	c.MusicProcesses = twin(c.MusicProcesses)
	c.AlwaysAllowProcesses = twin(c.AlwaysAllowProcesses)
	for i := range c.Rations {
		c.Rations[i].Match = twin(c.Rations[i].Match)
	}
}

func watchPeer(dir string, peer uint32, exe, role string) {
	for {
		if peer != 0 {
			waitForExit(peer)
		}
		if _, ok := session.ReadActive(dir); !ok {
			return
		}
		if peer != 0 {
			logx.Printf("%s (pid %d) died — restarting it", role, peer)
			time.Sleep(500 * time.Millisecond)
		}
		pid, err := spawn(exe, role, "--peer", strconv.Itoa(os.Getpid()))
		if err != nil {
			logx.Printf("restart %s failed: %v", role, err)
			time.Sleep(2 * time.Second)
			continue
		}
		peer = pid
	}
}

// spawn starts a detached process in its own session. It is reaped in the
// background so waitForExit sees it go instead of a lingering zombie.
func spawn(exe string, args ...string) (uint32, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() { _ = cmd.Wait() }()
	return uint32(cmd.Process.Pid), nil
}

func waitForExit(pid uint32) {
	for syscall.Kill(int(pid), 0) == nil {
		time.Sleep(500 * time.Millisecond)
	}
}

var locks []*os.File

// singleInstance takes an exclusive lock on <dir>/<role>.lock for the life
// of the process; false means another one holds it.
func singleInstance(dir, role string) bool {
	f, err := os.OpenFile(filepath.Join(dir, role+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return false
	}
	locks = append(locks, f)
	return true
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

// copyExe writes beside the target and renames, so a running copy of the
// old file keeps working ("text file busy" otherwise).
func copyExe(src, dst string) error {
	if src == dst {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// openUI replaces this process with the app's start window.
func openUI(path string) error {
	return syscall.Exec(path, []string{path, "open"}, os.Environ())
}

func lookPath(name string) (string, error) { return exec.LookPath(name) }

func printJSON(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// writeAtomic writes beside the target and renames, so a reader never sees
// half a file.
func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
