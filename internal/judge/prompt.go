package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// What every judge is asked: the categories, the instructions, and the
// text prompt for judges that read free text (LLMs, Copilot).

// Options are the categories the judge picks from.
// With a task, "is this part of the task?" matters more than "is it fun?".
func Options(intent string) map[string]string {
	if intent == "" {
		return map[string]string{
			"work":          "Coding, documentation, email, work tools, technical reference",
			"learning":      "Technical talks, tutorials, courses, lectures, engineering blogs",
			"music":         "Audio meant only to be listened to in the background: song or album audio, playlists, lofi or ambient streams",
			"entertainment": "Anything meant to be watched for fun: vlogs, comedy, gaming, movies, shows, sports, memes, music videos, concerts, reactions, viral videos",
			"social":        "Social media feeds, forums browsed for fun, chatting",
		}
	}
	return map[string]string{
		"on_task":       "Directly helps with the current task: related code, docs, issues, references, searches, tutorials on this topic. A site's home page counts only if the whole site is about this topic",
		"supporting":    "General work tools that are fine during any task: email, team chat, calendar, notes, terminal, file explorer, a plain search page",
		"off_task":      "Work or learning that is unrelated to the current task",
		"music":         "Audio meant only to be listened to in the background: song or album audio, playlists, lofi or ambient streams",
		"entertainment": "Anything for fun: videos, vlogs, gaming, movies, shows, sports, memes, music videos, social media feeds",
	}
}

func instructions(q Question) string {
	var s string
	switch {
	case q.Explanation != "" && q.Intent != "":
		s = "A software engineer is in a focus session working on the current task. This window was blocked, and they explain why they need it. " +
			"Judge the window together with their explanation: if the explanation is specific and credibly connects this window to the task, it's on_task or supporting. " +
			"Vague reasons (\"research\", \"just a minute\", \"need a break\") don't count."
	case q.Explanation != "":
		s = "A software engineer is in a focus session. This window was blocked, and they explain why they need it. " +
			"Judge the window together with their explanation: if the explanation is specific and credibly makes this work or learning, choose that. " +
			"Vague reasons (\"research\", \"just a minute\", \"need a break\") don't count."
	case q.Intent != "":
		s = "A software engineer is in a focus session working on the current task. Based only on this window's app, title and page address, how does it relate to that task?"
	default:
		s = "A software engineer is in a focus session. Based only on this window's app, title and page address, what is it most likely for?"
	}
	if len(q.Notes) > 0 {
		s += " Notes they gave earlier in this session tell you more about the task."
	}
	return s
}

// llmPrompt is the prompt for judges that take free text (OpenAI-compatible
// APIs, Copilot). They reply with {"category", "confidence"} JSON.
func llmPrompt(q Question) (system, user string) {
	opts := Options(q.Intent)
	names := make([]string, 0, len(opts))
	for k := range opts {
		names = append(names, k)
	}
	sort.Strings(names)

	system = "You classify what a person has open on their computer during a focus session. " +
		`Reply with JSON only, like {"category": "<one of: ` + strings.Join(names, ", ") +
		`>", "confidence": <number from 0 to 1>}. No other text.`

	var b strings.Builder
	if strings.Contains(q.Intent, "\n") {
		b.WriteString("Current task:\n" + q.Intent + "\n") // spelled out (task_detail.go)
	} else if q.Intent != "" {
		b.WriteString("Current task: " + q.Intent + "\n")
	}
	if len(q.Notes) > 0 {
		b.WriteString("Notes from earlier in this session:\n")
		for _, n := range q.Notes {
			b.WriteString("- " + n + "\n")
		}
	}
	b.WriteString("App: " + q.App + "\n")
	b.WriteString("Window title: " + q.Title + "\n")
	if q.Page != "" {
		b.WriteString("Page: " + q.Page + "\n")
	}
	if q.Explanation != "" {
		b.WriteString("Their explanation: " + q.Explanation + "\n")
	}
	b.WriteString("\n" + instructions(q) + "\n\nCategories:\n")
	for _, n := range names {
		b.WriteString("- " + n + ": " + opts[n] + "\n")
	}
	return system, b.String()
}

// parseLLMAnswer reads {"category": ..., "confidence": ...} out of a model
// reply, tolerating code fences or stray text around the JSON.
func parseLLMAnswer(content string, opts map[string]string) (Answer, error) {
	i, j := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if i < 0 || j <= i {
		return Answer{}, fmt.Errorf("no JSON in reply: %q", content)
	}
	var out struct {
		Category   string   `json:"category"`
		Confidence *float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(content[i:j+1]), &out); err != nil {
		return Answer{}, fmt.Errorf("bad JSON in reply: %w", err)
	}
	cat := strings.ToLower(strings.TrimSpace(out.Category))
	if _, ok := opts[cat]; !ok {
		return Answer{}, fmt.Errorf("unknown category %q", out.Category)
	}
	conf := 0.8 // models that skip confidence are usually fairly sure
	if out.Confidence != nil {
		conf = *out.Confidence
		if conf > 1 && conf <= 100 {
			conf /= 100 // "85" meaning 85%
		}
		conf = max(0, min(1, conf))
	}
	return Answer{Choice: cat, Confidence: conf, Probabilities: map[string]float64{cat: conf}}, nil
}

func postJSON(ctx context.Context, client *http.Client, url, key string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
