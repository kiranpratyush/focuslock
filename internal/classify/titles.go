package classify

import "strings"

// Window titles: browser suffixes ("- Brave"), placeholder titles a page
// shows while loading, and browser pop-ups that aren't pages at all.

// genericTitleWords are title parts that say nothing about the page:
// a site's home, feed and tab names.
var genericTitleWords = map[string]bool{
	"home": true, "live": true, "videos": true, "shorts": true, "explore": true,
	"notifications": true, "search": true, "feed": true, "for you": true,
	"following": true, "popular": true, "trending": true, "subscriptions": true,
	"youtube": true, "x": true, "twitter": true, "reddit": true,
}

// isPlaceholderTitle reports whether a browser title names only the site,
// not the page: "YouTube - Brave", "(3) Home / X - Google Chrome",
// "Hello Interview - Brave" on hellointerview.com, or
// "x.com/someone/status/123 - Brave". Browsers show these while a page
// loads, and on home and feed pages.
func isPlaceholderTitle(title, host string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	// Drop the browser's own suffix: " - Brave", " — Mozilla Firefox"...
	for _, sep := range []string{" - ", " — "} {
		if i := strings.LastIndex(t, sep); i >= 0 && isBrowserName(t[i+len(sep):]) {
			t = strings.TrimSpace(t[:i])
			break
		}
	}
	// Notification count: "(3) YouTube".
	if strings.HasPrefix(t, "(") {
		if i := strings.Index(t, ") "); i > 0 {
			t = strings.TrimSpace(t[i+2:])
		}
	}
	if t == "" || strings.Contains(t, "://") || (host != "" && strings.HasPrefix(t, host)) ||
		strings.HasPrefix(t, "www.") {
		return true
	}
	site := siteName(host)
	parts := strings.FieldsFunc(t, func(r rune) bool { return r == '-' || r == '|' || r == '/' || r == '—' })
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && strings.ReplaceAll(p, " ", "") != site && !genericTitleWords[p] {
			return false
		}
	}
	return true
}

// siteName is the main label of a host: "en.wikipedia.org" -> "wikipedia",
// "bbc.co.uk" -> "bbc".
func siteName(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}
	i := len(labels) - 2
	switch labels[i] {
	case "co", "com", "org", "net", "ac", "gov", "edu":
		if i > 0 {
			i--
		}
	}
	return labels[i]
}

// hasBrowserSuffix reports whether a title ends like a browser's main
// window: "Page - Brave", "Page — Mozilla Firefox".
func hasBrowserSuffix(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, sep := range []string{" - ", " — "} {
		if i := strings.LastIndex(t, sep); i >= 0 && isBrowserName(t[i+len(sep):]) {
			return true
		}
	}
	return isBrowserName(t) // a bare "Brave" / "Google Chrome" window
}

// isBrowserPopup: a browser window that isn't a page, like the
// `"Drive" added` bubble or a permission prompt. Tabs end in " - Brave";
// installed web apps (YouTube as an app) have no address bar or suffix but
// still show "Video - YouTube", so a title with a separator is never skipped.
func isBrowserPopup(title string) bool {
	if hasBrowserSuffix(title) {
		return false
	}
	for _, sep := range []string{" - ", " | ", " — ", " / ", " · "} {
		if strings.Contains(title, sep) {
			return false
		}
	}
	return true
}

func isBrowserName(s string) bool {
	s = strings.ReplaceAll(s, "\u200b", "") // Edge: "Microsoft\u200b Edge"
	for _, b := range []string{"brave", "google chrome", "chrome", "microsoft edge", "edge", "mozilla firefox", "firefox", "opera", "vivaldi"} {
		if strings.HasSuffix(s, b) {
			return true
		}
	}
	return false
}

// TrimBrowserSuffix: "Page - Brave" -> "Page".
func TrimBrowserSuffix(title string) string {
	t := strings.TrimSpace(title)
	lower := strings.ToLower(t)
	for _, sep := range []string{" - ", " — "} {
		if i := strings.LastIndex(lower, sep); i >= 0 && isBrowserName(lower[i+len(sep):]) {
			return strings.TrimSpace(t[:i])
		}
	}
	return t
}
