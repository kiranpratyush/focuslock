// Package logx is the app's one log function. It does nothing until the
// program sets it (cmd/focuslock points it at focuslock.log), so the
// portable packages can log without knowing where logs go, and tests stay quiet.
package logx

// Printf writes one line to the log. Replace it to send logs somewhere.
var Printf = func(format string, args ...any) {}
