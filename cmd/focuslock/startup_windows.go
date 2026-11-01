//go:build windows

package main

// "focuslock startup on|off|status": start the cue when you sign in.
//
// It's one value under HKEY_CURRENT_USER\...\Run, the same place apps use
// for "start with Windows". It's per-user, so no admin rights are needed.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"focuslock/internal/config"
	"focuslock/internal/cue"
)

const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "FocusLockCue"
)

func cmdStartup(dir string, args []string) {
	arg := "status"
	if len(args) > 0 {
		arg = args[0]
	}
	switch arg {
	case "on":
		exe, err := os.Executable()
		if err != nil {
			fatal(err)
		}
		line := startupCommand(exe)
		if err := regSetRun(line); err != nil {
			fatal(fmt.Errorf("couldn't add FocusLock to startup: %w", err))
		}
		fmt.Println("FocusLock will remind you to plan a session each time you sign in.")
		fmt.Println("Starts with:", line)
		if cueRunning() {
			fmt.Println("The cue is already running.")
		} else if _, err := spawn(exe, "cue", "--hello"); err == nil {
			fmt.Println("Started it now as well: look for the pill at the top of your screen.")
		}
		if cfg, err := config.Load(dir); err == nil && !cue.InWorkHours(time.Now(), cfg.Cue) {
			fmt.Printf("Note: it's outside your work hours (%s), so it stays hidden until they start.\n", cfg.Cue.WorkHours)
		}
		fmt.Println(`If you move focuslock.exe, run "focuslock startup on" again.`)
	case "off":
		if err := regDeleteRun(); err != nil {
			fatal(fmt.Errorf("couldn't remove FocusLock from startup: %w", err))
		}
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16(cueClass))), 0); h != 0 {
			pPostMessageW.Call(h, WM_CLOSE, 0, 0) // the cue closes itself
		}
		fmt.Println("FocusLock won't start when you sign in, and the cue is closed.")
	case "status":
		if line, ok := regGetRun(); ok {
			fmt.Println("On: starts at sign-in with", line)
		} else {
			fmt.Println(`Off. Turn it on with "focuslock startup on".`)
		}
		if cueRunning() {
			fmt.Println("The cue is running.")
		}
	default:
		fatal(fmt.Errorf("use: focuslock startup on | off | status"))
	}
}

// startupCommand runs the cue without a console window flashing up at
// sign-in. focuslock.exe is a console program, so Windows would open a
// window for it; conhost's --headless mode doesn't.
func startupCommand(exe string) string {
	conhost := filepath.Join(os.Getenv("SystemRoot"), "System32", "conhost.exe")
	if _, err := os.Stat(conhost); err == nil {
		return fmt.Sprintf(`"%s" --headless "%s" cue`, conhost, exe)
	}
	return fmt.Sprintf(`"%s" cue`, exe)
}

func cueRunning() bool {
	h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16(cueClass))), 0)
	return h != 0
}

// ---------- the registry, through advapi32 ----------

var (
	advapi32          = syscall.NewLazyDLL("advapi32.dll")
	pRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keySetValue     = 0x0002
	keyQueryValue   = 0x0001
	regSZ           = 1
	errFileNotFound = 2
)

func openRunKey() (uintptr, error) {
	var key uintptr
	r, _, _ := pRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(runKey))), 0, 0, 0,
		keySetValue|keyQueryValue, 0, uintptr(unsafe.Pointer(&key)), 0)
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return key, nil
}

func regSetRun(value string) error {
	key, err := openRunKey()
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(key)
	data, _ := syscall.UTF16FromString(value)
	r, _, _ := pRegSetValueExW.Call(key, uintptr(unsafe.Pointer(u16(runValue))), 0, regSZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

func regDeleteRun() error {
	key, err := openRunKey()
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(key)
	r, _, _ := pRegDeleteValueW.Call(key, uintptr(unsafe.Pointer(u16(runValue))))
	if r != 0 && r != errFileNotFound { // already off is fine
		return syscall.Errno(r)
	}
	return nil
}

func regGetRun() (string, bool) {
	key, err := openRunKey()
	if err != nil {
		return "", false
	}
	defer pRegCloseKey.Call(key)
	buf := make([]uint16, 1024)
	size := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ := pRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(u16(runValue))), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 || typ != regSZ {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}
