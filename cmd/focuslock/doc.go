// Command focuslock is a Windows focus timer you can't casually stop.
//
// This package is the Windows app: everything that talks to Win32. The
// decisions themselves live in the portable packages under internal/,
// which build and test anywhere.
//
// How a session runs:
//
//	focuslock start      main_windows.go: the start window (setup_windows.go),
//	                     then it copies itself as a guard and a worker
//	guard + worker       worker_windows.go: each restarts the other if killed
//	worker               ui_windows.go: every 500 ms, tick() looks at the
//	                     window in front, asks classify.Classifier about it,
//	                     and covers it (blockscreen_windows.go), closes the
//	                     tab, or kills the app
//	other monitors       monitors_windows.go: the front window on each other
//	                     monitor is checked the same way
//	the address bar      uia_windows.go: reads a tab's real URL with
//	                     UI Automation, the accessibility API
//	jevos                jevos_windows.go: starts jev.exe when the judge is jevos
//	between sessions     cue_windows.go: a breathing line and a "plan your next
//	                     session" pill; startup_windows.go starts it at sign-in
//	Win32 plumbing       winapi_windows.go: raw syscalls, no dependencies
package main
