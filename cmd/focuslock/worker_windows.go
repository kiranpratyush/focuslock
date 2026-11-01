//go:build windows

package main

import (
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

// The guard and the worker: two copies of the exe under different names.
// The worker does the blocking; each one restarts the other if it dies.

// runGuard keeps the worker alive. It has no window.
func runGuard(dir string, peer uint32) {
	if !singleInstance(`Local\FocusLockGuard`) {
		return
	}
	logx.Printf("guard started (pid %d, peer %d)", os.Getpid(), peer)
	watchPeer(dir, peer, filepath.Join(dir, workerExeName), "worker")
	logx.Printf("guard exiting")
}

// runWorker does the actual blocking and keeps the guard alive.
func runWorker(dir string, peer uint32) {
	if !singleInstance(`Local\FocusLockWorker`) {
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
	if s.Intent != "" {
		logx.Printf("task: %s", s.Intent)
	}
	if s.TaskDetail != "" {
		logx.Printf("task as the judge sees it: %s", strings.ReplaceAll(s.TaskDetail, "\n", " | "))
	}
	if len(s.AllowDomains)+len(s.AllowApps) > 0 {
		logx.Printf("allowed this session: %s", strings.Join(append(append([]string{}, s.AllowDomains...), s.AllowApps...), ", "))
	}
	cfg.ApplySession(s.AllowDomains, s.AllowApps)
	go watchPeer(dir, peer, filepath.Join(dir, guardExeName), "guard")
	cls := classify.New(cfg, s.Intent)
	cls.SetTaskDetail(s.TaskDetail)
	cls.Restore(s.Notes, s.ExplainUsed)
	save := func() {
		st := s
		st.Notes, st.ExplainUsed = cls.SessionNotes()
		if err := session.Write(dir, st); err != nil {
			logx.Printf("save explanations: %v", err)
		}
	}
	stopJevos := superviseJevos(cfg, true)
	runUI(cfg, s, cls, save)
	stopJevos()
	_ = os.Remove(session.Path(dir))
	logx.Printf("worker exiting: session over")
}

// watchPeer waits for the peer process to die and restarts it,
// until the session ends.
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
