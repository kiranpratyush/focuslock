//go:build linux

package session

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// BootTime is when Linux last started, from the btime line of /proc/stat.
// A session is tied to the boot it started in, so restarting ends it.
func BootTime() time.Time {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Unix(0, 0)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "btime "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			return time.Unix(n, 0)
		}
	}
	return time.Unix(0, 0)
}
