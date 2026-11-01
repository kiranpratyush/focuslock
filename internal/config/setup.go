package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SessionSetup is what the start pop-up asks: how long, the task, and the
// sites and apps this session needs on top of focus.json (Azure portal for
// a deploy, Postman for an API bug...). The last answers are remembered
// and pre-filled next time.
type SessionSetup struct {
	Duration     string   `json:"duration"`
	Task         string   `json:"task"`
	AllowDomains []string `json:"allowDomains"`
	AllowApps    []string `json:"allowApps"`
}

func LastSetupPath(dir string) string { return filepath.Join(dir, "last-session.json") }

func LoadLastSetup(dir string) SessionSetup {
	s := SessionSetup{Duration: "50m"}
	if b, err := os.ReadFile(LastSetupPath(dir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func SaveLastSetup(dir string, s SessionSetup) {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(LastSetupPath(dir), b, 0o644)
}

// splitList splits on commas, semicolons, spaces and new lines.
func splitList(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

// ParseDomainList turns "https://portal.azure.com/#home, dev.azure.com/myorg"
// into ["portal.azure.com", "dev.azure.com/myorg"].
func ParseDomainList(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range splitList(text) {
		d = strings.ToLower(d)
		if i := strings.Index(d, "://"); i >= 0 {
			d = d[i+3:]
		}
		if i := strings.IndexAny(d, "?#"); i >= 0 {
			d = d[:i]
		}
		d = strings.TrimSuffix(strings.TrimPrefix(d, "www."), "/")
		if d == "" || !strings.Contains(strings.SplitN(d, "/", 2)[0], ".") && d != "localhost" {
			continue // not a domain
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// ParseAppList turns "Postman, C:\Tools\ssms.exe, docker desktop" into exe
// names. Names with spaces need commas between them.
func ParseAppList(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }) {
		a = strings.ToLower(strings.TrimSpace(a))
		if i := strings.LastIndexAny(a, `\/`); i >= 0 {
			a = a[i+1:]
		}
		if a == "" {
			continue
		}
		if !strings.HasSuffix(a, ".exe") {
			a += ".exe"
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

// ApplySession adds this session's sites and apps (from the start window)
// to the allow lists. It copies the lists, so the loaded config isn't shared.
func (c *Config) ApplySession(domains, apps []string) {
	c.AllowDomains = append(append([]string(nil), c.AllowDomains...), domains...)
	c.AlwaysAllowProcesses = append(append([]string(nil), c.AlwaysAllowProcesses...), apps...)
}
