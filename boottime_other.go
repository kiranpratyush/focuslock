//go:build !windows

package main

import "time"

// Only used so the portable parts (classifier, state) build and test off Windows.
func bootTime() time.Time { return time.Unix(0, 0) }
