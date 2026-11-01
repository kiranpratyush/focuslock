//go:build !windows

package main

import (
	"fmt"
	"os"
)

// FocusLock only runs on Windows. This stub lets `go build ./...` and
// `go vet ./...` work elsewhere, where the portable packages are developed
// and tested.
func main() {
	fmt.Fprintln(os.Stderr, "FocusLock runs on Windows. Build it with: GOOS=windows go build ./cmd/focuslock")
	os.Exit(1)
}
