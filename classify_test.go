package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig() Config {
	c := defaultConfig()
	c.normalize()
	c.Judge.SettleMs = 0 // judge at once; TestSettleDelay covers the delay
	return c
}

func TestRules(t *testing.T) {
	c := NewClassifier(testConfig(), "")
	cases := []struct {
		proc, title string
		want        Category
	}{
		{"steam.exe", "Steam", CatEntertainment},
		{"spotify.exe", "Spotify Premium", CatMusic},
		{"code.exe", "main.go - focuslock - Visual Studio Code", CatAllow},
		{"chrome.exe", "Instagram - Google Chrome", CatEntertainment},
		{"chrome.exe", "Netflix - Google Chrome", CatEntertainment},
		{"chrome.exe", "lofi hip hop radio - YouTube - Google Chrome", CatMusic},
		{"chrome.exe", "golang/go: The Go programming language - GitHub - Google Chrome", CatAllow},
		{"msedge.exe", "New tab - Microsoft Edge", CatAllow},
		// Unknown tab, no API key: allowed (rules only).
		{"chrome.exe", "Funniest fails 2026 - YouTube - Google Chrome", CatAllow},
	}
	for _, tc := range cases {
		if got := c.Classify(tc.proc, tc.title, "").Cat; got != tc.want {
			t.Errorf("%s %q: got %s, want %s", tc.proc, tc.title, got, tc.want)
		}
	}
}

func fakeJev(t *testing.T, choice string, prob float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(401)
			return
		}
		var req jevRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		q, ok := req.Questions["category"]
		if !ok || q.Type != "choice" || len(q.Criteria) != 5 {
			t.Errorf("unexpected question: %+v", req.Questions)
		}
		if req.State["window_title"] == "" {
			t.Errorf("missing title in state")
		}
		if req.State["page"] != "youtube.com/watch" {
			t.Errorf("page should be host+path without query, got %q", req.State["page"])
		}
		probs := map[string]float64{"work": 0, "learning": 0, "music": 0, "entertainment": 0, "social": 0}
		probs[choice] = prob
		json.NewEncoder(w).Encode(map[string]any{
			"model": "jev-1.13.0",
			"answers": map[string]any{"category": map[string]any{
				"type": "choice", "choice": choice, "confidence": prob, "probabilities": probs,
			}},
		})
	}))
}

func classifyWithJev(t *testing.T, choice string, prob float64, title string, tweak ...func(*Config)) Verdict {
	srv := fakeJev(t, choice, prob)
	defer srv.Close()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	cfg := testConfig()
	cfg.Jev.Endpoint = srv.URL
	for _, f := range tweak {
		f(&cfg)
	}
	c := NewClassifier(cfg, "")

	if v := c.Classify("chrome.exe", title, "youtube.com/watch?v=abc123"); v.Cat != CatPending {
		t.Fatalf("first call should be pending, got %s", v.Cat)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := c.Classify("chrome.exe", title, "youtube.com/watch?v=abc123"); v.Cat != CatPending {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("jev never answered")
	return Verdict{}
}

func TestJevBlocksConfidentEntertainment(t *testing.T) {
	v := classifyWithJev(t, "entertainment", 0.92, "Funniest fails 2026 - YouTube")
	if v.Cat != CatEntertainment || v.Source != "jev" {
		t.Fatalf("got %+v", v)
	}
}

func TestJevUnsureMeansAllow(t *testing.T) {
	notStrict := func(c *Config) { c.StrictDomains = nil }
	v := classifyWithJev(t, "entertainment", 0.55, "Some ambiguous video - YouTube", notStrict)
	if v.Cat != CatAllow {
		t.Fatalf("low confidence should allow, got %+v", v)
	}
}

func TestJevUnsureOnStrictSiteBlocks(t *testing.T) {
	// From a real log: a YouTube page passed at learning 0.58.
	v := classifyWithJev(t, "learning", 0.58, "Some ambiguous video - YouTube")
	if v.Cat != CatEntertainment {
		t.Fatalf("unsure on youtube.com should block, got %+v", v)
	}
}

func TestJevLearningAllowed(t *testing.T) {
	v := classifyWithJev(t, "learning", 0.9, "Raft consensus explained - YouTube")
	if v.Cat != CatAllow {
		t.Fatalf("got %+v", v)
	}
}

func TestJevMusic(t *testing.T) {
	v := classifyWithJev(t, "music", 0.95, "Arijit Singh hits playlist - YouTube")
	if v.Cat != CatMusic {
		t.Fatalf("got %+v", v)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := []struct{ in, host, path string }{
		{"https://www.YouTube.com/shorts/abc?x=1", "youtube.com", "/shorts/abc"},
		{"youtube.com/watch?v=abc", "youtube.com", "/watch"},
		{"m.youtube.com/shorts/abc", "m.youtube.com", "/shorts/abc"},
		{"chrome://settings", "", ""},
		{"edge://newtab", "", ""},
		{"about:blank", "", ""},
		{"how to learn go", "", ""},
		{"localhost:8080/api", "localhost", "/api"},
		{"", "", ""},
	}
	for _, tc := range cases {
		h, p := normalizeURL(tc.in)
		if h != tc.host || p != tc.path {
			t.Errorf("%q: got (%q,%q), want (%q,%q)", tc.in, h, p, tc.host, tc.path)
		}
	}
}

func TestDomainRules(t *testing.T) {
	c := NewClassifier(testConfig(), "")
	cases := []struct {
		url, title string
		want       Category
	}{
		{"youtube.com/shorts/xyz", "Some short - YouTube", CatEntertainment},
		{"m.youtube.com/shorts/xyz", "Some short - YouTube", CatEntertainment},
		{"https://www.instagram.com/reels/1", "Instagram", CatEntertainment},
		{"music.youtube.com/watch?v=1", "Song - YouTube Music", CatMusic},
		{"github.com/golang/go", "Netflix clone repo", CatAllow}, // URL beats title
		{"chrome://settings", "Settings", CatAllow},
		{"myteam.atlassian.net/browse/X-1", "X-1 Fix bug", CatAllow},
		// Unknown domain, no Jev key: title rules then allow.
		{"example.com/netflix-review", "Netflix review blog", CatEntertainment},
		{"example.com/post", "Some post", CatAllow},
	}
	for _, tc := range cases {
		if got := c.Classify("chrome.exe", tc.title, tc.url).Cat; got != tc.want {
			t.Errorf("%s %q: got %s, want %s", tc.url, tc.title, got, tc.want)
		}
	}
}

func answer(probs map[string]float64) Answer {
	best, bp := "", -1.0
	for k, v := range probs {
		if v > bp {
			best, bp = k, v
		}
	}
	return Answer{Choice: best, Confidence: bp, Probabilities: probs}
}

func TestMusicVideoTossUpIsEntertainment(t *testing.T) {
	// Leans "music" but isn't very sure: a music video being watched.
	v := decide(answer(map[string]float64{"music": 0.6, "entertainment": 0.3, "work": 0.1}), 0.75, 0.9, false)
	if v.Cat != CatEntertainment {
		t.Fatalf("got %+v", v)
	}
}

func TestSureMusicIsMusic(t *testing.T) {
	v := decide(answer(map[string]float64{"music": 0.95, "entertainment": 0.05}), 0.75, 0.9, false)
	if v.Cat != CatMusic {
		t.Fatalf("got %+v", v)
	}
}

func TestLearningStaysAllowed(t *testing.T) {
	v := decide(answer(map[string]float64{"learning": 0.7, "entertainment": 0.2, "music": 0.1}), 0.75, 0.9, false)
	if v.Cat != CatAllow {
		t.Fatalf("got %+v", v)
	}
}

func TestOffTask(t *testing.T) {
	v := decide(answer(map[string]float64{"off_task": 0.8, "on_task": 0.2}), 0.75, 0.9, false)
	if v.Cat != CatOffTask {
		t.Fatalf("got %+v", v)
	}
	v = decide(answer(map[string]float64{"on_task": 0.6, "off_task": 0.4}), 0.75, 0.9, false)
	if v.Cat != CatAllow {
		t.Fatalf("unsure should allow, got %+v", v)
	}
	// LLM-style answer: single confidence, no full distribution.
	v = decide(Answer{Choice: "off_task", Confidence: 0.85, Probabilities: map[string]float64{"off_task": 0.85}}, 0.75, 0.9, false)
	if v.Cat != CatOffTask {
		t.Fatalf("got %+v", v)
	}
}

// ---- task (intent) mode ----

func waitVerdict(t *testing.T, c *Classifier, proc, title, u string) Verdict {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := c.Classify(proc, title, u); v.Cat != CatPending {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("judge never answered")
	return Verdict{}
}

func TestJevGetsTask(t *testing.T) {
	var got jevRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{
			"type": "choice", "choice": "off_task", "confidence": 0.9,
			"probabilities": map[string]float64{"on_task": 0.05, "supporting": 0.05, "off_task": 0.9},
		}}})
	}))
	defer srv.Close()
	t.Setenv("TYPESAFE_API_KEY", "k")
	cfg := testConfig()
	cfg.Jev.Endpoint = srv.URL
	c := NewClassifier(cfg, "fix pagination bug in orders API")

	v := waitVerdict(t, c, "chrome.exe", "Kubernetes operators deep dive", "example.com/k8s")
	if v.Cat != CatOffTask {
		t.Fatalf("got %+v", v)
	}
	if got.State["current_task"] != "fix pagination bug in orders API" {
		t.Errorf("task not sent: %+v", got.State)
	}
	if _, ok := got.Questions["category"].Criteria["on_task"]; !ok {
		t.Errorf("task-mode options not used: %+v", got.Questions["category"].Criteria)
	}
}

func TestAppsJudgedOnlyWithTask(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{
			"type": "choice", "choice": "off_task", "confidence": 0.9,
			"probabilities": map[string]float64{"off_task": 0.9, "on_task": 0.1},
		}}})
	}))
	defer srv.Close()
	t.Setenv("TYPESAFE_API_KEY", "k")
	cfg := testConfig()
	cfg.Jev.Endpoint = srv.URL

	noTask := NewClassifier(cfg, "")
	if v := noTask.Classify("winword.exe", "Trip plan.docx - Word", ""); v.Cat != CatAllow {
		t.Errorf("apps shouldn't be judged without a task, got %+v", v)
	}
	withTask := NewClassifier(cfg, "fix pagination bug")
	if v := withTask.Classify("code.exe", "orders.go - api - Visual Studio Code", ""); v.Cat != CatAllow || v.Source != "rule" {
		t.Errorf("editor is always allowed, got %+v", v)
	}
	if v := waitVerdict(t, withTask, "winword.exe", "Trip plan.docx - Word", ""); v.Cat != CatOffTask {
		t.Errorf("unrelated app with a task should be off-task, got %+v", v)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 judge call, got %d", calls)
	}
}

// ---- OpenAI-compatible (Ollama / LM Studio) ----

func TestOpenAIJudge(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		// Small models often wrap JSON in a code fence.
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
			"message": map[string]string{"role": "assistant", "content": "```json\n{\"category\": \"entertainment\", \"confidence\": 0.9}\n```"},
		}}})
	}))
	defer srv.Close()
	cfg := testConfig()
	cfg.Judge.Provider = "openai"
	cfg.OpenAI.BaseURL = srv.URL + "/v1"
	c := NewClassifier(cfg, "write SQL migration")

	v := waitVerdict(t, c, "chrome.exe", "Funniest fails 2026 - YouTube", "youtube.com/watch?v=x")
	if v.Cat != CatEntertainment || v.Source != "llm" {
		t.Fatalf("got %+v", v)
	}
	if got.Model != "qwen2.5:3b" || got.Temperature != 0 || got.ResponseFormat["type"] != "json_object" {
		t.Errorf("request: %+v", got)
	}
	user := got.Messages[1].Content
	for _, want := range []string{"Current task: write SQL migration", "Page: youtube.com/watch", "on_task:"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "v=x") {
		t.Errorf("query string leaked into prompt")
	}
}

func TestParseLLMAnswer(t *testing.T) {
	opts := Options("a task")
	ok := []struct {
		in   string
		cat  string
		conf float64
	}{
		{`{"category":"on_task","confidence":0.7}`, "on_task", 0.7},
		{`Sure! {"category": "OFF_TASK", "confidence": 85}`, "off_task", 0.85},
		{`{"category":"supporting"}`, "supporting", 0.8},
	}
	for _, tc := range ok {
		a, err := parseLLMAnswer(tc.in, opts)
		if err != nil || a.Choice != tc.cat || a.Confidence != tc.conf {
			t.Errorf("%q: got %+v, %v", tc.in, a, err)
		}
	}
	for _, bad := range []string{"entertainment", `{"category":"gaming"}`, `{oops}`} {
		if _, err := parseLLMAnswer(bad, opts); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestNoJudgeWhenJevDisabled(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "k")
	cfg := testConfig()
	cfg.Jev.Enabled = false // an older work-laptop config
	if c := NewClassifier(cfg, "x"); c.JudgeName() != "" {
		t.Fatalf("judge should be off, got %q", c.JudgeName())
	}
}

// ---- strict sites, placeholders, settle delay ----

func TestStrictDecide(t *testing.T) {
	cases := []struct {
		name  string
		probs map[string]float64
		want  Category
	}{
		{"sure learning", map[string]float64{"learning": 0.8, "entertainment": 0.2}, CatAllow},
		{"work+learning together", map[string]float64{"work": 0.4, "learning": 0.4, "social": 0.2}, CatAllow},
		{"coin-flip learning", map[string]float64{"learning": 0.51, "social": 0.49}, CatEntertainment},
		{"unsure on task", map[string]float64{"on_task": 0.5, "off_task": 0.3, "entertainment": 0.2}, CatOffTask},
		{"sure on task", map[string]float64{"on_task": 0.8, "off_task": 0.2}, CatAllow},
	}
	for _, tc := range cases {
		if got := decide(answer(tc.probs), 0.75, 0.9, true).Cat; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
	// LLM-style answer with one confidence.
	v := decide(Answer{Choice: "learning", Confidence: 0.6, Probabilities: map[string]float64{"learning": 0.6}}, 0.75, 0.9, true)
	if v.Cat != CatEntertainment {
		t.Errorf("LLM unsure learning on strict site: got %+v", v)
	}
}

func TestPlaceholderTitles(t *testing.T) {
	cases := []struct {
		title, host string
		want        bool
	}{
		{"YouTube - Brave", "youtube.com", true},
		{"Live - YouTube - Brave", "youtube.com", true},
		{"(3) YouTube - Google Chrome", "youtube.com", true},
		{"X - Brave", "x.com", true},
		{"Home / X - Microsoft Edge", "x.com", true},
		{"x.com/shubh6200/status/2091504468122816717 - Brave", "x.com", true},
		{"reddit - Brave", "reddit.com", true},
		{"", "youtube.com", true},
		{"Raft consensus explained - YouTube - Brave", "youtube.com", false},
		{"Devendra Fadnavis LIVE | Maharashtra Politics - YouTube - Brave", "youtube.com", false},
		{`Shubham Mishra on X: "PubSub is everywhere" / X - Brave`, "x.com", false},
		{"lo-fi beats - YouTube - Brave", "youtube.com", false},
	}
	for _, tc := range cases {
		if got := isPlaceholderTitle(tc.title, tc.host); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.title, got, tc.want)
		}
	}
}

func TestPlaceholderBlockedOnlyOnStrictSitesWithJudge(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	c := NewClassifier(testConfig(), "")
	if v := c.Classify("brave.exe", "YouTube - Brave", "https://www.youtube.com/channel/UC4R8"); v.Cat != CatEntertainment || v.Source != "placeholder" {
		t.Errorf("youtube placeholder: got %+v", v)
	}
	// Not a strict site: goes to the judge as before.
	if v := c.Classify("brave.exe", "Medium - Brave", "https://medium.com/"); v.Cat != CatPending || v.Hold {
		t.Errorf("non-strict site: got %+v", v)
	}
	// No judge (work laptop mode): rules only, nothing blocked by this.
	cfg := testConfig()
	cfg.Judge.Provider = "none"
	c = NewClassifier(cfg, "")
	if v := c.Classify("brave.exe", "YouTube - Brave", "https://www.youtube.com/"); v.Cat != CatAllow {
		t.Errorf("no judge: got %+v", v)
	}
}

func TestSettleDelay(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{
			"type": "choice", "choice": "learning", "confidence": 0.9,
			"probabilities": map[string]float64{"learning": 0.9, "entertainment": 0.1},
		}}})
	}))
	defer srv.Close()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	cfg := testConfig()
	cfg.Jev.Endpoint = srv.URL
	cfg.Judge.SettleMs = 1200
	c := NewClassifier(cfg, "")
	clock := time.Now()
	c.now = func() time.Time { return clock }

	const u = "https://www.youtube.com/watch?v=abc"
	// Titles flip while the page loads; none has been stable for 1.2s.
	for _, title := range []string{"Loading one - YouTube", "Loading two - YouTube", "Raft explained - YouTube"} {
		v := c.Classify("brave.exe", title, u)
		if v.Cat != CatPending || !v.Hold {
			t.Fatalf("%q: want held pending, got %+v", title, v)
		}
		clock = clock.Add(500 * time.Millisecond)
	}
	clock = clock.Add(800 * time.Millisecond) // "Raft explained" now stable for 1.3s
	c.Classify("brave.exe", "Raft explained - YouTube", u)
	v := waitVerdict(t, c, "brave.exe", "Raft explained - YouTube", u)
	if v.Cat != CatAllow {
		t.Fatalf("got %+v", v)
	}
	if asked != 1 {
		t.Fatalf("judge asked %d times, want 1 (only the settled title)", asked)
	}
	// Cached: answers at once even though the settle window moved on.
	if v := c.Classify("brave.exe", "Raft explained - YouTube", u); v.Cat != CatAllow {
		t.Fatalf("cached: got %+v", v)
	}
}

// ---- misses from the 2026-11-01 task session ----

// fakeJudge answers every question with probs and counts the calls.
func fakeJudge(t *testing.T, probs map[string]float64) (*atomic.Int32, func(intent string, tweak ...func(*Config)) *Classifier) {
	t.Helper()
	calls := &atomic.Int32{}
	best, bp := "", -1.0
	for k, v := range probs {
		if v > bp {
			best, bp = k, v
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{
			"type": "choice", "choice": best, "confidence": bp, "probabilities": probs,
		}}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TYPESAFE_API_KEY", "k")
	mk := func(intent string, tweak ...func(*Config)) *Classifier {
		cfg := testConfig()
		cfg.Jev.Endpoint = srv.URL
		for _, f := range tweak {
			f(&cfg)
		}
		return NewClassifier(cfg, intent)
	}
	return calls, mk
}

const goTask = "Learn Go channels and select: buffered vs unbuffered, closing channels, worker pools"

func TestSiteNameTitlesArePlaceholders(t *testing.T) {
	cases := []struct {
		title, host string
		want        bool
	}{
		{"Hello Interview - Brave", "hellointerview.com", true},
		{"Wikipedia - Wikipedia - Brave", "en.wikipedia.org", true},
		{"Go by Example - Brave", "gobyexample.com", true},
		{"BBC - Brave", "bbc.co.uk", true},
		{"Go by Example: Channels - Brave", "gobyexample.com", false},
		{"Systems design - Wikipedia - Brave", "en.wikipedia.org", false},
	}
	for _, tc := range cases {
		if got := isPlaceholderTitle(tc.title, tc.host); got != tc.want {
			t.Errorf("%q on %s: got %v, want %v", tc.title, tc.host, got, tc.want)
		}
	}
}

func TestSiteNameTitleWithTaskNeedsSureJudge(t *testing.T) {
	// A guess about a bare home page isn't enough with a task set...
	_, mk := fakeJudge(t, map[string]float64{"on_task": 0.6, "supporting": 0.1, "off_task": 0.3})
	if v := waitVerdict(t, mk(goTask), "brave.exe", "Hello Interview - Brave", "https://www.hellointerview.com/"); v.Cat != CatOffTask {
		t.Errorf("unsure home page with a task should be covered, got %+v", v)
	}
	// ...but a sure one is (Go by Example's home is all about Go).
	_, mk = fakeJudge(t, map[string]float64{"on_task": 0.9, "off_task": 0.1})
	if v := waitVerdict(t, mk(goTask), "brave.exe", "Go by Example - Brave", "https://gobyexample.com/"); v.Cat != CatAllow {
		t.Errorf("sure on-task home page should pass, got %+v", v)
	}
	// Without a task, home pages of non-strict sites are judged as before.
	_, mk = fakeJudge(t, map[string]float64{"learning": 0.6, "entertainment": 0.4})
	if v := waitVerdict(t, mk(""), "brave.exe", "Hello Interview - Brave", "https://www.hellointerview.com/"); v.Cat != CatAllow {
		t.Errorf("no task: got %+v", v)
	}
}

func TestUnsureAppsCoveredWithTask(t *testing.T) {
	// From the log: whatsapp.root.exe "WhatsApp" -> allow [off_task 0.51]
	probs := map[string]float64{"off_task": 0.51, "supporting": 0.29, "on_task": 0.2}
	_, mk := fakeJudge(t, probs)
	if v := waitVerdict(t, mk(goTask), "whatsapp.root.exe", "WhatsApp", ""); v.Cat != CatOffTask {
		t.Errorf("unsure app with a task should be covered, got %+v", v)
	}
	_, mk = fakeJudge(t, probs)
	lenient := mk(goTask, func(c *Config) { c.Judge.StrictApps = false })
	if v := waitVerdict(t, lenient, "whatsapp.root.exe", "WhatsApp", ""); v.Cat != CatAllow {
		t.Errorf("strictApps=false should allow unsure apps, got %+v", v)
	}
}

func TestBrowserPopupsNotJudged(t *testing.T) {
	calls, mk := fakeJudge(t, map[string]float64{"off_task": 0.77, "on_task": 0.23})
	c := mk(goTask)
	// From the log: brave.exe "\"Application launcher for Drive (by Google)\" added" -> off_task 0.77
	if v := c.Classify("brave.exe", `"Application launcher for Drive (by Google)" added`, ""); v.Cat != CatAllow || v.Source != "rule" {
		t.Errorf("pop-up: got %+v", v)
	}
	// An installed web app has no address bar or " - Brave", but it's a page: judged.
	if v := c.Classify("brave.exe", "Funny video - YouTube", ""); v.Cat != CatPending {
		t.Errorf("web app window should be judged, got %+v", v)
	}
	// Edge puts a zero-width space in its name.
	if !hasBrowserSuffix("New tab - Microsoft​ Edge") {
		t.Error("Edge suffix not recognised")
	}
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Errorf("judge calls: got %d, want 1 (only the web app)", n)
	}
}

func TestPlaceholderWaitsForSettle(t *testing.T) {
	_, mk := fakeJudge(t, map[string]float64{"learning": 0.9, "entertainment": 0.1})
	c := mk("", func(c *Config) { c.Judge.SettleMs = 1200 })
	clock := time.Now()
	c.now = func() time.Time { return clock }
	const u = "https://www.youtube.com/watch?v=eqSgTJ6HTsw"
	// From the log: this loading title used to flash a block before the real one arrived.
	if v := c.Classify("brave.exe", "youtube.com/watch?v=eqSgTJ6HTsw - Brave", u); v.Cat != CatPending || !v.Hold {
		t.Fatalf("loading title should wait, got %+v", v)
	}
	// A placeholder that stays (the home page) is blocked once it settles.
	const home = "https://www.youtube.com/"
	c.Classify("brave.exe", "YouTube - Brave", home)
	clock = clock.Add(1300 * time.Millisecond)
	if v := c.Classify("brave.exe", "YouTube - Brave", home); v.Cat != CatEntertainment || v.Source != "placeholder" {
		t.Fatalf("settled placeholder: got %+v", v)
	}
}

func TestSettleWithTwoMonitors(t *testing.T) {
	// The window you're in and a video on the other monitor are classified
	// in turn every tick; neither may reset the other's settle wait.
	calls, mk := fakeJudge(t, map[string]float64{"entertainment": 0.95, "learning": 0.05})
	c := mk("", func(c *Config) { c.Judge.SettleMs = 1200 })
	clock := time.Now()
	c.now = func() time.Time { return clock }
	for i := 0; i < 4; i++ { // 4 ticks of 500ms
		c.Classify("brave.exe", "Funniest fails 2026 - YouTube - Brave", "https://www.youtube.com/watch?v=a")
		c.Classify("code.exe", "main.go - Visual Studio Code", "")
		clock = clock.Add(500 * time.Millisecond)
	}
	v := waitVerdict(t, c, "brave.exe", "Funniest fails 2026 - YouTube - Brave", "https://www.youtube.com/watch?v=a")
	if v.Cat != CatEntertainment || calls.Load() != 1 {
		t.Fatalf("got %+v after %d calls", v, calls.Load())
	}
}
