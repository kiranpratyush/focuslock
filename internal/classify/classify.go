package classify

import (
	"context"
	"strings"
	"sync"
	"time"

	"focuslock/internal/config"
	"focuslock/internal/judge"
	"focuslock/internal/logx"
	"focuslock/internal/session"
	"focuslock/internal/textutil"
)

// Classifier decides what a foreground window is.
// Rules answer instantly; the judge (Jev or an LLM) answers asynchronously
// and is cached.
type Classifier struct {
	cfg    config.Config
	intent string
	// The task as the judge spelled it out (task_detail.go); "" = as written.
	taskDetail string
	judge      judge.Judge // nil = rules only

	mu       sync.Mutex
	cache    map[string]judge.Verdict
	inflight map[string]bool
	failedAt map[string]time.Time

	// The title must stay the same for cfg.Judge.SettleMs before it's
	// judged. Kept per window key: windows on other monitors are classified
	// every tick too, and must not reset each other's wait.
	seen map[string]seenAt
	now  func() time.Time // for tests

	// Accepted explanations (explain.go) and how many were tried.
	notes       []session.Note
	explainUsed int
}

func New(cfg config.Config, intent string) *Classifier {
	return &Classifier{
		cfg:      cfg,
		intent:   strings.TrimSpace(intent),
		judge:    judge.New(cfg),
		cache:    map[string]judge.Verdict{},
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

// SetTaskDetail gives the judge a fuller description of the task. The bar
// and block screen keep showing the task as written.
func (c *Classifier) SetTaskDetail(detail string) { c.taskDetail = strings.TrimSpace(detail) }

// Classify never blocks on the network. proc is the lowercase exe name;
// rawURL is the address bar text ("" when unknown).
func (c *Classifier) Classify(proc, title, rawURL string) judge.Verdict {
	host, path := NormalizeURL(rawURL)
	isBrowser := textutil.Contains(c.cfg.Browsers, proc)
	if v, ok := c.byURL(proc, rawURL, host, path); ok {
		return v
	}
	if v, ok := c.byRules(proc, title, host != ""); ok {
		return v
	}
	if isBrowser && strings.TrimSpace(rawURL) == "" && isBrowserPopup(title) {
		return judge.Verdict{Cat: judge.CatAllow, Source: "rule", Prob: 1}
	}
	if c.judge == nil {
		return judge.Verdict{Cat: judge.CatAllow, Source: "fallback"}
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
	pending := judge.Verdict{Cat: judge.CatPending, Source: c.judge.Name(), Hold: strictSite}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.explained(proc, title, host, path) {
		return judge.Verdict{Cat: judge.CatAllow, Source: "explained", Prob: 1}
	}
	q.Notes = c.noteTexts()
	if v, ok := c.cache[key]; ok {
		return v
	}
	if t, ok := c.failedAt[key]; ok && time.Since(t) < 30*time.Second {
		if strictSite {
			// The judge isn't answering (jevos stopped, no network...). On
			// strict sites unsure means blocked, so a dead judge isn't a way in.
			return judge.Verdict{Cat: judge.CatEntertainment, Source: "unjudged", Prob: 1}
		}
		return judge.Verdict{Cat: judge.CatAllow, Source: "fallback"}
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
		return judge.Verdict{Cat: judge.CatEntertainment, Source: "placeholder", Prob: 1}
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
func (c *Classifier) question(proc, title, host, path string) judge.Question {
	page := ""
	if c.cfg.Judge.SendURL && host != "" {
		page = host + path
	}
	task := c.intent
	if task != "" && c.taskDetail != "" {
		task = c.taskDetail
	}
	return judge.Question{Intent: task, App: proc, Title: title, Page: page}
}

func (c *Classifier) ask(key string, q judge.Question, strict bool) {
	ctx, cancel := context.WithTimeout(context.Background(), judge.Timeout(c.judge, c.cfg))
	defer cancel()
	a, err := c.judge.Ask(ctx, q)
	var v judge.Verdict
	if err == nil {
		v = judge.VerdictFor(c.judge, a, strict, c.cfg.Judge)
		v.Source = c.judge.Name()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, key)
	if err != nil {
		logx.Printf("%s error: %v", c.judge.Name(), err)
		c.failedAt[key] = time.Now()
		return
	}
	if len(c.cache) > 2000 {
		c.cache = map[string]judge.Verdict{}
	}
	c.cache[key] = v
	logx.Printf("%s: %s %q %s -> %s [%s %.2f]", c.judge.Name(), q.App, q.Title, q.Page, v.Cat, a.Choice, v.Prob)
}
