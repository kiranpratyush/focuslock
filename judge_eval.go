package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// judge-eval: how good is a judge on your own browsing?
//
//	focuslock judge-eval make [file]   cases from focuslock.log, to label
//	focuslock judge-eval [file]        run the configured judge on them
//
// Label each case "A" (should be allowed) or "B" (should be blocked);
// leave "" or "?" for ones you're unsure about. Cases run through the real
// classifier, so rules, strict sites and thresholds all count.

type EvalCase struct {
	Task  string `json:"task"`
	App   string `json:"app"`
	Title string `json:"title"`
	Page  string `json:"page"`  // host+path; "" for apps
	Label string `json:"label"` // "A" allow, "B" block; anything else isn't scored
	Then  string `json:"then"`  // what the judge said when it was logged
}

var judgeLogLine = regexp.MustCompile(`^\S+ \S+ (\w+): (?:([^"]+\.exe) )?"(.*)" (\S*) ?-> (\w+) (?:\[(\w+) ([\d.]+)\]|\(([\d.]+)\))$`)

// makeEvalCases reads the judge's decisions out of a focuslock.log, one
// case per distinct task + window.
func makeEvalCases(r io.Reader) []EvalCase {
	var out []EvalCase
	seen := map[string]bool{}
	task := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, " worker started "):
			task = ""
			continue
		case strings.Contains(line, " task: "):
			task = strings.SplitN(line, " task: ", 2)[1]
			continue
		}
		m := judgeLogLine.FindStringSubmatch(line)
		if m == nil || !judgeLogNames[m[1]] {
			continue
		}
		app, title, page := m[2], m[3], m[4]
		if app == "" {
			app = "brave.exe" // early logs didn't record the app
		}
		page = strings.TrimSuffix(page, "/")
		key := task + "|" + app + "|" + title + "|" + page
		if seen[key] {
			continue
		}
		seen[key] = true
		then := m[1] + " said " + m[5]
		if m[6] != "" {
			then += " [" + m[6] + " " + m[7] + "]"
		}
		out = append(out, EvalCase{Task: task, App: app, Title: title, Page: page, Then: then})
	}
	return out
}

var judgeLogNames = map[string]bool{"jev": true, "llm": true, "copilot": true, "jevos": true}

func writeEvalCases(w io.Writer, cases []EvalCase) error {
	for _, c := range cases {
		b, _ := json.Marshal(c)
		if _, err := fmt.Fprintln(w, string(b)); err != nil {
			return err
		}
	}
	return nil
}

func readEvalCases(r io.Reader) ([]EvalCase, error) {
	var out []EvalCase
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c EvalCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		c.Label = strings.ToUpper(strings.TrimSpace(c.Label))
		out = append(out, c)
	}
	return out, sc.Err()
}

type evalSummary struct {
	Scored, Correct, WrongBlocks, Missed, Unscored, Errors int
	Median, P95                                            time.Duration
}

// runEval runs every case through a classifier using the configured judge.
func runEval(cfg Config, cases []EvalCase, w io.Writer) evalSummary {
	cfg.Judge.SettleMs = 0
	j := newJudge(cfg)
	if j == nil {
		fmt.Fprintln(w, "No judge configured (or it isn't available) — nothing to evaluate.")
		return evalSummary{}
	}
	if c, ok := j.(closingJudge); ok {
		defer c.Close()
	}
	fmt.Fprintf(w, "Judge: %s\n\n", judgeStatus(cfg))

	byTask := map[string]*Classifier{}
	var s evalSummary
	var took []time.Duration
	limit := judgeTimeout(j, cfg) + 5*time.Second
	for i, c := range cases {
		cls, ok := byTask[c.Task]
		if !ok {
			cls = NewClassifier(cfg, c.Task)
			cls.judge = j // one judge (one Copilot CLI, one connection) for all
			if detail, err := describeTask(cfg, j, c.Task); err != nil {
				fmt.Fprintf(w, "Couldn't spell out %q: %v\n", c.Task, err)
			} else if detail != "" {
				cls.SetTaskDetail(detail)
				fmt.Fprintf(w, "Task %q, as the judge spelled it out:\n  %s\n\n", c.Task, strings.ReplaceAll(detail, "\n", "\n  "))
			}
			byTask[c.Task] = cls
		}
		u := ""
		if c.Page != "" {
			u = "https://" + c.Page
		}
		start := time.Now()
		var v Verdict
		for {
			if v = cls.Classify(c.App, c.Title, u); v.Cat != CatPending || time.Since(start) > limit {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if v.Source == j.Name() {
			took = append(took, time.Since(start))
		}
		if v.Source == "fallback" || v.Source == "unjudged" || v.Cat == CatPending {
			s.Errors++
		}
		blocked := v.Cat == CatEntertainment || v.Cat == CatOffTask
		mark := ""
		switch c.Label {
		case "A", "B":
			s.Scored++
			switch {
			case blocked == (c.Label == "B"):
				s.Correct++
			case blocked:
				s.WrongBlocks++
				mark = "WRONG BLOCK"
			default:
				s.Missed++
				mark = "MISSED"
			}
		default:
			s.Unscored++
		}
		if mark != "" {
			task := "no task"
			if c.Task != "" {
				task = "task: " + shorten(c.Task, 40)
			}
			fmt.Fprintf(w, "%-11s #%-3d %-60s %s  (%s; %s/%s %.2f; %s)\n", mark, i+1, shorten(c.Title, 60), c.Page, task, v.Source, v.Cat, v.Prob, c.Then)
		}
	}
	if len(took) > 0 {
		sort.Slice(took, func(a, b int) bool { return took[a] < took[b] })
		s.Median = took[len(took)/2].Round(10 * time.Millisecond)
		s.P95 = took[max(0, len(took)*95/100-1)].Round(10 * time.Millisecond)
	}
	pct := 0.0
	if s.Scored > 0 {
		pct = 100 * float64(s.Correct) / float64(s.Scored)
	}
	fmt.Fprintf(w, "\n%d/%d correct (%.0f%%): %d real work blocked, %d distractions missed. %d not labelled.\n",
		s.Correct, s.Scored, pct, s.WrongBlocks, s.Missed, s.Unscored)
	if len(took) > 0 {
		fmt.Fprintf(w, "Judge answers: median %s, slowest 5%% over %s.\n", s.Median, s.P95)
	}
	if s.Errors > 0 {
		fmt.Fprintf(w, "WARNING: the judge failed or timed out on %d cases; those count as rules-only verdicts.\n", s.Errors)
	}
	return s
}

// judgeEvalCommand is "focuslock judge-eval ...".
func judgeEvalCommand(cfg Config, logPath, defaultFile string, args []string, w io.Writer) error {
	if len(args) > 0 && args[0] == "make" {
		path := defaultFile
		if len(args) > 1 {
			path = args[1]
		}
		lf, err := os.Open(logPath)
		if err != nil {
			return err
		}
		defer lf.Close()
		cases := makeEvalCases(lf)
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := writeEvalCases(f, cases); err != nil {
			return err
		}
		fmt.Fprintf(w, "Wrote %d cases to %s.\nLabel each \"A\" (should be allowed) or \"B\" (should be blocked), then run: focuslock judge-eval\n", len(cases), path)
		return nil
	}
	path := defaultFile
	if len(args) > 0 {
		path = args[0]
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w (create it with: focuslock judge-eval make)", err)
	}
	defer f.Close()
	cases, err := readEvalCases(f)
	if err != nil {
		return err
	}
	runEval(cfg, cases, w)
	return nil
}
