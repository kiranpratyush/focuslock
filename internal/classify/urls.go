package classify

import (
	"net/url"
	"strings"
)

// Web addresses: turning what the address bar shows into host + path, and
// matching it against the domain lists in focus.json.

// NormalizeURL turns address bar text like "https://www.YouTube.com/shorts/x?y=1"
// or "youtube.com/watch?v=1" into ("youtube.com", "/shorts/x").
// Non-web schemes (chrome://, edge://, about:, file:) give empty host.
func NormalizeURL(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t") {
		return "", "" // empty, or a search query being shown
	}
	lower := strings.ToLower(raw)
	if i := strings.Index(lower, "://"); i >= 0 {
		scheme := lower[:i]
		if scheme != "http" && scheme != "https" {
			return "", ""
		}
	} else if strings.HasPrefix(lower, "about:") || strings.HasPrefix(lower, "file:") {
		return "", ""
	} else {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", ""
	}
	host = strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	return host, u.EscapedPath()
}

// MatchDomain: "youtube.com" matches youtube.com and *.youtube.com;
// "youtube.com/shorts" additionally requires the path to start with /shorts.
func MatchDomain(pattern, host, path string) bool {
	pHost, pPath := pattern, ""
	if i := strings.Index(pattern, "/"); i >= 0 {
		pHost, pPath = pattern[:i], pattern[i:]
	}
	pHost = strings.TrimPrefix(pHost, "www.")
	if host != pHost && !strings.HasSuffix(host, "."+pHost) {
		return false
	}
	return pPath == "" || strings.HasPrefix(strings.ToLower(path), pPath)
}
