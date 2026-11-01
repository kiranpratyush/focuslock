package classify

import (
	"strings"

	"focuslock/internal/judge"
	"focuslock/internal/textutil"
)

// The rules that decide a window without asking the judge: domain lists,
// process lists, title keywords, strict sites.

// isStrict reports whether a browser tab is on one of cfg.StrictDomains.
func (c *Classifier) isStrict(proc, host, path string) bool {
	if host == "" || !textutil.Contains(c.cfg.Browsers, proc) {
		return false
	}
	for _, p := range c.cfg.StrictDomains {
		if MatchDomain(p, host, path) {
			return true
		}
	}
	return false
}

// byURL applies domain rules to browser tabs whose URL we could read.
func (c *Classifier) byURL(proc, rawURL, host, path string) (judge.Verdict, bool) {
	if !textutil.Contains(c.cfg.Browsers, proc) || strings.TrimSpace(rawURL) == "" {
		return judge.Verdict{}, false
	}
	if host == "" {
		// chrome://, edge://, about:, file:// — browser's own pages.
		return judge.Verdict{Cat: judge.CatAllow, Source: "url", Prob: 1}, true
	}
	lists := []struct {
		patterns []string
		cat      judge.Category
	}{
		{c.cfg.AllowDomains, judge.CatAllow},
		{c.cfg.SearchDomains, judge.CatAllow},
		{c.cfg.MusicDomains, judge.CatMusic},
		{c.cfg.BlockDomains, judge.CatEntertainment},
	}
	for _, l := range lists {
		for _, p := range l.patterns {
			if MatchDomain(p, host, path) {
				return judge.Verdict{Cat: l.cat, Source: "url", Prob: 1}, true
			}
		}
	}
	return judge.Verdict{}, false
}

// urlKnown: a browser tab whose web address was read. Its allowed sites are
// decided by allowDomains, so allowTitleKeywords don't apply: "docs" or
// "claude" in a title would otherwise wave through any page, like a YouTube
// video called "Claude vs ChatGPT — funniest moments".
func (c *Classifier) byRules(proc, title string, urlKnown bool) (judge.Verdict, bool) {
	t := strings.ToLower(title)
	isBrowser := textutil.Contains(c.cfg.Browsers, proc)

	if textutil.Contains(c.cfg.BlockedProcesses, proc) {
		return judge.Verdict{Cat: judge.CatEntertainment, Source: "rule", Prob: 1}, true
	}
	if textutil.Contains(c.cfg.MusicProcesses, proc) {
		return judge.Verdict{Cat: judge.CatMusic, Source: "rule", Prob: 1}, true
	}
	if !isBrowser {
		// Apps are judged only when a task is set and a judge is available;
		// your everyday tools (alwaysAllowProcesses) are never judged.
		if c.intent == "" || c.judge == nil || textutil.Contains(c.cfg.AlwaysAllowProcesses, proc) || strings.TrimSpace(t) == "" {
			return judge.Verdict{Cat: judge.CatAllow, Source: "rule", Prob: 1}, true
		}
		return judge.Verdict{}, false
	}
	for _, k := range c.cfg.AllowTitleKeywords {
		if !urlKnown && strings.Contains(t, k) {
			return judge.Verdict{Cat: judge.CatAllow, Source: "rule", Prob: 1}, true
		}
	}
	for _, k := range c.cfg.MusicTitleKeywords {
		if strings.Contains(t, k) {
			return judge.Verdict{Cat: judge.CatMusic, Source: "rule", Prob: 1}, true
		}
	}
	for _, k := range c.cfg.BlockTitleKeywords {
		if strings.Contains(t, k) {
			return judge.Verdict{Cat: judge.CatEntertainment, Source: "rule", Prob: 1}, true
		}
	}
	// Blank/new tab pages: nothing to judge.
	if strings.TrimSpace(t) == "" || strings.HasPrefix(t, "new tab") {
		return judge.Verdict{Cat: judge.CatAllow, Source: "rule", Prob: 1}, true
	}
	return judge.Verdict{}, false
}
