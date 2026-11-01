// Package textutil has the two tiny string helpers every other package uses.
package textutil

// Shorten cuts s to n characters (runes), ending with "…" if it was longer.
func Shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Contains reports whether xs has s. (slices.Contains does the same; this
// predates it here and reads the same at call sites.)
func Contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
