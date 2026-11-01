//go:build windows

package session

import (
	"syscall"
	"time"
)

var getTickCount64 = syscall.NewLazyDLL("kernel32.dll").NewProc("GetTickCount64")

// BootTime is when Windows last started: now minus the milliseconds since boot.
// A session is tied to the boot it started in, so restarting ends it.
func BootTime() time.Time {
	ms, _, _ := getTickCount64.Call()
	return time.Now().Add(-time.Duration(ms) * time.Millisecond)
}
