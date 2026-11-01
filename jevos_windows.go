//go:build windows

package main

import (
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const CREATE_NO_WINDOW = 0x08000000

// superviseJevos runs jev.exe for as long as FocusLock needs it, when the
// judge is "jevos" and "jevos": {"exe": ...} is set. If it stops (crashed,
// or killed to get around the judge) it's started again; while it's down,
// strict sites stay covered. stop() ends it.
//
// session: the worker owns jev.exe for the session, so stop() also ends one
// a killed worker left behind. The check commands only stop what they started.
func superviseJevos(cfg Config, session bool) (stop func()) {
	if cfg.Judge.Provider != "jevos" || strings.TrimSpace(cfg.Jevos.Exe) == "" {
		return func() {}
	}
	exe := cfg.Jevos.Exe
	host, port := "127.0.0.1", "8017"
	if u, err := url.Parse(cfg.Jevos.Endpoint); err == nil {
		if u.Hostname() != "" {
			host = u.Hostname()
		}
		if u.Port() != "" {
			port = u.Port()
		}
	}
	threads := cfg.Jevos.Threads
	if threads <= 0 {
		threads = max(2, runtime.NumCPU()/2) // leave the rest for your work
	}

	var (
		mu      sync.Mutex
		cmd     *exec.Cmd
		running bool
	)
	done := make(chan struct{})
	start := func() error {
		c := exec.Command(exe, "serve", "--threads", strconv.Itoa(threads), "--host", host, "--port", port)
		c.Dir = filepath.Dir(exe) // jev finds model/ beside itself
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
		if err := c.Start(); err != nil {
			return err
		}
		cmd, running = c, true
		go func() {
			err := c.Wait()
			mu.Lock()
			if cmd == c {
				running = false
			}
			mu.Unlock()
			select {
			case <-done:
			default:
				logf("jevos stopped (%v); starting it again", err)
			}
		}()
		logf("jevos: started %s (pid %d, %d threads, port %s)", exe, c.Process.Pid, threads, port)
		return nil
	}

	go func() {
		wait := 2 * time.Second
		for {
			mu.Lock()
			need := !running && !jevosHealthy(cfg.Jevos.Endpoint)
			if need {
				if err := start(); err != nil {
					// Smart App Control or a wrong path: say so, try again later.
					logf("jevos: can't start %s: %v (blocked by Windows? wrong \"exe\" path?)", exe, err)
					wait = 30 * time.Second
				} else {
					wait = 2 * time.Second
				}
			}
			mu.Unlock()
			select {
			case <-done:
				return
			case <-time.After(wait):
			}
		}
	}()

	return func() {
		close(done)
		mu.Lock()
		defer mu.Unlock()
		if cmd != nil && running {
			_ = cmd.Process.Kill()
		}
		if session {
			// Also one left behind by a worker that was killed and restarted.
			killByName([]string{strings.ToLower(filepath.Base(exe))})
		}
	}
}
