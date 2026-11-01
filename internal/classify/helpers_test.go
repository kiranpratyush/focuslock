package classify

import (
	"focuslock/internal/config"
)

// testConfig is the default config with the settle delay off, so the judge
// is asked at once (TestSettleDelay covers the delay).
func testConfig() config.Config {
	c := config.Default()
	c.Normalize()
	c.Judge.SettleMs = 0
	return c
}

// The wire formats the fake judge servers decode. They mirror package
// judge's unexported request types; tests only need these fields.
type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevRequest struct {
	Model     string                 `json:"model"`
	State     map[string]string      `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format"`
}
