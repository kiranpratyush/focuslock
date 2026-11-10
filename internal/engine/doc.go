// Package engine is FocusLock's monitor loop, the same on every OS.
//
// Twice a second the platform layer reports the window in front (and the
// ones in front on other monitors); Tick decides what to block, kills and
// closes through the platform, and returns a Frame: the bar text and the
// block screens a frontend should draw. Nothing here draws or calls an OS
// API, so the rules are tested with a fake platform.
package engine
