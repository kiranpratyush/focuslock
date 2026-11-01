package main

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Category string

const (
	CatAllow         Category = "allow"
	CatMusic         Category = "music"
	CatEntertainment Category = "entertainment"
	CatOffTask       Category = "off_task" // not fun, just not this task
	CatPending       Category = "pending"  // waiting for the judge; treated as allow
)

type Verdict struct {
	Cat    Category
	Source string // "rule", "url", "placeholder", "jev", "llm", "fallback"
	Prob   float64
	// Hold (only with CatPending): this is a strict site still being judged,
	// so an overlay already covering it should stay up instead of flickering off.
	Hold bool
}

// Classifier decides what a foreground window is.
// Rules answer instantly; the judge (Jev or an LLM) answers asynchronously
// and is cached.
type Classifier struct {
	cfg    Config
	intent string
	judge  Judge // nil = rules only

	mu       sync.Mutex
	cache    map[string]Verdict
	inflight map[string]bool
	failedAt map[string]time.Time

	// The title must stay the same for cfg.Judge.SettleMs before it's
	// judged. Kept per window key: windows on other monitors are classified
	// every tick too, and must not reset each other's wait.
	seen map[string]seenAt
	now  func() time.Time // for tests

	// Accepted explanations (explain.go) and how many were tried.
	notes       []SessionNote
	explainUsed int
}

func NewClassifier(cfg Config, intent string) *Classifier {
	return &Classifier{
		cfg:      cfg,
		intent:   strings.TrimSpace(intent),
		judge:    newJudge(cfg),
		cache:    map[string]Verdict{},
		inflight: map[string]bool{},
		failedAt: map[string]time.Time{},
		seen:     map[string]seenAt{},
		now:      time.Now,
	}
}

// JudgeName is "" when running on rules only.
func (c *Classifier) JudgeName() string {
	if c.judge == nil {
		return ""
	}
	return c.judge.Name()
}

func (c *Classifier) Intent() string { return c.intent }

// Classify never blocks on the network. proc is the lowercase exe name;
// rawURL is the address bar text ("" when unknown).
func (c *Classifier) Classify(proc, title, rawURL string) Verdict {
	host, path := normalizeURL(rawURL)
	isBrowser := contains(c.cfg.Browsers, proc)
	if v, ok := c.byURL(proc, rawURL, host, path); ok {
		return v
	}
	if v, ok := c.byRules(proc, title); ok {
		return v
	}
	if isBrowser && strings.TrimSpace(rawURL) == "" && isBrowserPopup(title) {
		return Verdict{Cat: CatAllow, Source: "rule", Prob: 1}
	}
	if c.judge == nil {
		return Verdict{Cat: CatAllow, Source: "fallback"}
	}
	strictSite := c.isStrict(proc, host, path)
	placeholder := isBrowser && host != "" && isPlaceholderTitle(title, host)
	// strict: the judge must be sure it's work before it's allowed.
	//   - strict sites (YouTube, X...)
	//   - with a task, a page whose title is just the site's name ("Hello
	//     Interview"): there's nothing to go on, so a guess isn't enough
	//   - with a task, apps (cfg.Judge.StrictApps)
	strict := strictSite ||
		(placeholder && c.intent != "") ||
		(!isBrowser && c.cfg.Judge.StrictApps)
	q := c.question(proc, title, host, path)
	key := proc + "|" + title + "|" + q.Page
	if strict {
		key += "|strict"
	}
	pending := Verdict{Cat: CatPending, Source: c.judge.Name(), Hold: strictSite}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.explained(proc, title, host, path) {
		return Verdict{Cat: CatAllow, Source: "explained", Prob: 1}
	}
	q.Notes = c.noteTexts()
	if v, ok := c.cache[key]; ok {
		return v
	}
	if t, ok := c.failedAt[key]; ok && time.Since(t) < 30*time.Second {
		return Verdict{Cat: CatAllow, Source: "fallback"}
	}
	if c.inflight[key] {
		return pending
	}
	if !c.settled(key) {
		// Also covers placeholders: a video's "youtube.com/watch?v=..." title
		// is gone before it settles, so a good video never flashes blocked.
		return pending
	}
	if strictSite && placeholder {
		// "YouTube - Brave" that stayed: a home or feed page. On these
		// sites that's not work, and there's nothing for the judge to read.
		return Verdict{Cat: CatEntertainment, Source: "placeholder", Prob: 1}
	}
	c.inflight[key] = true
	go c.ask(key, q, strict)
	return pending
}

type seenAt struct{ first, last time.Time }

// settled reports whether key has been in front long enough to judge.
// A key not seen for 2 seconds starts over: the title changed and came back.
// Caller holds c.mu.
func (c *Classifier) settled(key string) bool {
	now := c.now()
	e, ok := c.seen[key]
	if !ok || now.Sub(e.last) > 2*time.Second {
		e.first = now
	}
	e.last = now
	c.seen[key] = e
	if len(c.seen) > 500 {
		for k, v := range c.seen {
			if now.Sub(v.last) > time.Minute {
				delete(c.seen, k)
			}
		}
	}
	return now.Sub(e.first) >= time.Duration(c.cfg.Judge.SettleMs)*time.Millisecond
}

// question is what the judge gets for this window (without notes).
func (c *Classifier) question(proc, title, host, path string) Question {
	page := ""
	if c.cfg.Judge.SendURL && host != "" {
		page = host + path
	}
	return Question{Intent: c.intent, App: proc, Title: title, Page: page}
}

// isStrict reports whether a browser tab is on one of cfg.StrictDomains.
func (c *Classifier) isStrict(proc, host, path string) bool {
	if host == "" || !contains(c.cfg.Browsers, proc) {
		return false
	}
	for _, p := range c.cfg.StrictDomains {
		if matchDomain(p, host, path) {
			return true
		}
	}
	return false
}

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

// byURL applies domain rules to browser tabs whose URL we could read.
func (c *Classifier) byURL(proc, rawURL, host, path string) (Verdict, bool) {
	if !contains(c.cfg.Browsers, proc) || strings.TrimSpace(rawURL) == "" {
		return Verdict{}, false
	}
	if host == "" {
		// chrome://, edge://, about:, file:// — browser's own pages.
		return Verdict{Cat: CatAllow, Source: "url", Prob: 1}, true
	}
	lists := []struct {
		patterns []string
		cat      Category
	}{
		{c.cfg.AllowDomains, CatAllow},
		{c.cfg.MusicDomains, CatMusic},
		{c.cfg.BlockDomains, CatEntertainment},
	}
	for _, l := range lists {
		for _, p := range l.patterns {
			if matchDomain(p, host, path) {
				return Verdict{Cat: l.cat, Source: "url", Prob: 1}, true
			}
		}
	}
	return Verdict{}, false
}

// normalizeURL turns address bar text like "https://www.YouTube.com/shorts/x?y=1"
// or "youtube.com/watch?v=1" into ("youtube.com", "/shorts/x").
// Non-web schemes (chrome://, edge://, about:, file:) give empty host.
func normalizeURL(raw string) (host, path string) {
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

// matchDomain: "youtube.com" matches youtube.com and *.youtube.com;
// "youtube.com/shorts" additionally requires the path to start with /shorts.
func matchDomain(pattern, host, path string) bool {
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

func (c *Classifier) byRules(proc, title string) (Verdict, bool) {
	t := strings.ToLower(title)
	isBrowser := contains(c.cfg.Browsers, proc)

	if contains(c.cfg.BlockedProcesses, proc) {
		return Verdict{Cat: CatEntertainment, Source: "rule", Prob: 1}, true
	}
	if contains(c.cfg.MusicProcesses, proc) {
		return Verdict{Cat: CatMusic, Source: "rule", Prob: 1}, true
	}
	if !isBrowser {
		// Apps are judged only when a task is set and a judge is available;
		// your everyday tools (alwaysAllowProcesses) are never judged.
		if c.intent == "" || c.judge == nil || contains(c.cfg.AlwaysAllowProcesses, proc) || strings.TrimSpace(t) == "" {
			return Verdict{Cat: CatAllow, Source: "rule", Prob: 1}, true
		}
		return Verdict{}, false
	}
	for _, k := range c.cfg.AllowTitleKeywords {
		if strings.Contains(t, k) {
			return Verdict{Cat: CatAllow, Source: "rule", Prob: 1}, true
		}
	}
	for _, k := range c.cfg.MusicTitleKeywords {
		if strings.Contains(t, k) {
			return Verdict{Cat: CatMusic, Source: "rule", Prob: 1}, true
		}
	}
	for _, k := range c.cfg.BlockTitleKeywords {
		if strings.Contains(t, k) {
			return Verdict{Cat: CatEntertainment, Source: "rule", Prob: 1}, true
		}
	}
	// Blank/new tab pages: nothing to judge.
	if strings.TrimSpace(t) == "" || strings.HasPrefix(t, "new tab") {
		return Verdict{Cat: CatAllow, Source: "rule", Prob: 1}, true
	}
	return Verdict{}, false
}

func (c *Classifier) ask(key string, q Question, strict bool) {
	ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout(c.judge, c.cfg))
	defer cancel()
	a, err := c.judge.Ask(ctx, q)
	var v Verdict
	if err == nil {
		v = decide(a, c.cfg.Judge.Threshold, c.cfg.Judge.MusicThreshold, strict)
		v.Source = c.judge.Name()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, key)
	if err != nil {
		logf("%s error: %v", c.judge.Name(), err)
		c.failedAt[key] = time.Now()
		return
	}
	if len(c.cache) > 2000 {
		c.cache = map[string]Verdict{}
	}
	c.cache[key] = v
	logf("%s: %s %q %s -> %s [%s %.2f]", c.judge.Name(), q.App, q.Title, q.Page, v.Cat, a.Choice, v.Prob)
}

// decide turns a judge's answer into a verdict.
//   - "music" counts as music only when the judge is very sure (musicThreshold),
//     because a music video you're watching looks like music from its title.
//   - If entertainment + social + music together reach threshold: entertainment.
//     (A song-or-video toss-up gets blocked.)
//   - With a task: if those plus off_task reach threshold, it's off-task.
//   - Anything else (on task, supporting, work/learning, or unsure) is allowed,
//     except on strict sites: there it's allowed only if work + learning (or
//     on_task + supporting) reach threshold. Unsure on YouTube means blocked.
func decide(a Answer, threshold, musicThreshold float64, strict bool) Verdict {
	if musicThreshold < threshold {
		musicThreshold = threshold
	}
	pr := a.Probabilities
	p := pr[a.Choice]
	if p == 0 {
		p = a.Confidence
	}
	fun := pr["entertainment"] + pr["social"] + pr["music"]
	off := pr["off_task"]
	if len(pr) == 0 {
		switch a.Choice {
		case "entertainment", "social", "music":
			fun = p
		case "off_task":
			off = p
		}
	}
	switch {
	case a.Choice == "music" && p >= musicThreshold:
		return Verdict{Cat: CatMusic, Prob: p}
	case fun >= threshold:
		return Verdict{Cat: CatEntertainment, Prob: fun}
	case fun+off >= threshold:
		return Verdict{Cat: CatOffTask, Prob: fun + off}
	}
	if strict {
		ok := pr["work"] + pr["learning"] + pr["on_task"] + pr["supporting"]
		if len(pr) == 0 {
			switch a.Choice {
			case "work", "learning", "on_task", "supporting":
				ok = p
			}
		}
		if ok < threshold {
			if off > fun {
				return Verdict{Cat: CatOffTask, Prob: 1 - ok}
			}
			return Verdict{Cat: CatEntertainment, Prob: 1 - ok}
		}
		return Verdict{Cat: CatAllow, Prob: ok}
	}
	return Verdict{Cat: CatAllow, Prob: p}
}

var logf = func(format string, args ...any) {}
