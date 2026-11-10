//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Hyprland and /proc: what Win32 and UI Automation do on Windows.

type hyprWindow struct {
	Address    string `json:"address"`
	At         [2]int `json:"at"`
	Size       [2]int `json:"size"`
	Class      string `json:"class"`
	Title      string `json:"title"`
	Pid        int    `json:"pid"`
	Monitor    int    `json:"monitor"`
	Fullscreen int    `json:"fullscreen"`
}

type hyprMonitor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

func hyprctlJSON(v any, args ...string) error {
	out, err := exec.Command("hyprctl", append([]string{"-j"}, args...)...).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

// activeWindow is the focused window; ok is false when nothing is focused.
func activeWindow() (hyprWindow, bool) {
	var w hyprWindow
	if hyprctlJSON(&w, "activewindow") != nil || w.Address == "" {
		return w, false
	}
	return w, true
}

func monitorByID(id int) (hyprMonitor, bool) {
	var ms []hyprMonitor
	if hyprctlJSON(&ms, "monitors") != nil {
		return hyprMonitor{}, false
	}
	for _, m := range ms {
		if m.ID == id {
			return m, true
		}
	}
	return hyprMonitor{}, false
}

// closeTab sends Ctrl+W to the browser window, wherever focus moves next.
func closeTab(address string) {
	_ = exec.Command("hyprctl", "dispatch", "sendshortcut", "CTRL, W, address:"+address).Run()
}

// processName is the lowercased command name of pid ("firefox", "steam").
func processName(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(b)))
}

// killByName ends this user's processes whose command name is in names
// (".exe" ignored), and returns the names it ended.
func killByName(names []string) []string {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSuffix(strings.ToLower(n), ".exe")] = true
	}
	entries, _ := filepath.Glob("/proc/[0-9]*")
	uid := os.Getuid()
	var killed []string
	seen := map[string]bool{}
	for _, e := range entries {
		pid, err := strconv.Atoi(filepath.Base(e))
		if err != nil || pid == os.Getpid() {
			continue
		}
		var st syscall.Stat_t
		if syscall.Stat(e, &st) != nil || int(st.Uid) != uid {
			continue
		}
		name := processName(pid)
		if name == "" || !want[name] {
			continue
		}
		if syscall.Kill(pid, syscall.SIGTERM) == nil && !seen[name] {
			seen[name] = true
			killed = append(killed, name)
			go func(p int) {
				// Anything still there after 3 seconds is told less politely.
				time.Sleep(3 * time.Second)
				_ = syscall.Kill(p, syscall.SIGKILL)
			}(pid)
		}
	}
	return killed
}
