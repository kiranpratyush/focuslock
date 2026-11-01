//go:build !windows

package session

import "time"

// BootTime off Windows: only so the portable packages build and test elsewhere.
func BootTime() time.Time { return time.Unix(0, 0) }
