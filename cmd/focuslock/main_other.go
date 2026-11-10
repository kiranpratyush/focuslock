//go:build !windows && !linux

package main

import (
	"fmt"
	"os"
)

// FocusLock only runs on Windows. This stub lets `go build ./...` and
// `go vet ./...` work elsewhere, where the portable packages are developed
// and tested.
func main() {
	fmt.Fprintln(os.Stderr, "FocusLock runs on Windows and Linux (Hyprland).")
	os.Exit(1)
}
