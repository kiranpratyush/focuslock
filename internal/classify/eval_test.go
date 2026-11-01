package classify

import (
	"bytes"
	"strings"
	"testing"
)

const sampleLog = `2026/11/01 17:39:20 worker started (pid 10360, peer 1784), 01:58 left
2026/11/01 17:39:43 jev: "what is chan in go at DuckDuckGo - Brave" duckduckgo.com/ -> allow (0.56)
2026/11/01 18:47:48 worker started (pid 3172, peer 13812), 03:58 left
2026/11/01 18:47:48 task: Learn Go channels and select
2026/11/01 18:48:39 jev: brave.exe "Funniest fails 2026 - YouTube - Brave" youtube.com/watch -> entertainment [entertainment 0.99]
2026/11/01 18:48:41 blocked [jev/entertainment 0.99]: brave.exe — "Funniest fails 2026 - YouTube - Brave" youtube.com/watch?v=x
2026/11/01 18:48:50 jev: brave.exe "Funniest fails 2026 - YouTube - Brave" youtube.com/watch -> entertainment [entertainment 0.99]
2026/11/01 18:50:08 jev: docker desktop.exe "Containers - Docker Desktop"  -> off_task [off_task 0.77]
2026/11/01 18:51:00 copilot: brave.exe "Go by Example: Channels - Brave" gobyexample.com/channels -> allow [on_task 0.90]
`

func TestMakeEvalCases(t *testing.T) {
	cases := makeEvalCases(strings.NewReader(sampleLog))
	if len(cases) != 4 {
		t.Fatalf("want 4 distinct cases, got %d: %+v", len(cases), cases)
	}
	if c := cases[0]; c.Task != "" || c.App != "brave.exe" || c.Page != "duckduckgo.com" || c.Then != "jev said allow" {
		t.Errorf("old-format line: %+v", c)
	}
	if c := cases[1]; c.Task != "Learn Go channels and select" || c.Then != "jev said entertainment [entertainment 0.99]" {
		t.Errorf("task line: %+v", c)
	}
	if c := cases[2]; c.App != "docker desktop.exe" || c.Page != "" {
		t.Errorf("app with a space, no page: %+v", c)
	}
	if c := cases[3]; c.Title != "Go by Example: Channels - Brave" {
		t.Errorf("other judge: %+v", c)
	}
	var buf bytes.Buffer
	writeEvalCases(&buf, cases)
	back, err := readEvalCases(&buf)
	if err != nil || len(back) != 4 || back[1] != cases[1] {
		t.Errorf("round trip: %v %+v", err, back)
	}
}

func TestRunEval(t *testing.T) {
	// A judge that calls everything entertainment: right on the fail video,
	// wrong on Go by Example; the search page is allowed by the search rule.
	_, mk := fakeJudge(t, map[string]float64{"entertainment": 0.95, "on_task": 0.05})
	cfg := mk("").cfg
	cases := []EvalCase{
		{Task: "Learn Go channels", App: "brave.exe", Title: "Funniest fails 2026 - YouTube - Brave", Page: "youtube.com/watch", Label: "B"},
		{Task: "Learn Go channels", App: "brave.exe", Title: "Go by Example: Channels - Brave", Page: "gobyexample.com/channels", Label: "A"},
		{Task: "Learn Go channels", App: "brave.exe", Title: "go channels at DuckDuckGo - Brave", Page: "duckduckgo.com", Label: "A"},
		{Task: "Learn Go channels", App: "brave.exe", Title: "Hmm - Brave", Page: "example.com", Label: "?"},
	}
	var out bytes.Buffer
	s := runEval(cfg, cases, &out)
	if s.Scored != 3 || s.Correct != 2 || s.WrongBlocks != 1 || s.Missed != 0 || s.Unscored != 1 || s.Errors != 0 {
		t.Fatalf("summary %+v\n%s", s, out.String())
	}
	if !strings.Contains(out.String(), "WRONG BLOCK") || !strings.Contains(out.String(), "Go by Example") {
		t.Errorf("report:\n%s", out.String())
	}
}
