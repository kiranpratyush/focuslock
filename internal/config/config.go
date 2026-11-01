package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config is loaded from focus.json in the FocusLock data folder.
// A default file is written on first run so you can edit it.
type Config struct {
	Browsers []string `json:"browsers"`
	// Domain rules match the active tab's URL (read from the address bar).
	// "youtube.com" also matches m.youtube.com; "youtube.com/shorts" matches that path.
	// Checked in order: allow, music, block. URL rules run before title rules.
	AllowDomains []string `json:"allowDomains"`
	MusicDomains []string `json:"musicDomains"`
	BlockDomains []string `json:"blockDomains"`
	// Web search results pages are always fine: the page you open from them
	// is judged. (Not YouTube's results: that's a wall of videos.)
	SearchDomains []string `json:"searchDomains"`
	// Distraction sites the judge must clear: a tab here is allowed only when
	// the judge is sure it's work or learning, and placeholder titles
	// ("YouTube", a bare URL) are blocked without asking.
	StrictDomains    []string `json:"strictDomains"`
	BlockedProcesses []string `json:"blockedProcesses"`
	MusicProcesses   []string `json:"musicProcesses"`
	// Title keywords that allow a tab. Only used when the tab's web address
	// can't be read; otherwise allowDomains decides.
	AllowTitleKeywords []string    `json:"allowTitleKeywords"`
	BlockTitleKeywords []string    `json:"blockTitleKeywords"`
	MusicTitleKeywords []string    `json:"musicTitleKeywords"`
	Music              MusicConfig `json:"music"`
	// After an entertainment tab has stayed in front (under the overlay)
	// this long, FocusLock sends Ctrl+W to close it. 0 disables.
	CloseTabAfterSeconds int `json:"closeTabAfterSeconds"`
	AppBarHeight         int `json:"appBarHeight"`
	// Pixels at 100% scaling left uncovered at the top of the browser,
	// so the tab strip and address bar stay usable. ~90 fits Chrome/Edge.
	OverlayTopOffset int `json:"overlayTopOffset"`
	// Apps never judged against your task (editors, terminals, chat...).
	AlwaysAllowProcesses []string      `json:"alwaysAllowProcesses"`
	Judge                JudgeConfig   `json:"judge"`
	Jev                  JevConfig     `json:"jev"`
	OpenAI               OpenAIConfig  `json:"openai"`
	Copilot              CopilotConfig `json:"copilot"`
	Jevos                JevosConfig   `json:"jevos"`
	Explain              ExplainConfig `json:"explain"`
	// Email, chat: allowed in check windows (e.g. 5 minutes every 30), covered in between.
	Rations []RationRule `json:"rations"`
}

type MusicConfig struct {
	Allowed                bool `json:"allowed"`
	ForegroundGraceSeconds int  `json:"foregroundGraceSeconds"`
}

// JudgeConfig picks who classifies tabs/apps the rules don't cover.
//
//	provider "jev"    — TypeSafe Jev (needs TYPESAFE_API_KEY)
//	provider "openai" — any OpenAI-compatible chat API: Ollama, LM Studio, OpenAI, Groq...
//	provider "copilot" — GitHub Copilot through the Copilot CLI (work laptop)
//	provider "jevos"  — jevos, an open-source Jev on your own CPU
//	provider "none"   — rules only, nothing leaves the machine
type JudgeConfig struct {
	Provider  string  `json:"provider"`
	Threshold float64 `json:"threshold"`
	// The judge must be at least this sure before something counts as
	// background music (music gets a grace period; a misjudged video would too).
	MusicThreshold float64 `json:"musicThreshold"`
	TimeoutMs      int     `json:"timeoutMs"`
	// A changed title must stay the same this long before it's judged, so the
	// placeholder a page shows while loading is never sent. 0 disables.
	SettleMs int `json:"settleMs"`
	// With a task, an app the judge isn't sure about is covered (overlay only,
	// never killed). false: unsure apps are allowed, as before.
	StrictApps bool `json:"strictApps"`
	// Judges that can write (Copilot, LLMs) spell the task out in four lines
	// once per session, and judge against that.
	DescribeTask bool `json:"describeTask"`
	// Send the tab's domain+path (never the ?query part) along with the title.
	SendURL bool `json:"sendUrl"`
}

type JevConfig struct {
	Enabled   bool   `json:"enabled"` // false turns Jev off even if provider is "jev"
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"apiKeyEnv"`
}

// CopilotConfig: provider "copilot" uses the GitHub Copilot CLI you're signed in to.
type CopilotConfig struct {
	// Model id; "focuslock judge-check" lists yours with their premium-request cost.
	// Every new tab or app judged is one request, so pick a cheap or included model.
	Model   string `json:"model"`
	CLIPath string `json:"cliPath"` // "" = find "copilot" on PATH
}

// JevosConfig: provider "jevos", the local open-source Jev (github.com/feder-cr/jev).
type JevosConfig struct {
	Endpoint string `json:"endpoint"`
	// Path to jev.exe (with its model/ folder beside it). When set, FocusLock
	// starts it for the session, restarts it if it stops, and stops it after.
	// "" means you run it yourself.
	Exe       string `json:"exe"`
	Threads   int    `json:"threads"`   // 0 = half your CPU's threads
	APIKeyEnv string `json:"apiKeyEnv"` // only if you set JEV_API_KEY on the server
}

// ExplainConfig: when the judge blocks something, you can say why you need it.
// If the judge accepts the reason, that page (or site, or app) is allowed for
// the rest of the session and the reason is kept as context for later checks.
type ExplainConfig struct {
	Enabled bool `json:"enabled"`
	// Explanations per session; each attempt counts, accepted or not.
	MaxPerSession int `json:"maxPerSession"`
}

type OpenAIConfig struct {
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"apiKeyEnv"` // optional for local servers
	JSONMode  bool   `json:"jsonMode"`  // set false if your server rejects response_format
}

func Default() Config {
	return Config{
		Browsers:         []string{"chrome.exe", "msedge.exe", "firefox.exe", "brave.exe", "opera.exe", "vivaldi.exe"},
		BlockedProcesses: []string{"steam.exe", "epicgameslauncher.exe", "battle.net.exe", "riotclientservices.exe", "netflix.exe"},
		MusicProcesses:   []string{"spotify.exe"},
		AllowDomains: []string{
			"github.com", "stackoverflow.com", "stackexchange.com", "go.dev", "pkg.go.dev",
			"learn.microsoft.com", "dev.azure.com", "visualstudio.com", "atlassian.net",
			"postgresql.org", "leetcode.com", "claude.ai", "chatgpt.com", "localhost", "127.0.0.1",
		},
		MusicDomains: []string{"music.youtube.com", "open.spotify.com", "jiosaavn.com", "gaana.com"},
		BlockDomains: []string{
			"youtube.com/shorts", "instagram.com", "facebook.com", "netflix.com", "primevideo.com",
			"hotstar.com", "twitch.tv", "9gag.com", "cricbuzz.com",
		},
		StrictDomains: []string{"youtube.com", "x.com", "twitter.com", "reddit.com"},
		SearchDomains: []string{
			"duckduckgo.com", "google.com/search", "google.co.in/search", "bing.com/search",
			"search.brave.com", "search.yahoo.com", "ecosia.org/search", "startpage.com",
		},
		AllowTitleKeywords: []string{
			"visual studio code", "stack overflow", "github", "localhost", "docs", "documentation",
			"leetcode", "chatgpt", "claude", "postgresql",
		},
		BlockTitleKeywords: []string{
			"netflix", "prime video", "hotstar", "instagram", "facebook", "twitch",
			"youtube shorts", "reels", "9gag", "cricbuzz",
		},
		MusicTitleKeywords:   []string{"spotify", "youtube music", "jiosaavn", "gaana", "lofi", "lo-fi"},
		Music:                MusicConfig{Allowed: true, ForegroundGraceSeconds: 60},
		CloseTabAfterSeconds: 120,
		AppBarHeight:         28,
		OverlayTopOffset:     90,
		AlwaysAllowProcesses: []string{
			"code.exe", "devenv.exe", "rider64.exe", "idea64.exe", "goland64.exe", "windowsterminal.exe",
			"wt.exe", "cmd.exe", "powershell.exe", "pwsh.exe", "wsl.exe", "ubuntu.exe", "explorer.exe",
			"taskmgr.exe", "ms-teams.exe", "teams.exe", "outlook.exe", "olk.exe", "slack.exe",
			"postman.exe", "ssms.exe", "notepad.exe", "obsidian.exe", "claude.exe",
			"msrdc.exe", "googledrivefs.exe", // Remote Desktop / WSL apps, Google Drive
			"searchhost.exe", "shellexperiencehost.exe", "startmenuexperiencehost.exe", "lockapp.exe",
		},
		Judge: JudgeConfig{
			Provider:       "jev",
			Threshold:      0.75,
			MusicThreshold: 0.9,
			TimeoutMs:      8000,
			SettleMs:       1200,
			DescribeTask:   true,
			StrictApps:     true,
			SendURL:        true,
		},
		Jev: JevConfig{
			Enabled:   true,
			Endpoint:  "https://api.typesafe.ai/v1/systemone",
			Model:     "jev-latest",
			APIKeyEnv: "TYPESAFE_API_KEY",
		},
		Copilot: CopilotConfig{Model: "gpt-5-mini"},
		Jevos:   JevosConfig{Endpoint: "http://127.0.0.1:8017/v1/systemone", APIKeyEnv: "JEV_API_KEY"},
		Explain: ExplainConfig{Enabled: true, MaxPerSession: 3},
		Rations: []RationRule{
			{
				Name:         "Email",
				Match:        []string{"outlook.exe", "olk.exe", "outlook.office.com", "outlook.office365.com", "outlook.live.com", "mail.google.com"},
				EveryMinutes: 30, ForMinutes: 5,
			},
			{
				Name:         "Teams",
				Match:        []string{"ms-teams.exe", "teams.exe", "teams.microsoft.com", "teams.live.com"},
				EveryMinutes: 20, ForMinutes: 5,
				// Meetings and calls are never covered.
				ExceptTitleKeywords: []string{"meeting", "call", "sharing", "presenting"},
			},
		},
		OpenAI: OpenAIConfig{
			BaseURL:   "http://localhost:11434/v1",
			Model:     "qwen2.5:3b",
			APIKeyEnv: "OPENAI_API_KEY",
			JSONMode:  true,
		},
	}
}

// Load reads focus.json from dir, writing the default if it is missing.
func Load(dir string) (Config, error) {
	path := filepath.Join(dir, "focus.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		out, _ := json.MarshalIndent(cfg, "", "  ")
		_ = os.WriteFile(path, out, 0o644)
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg := Default() // missing fields keep defaults
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	cfg.Normalize()
	return cfg, nil
}

// Normalize lower-cases lists and fills in defaults for empty settings.
func (c *Config) Normalize() {
	lower := func(xs []string) []string {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if x = strings.ToLower(strings.TrimSpace(x)); x != "" {
				out = append(out, x)
			}
		}
		return out
	}
	c.Browsers = lower(c.Browsers)
	c.AlwaysAllowProcesses = lower(c.AlwaysAllowProcesses)
	c.Judge.Provider = strings.ToLower(strings.TrimSpace(c.Judge.Provider))
	if c.Judge.TimeoutMs <= 0 {
		c.Judge.TimeoutMs = 8000
	}
	c.AllowDomains = lower(c.AllowDomains)
	c.MusicDomains = lower(c.MusicDomains)
	c.BlockDomains = lower(c.BlockDomains)
	c.StrictDomains = lower(c.StrictDomains)
	c.SearchDomains = lower(c.SearchDomains)
	if c.Judge.SettleMs < 0 {
		c.Judge.SettleMs = 0
	}
	c.BlockedProcesses = lower(c.BlockedProcesses)
	c.MusicProcesses = lower(c.MusicProcesses)
	c.AllowTitleKeywords = lower(c.AllowTitleKeywords)
	c.BlockTitleKeywords = lower(c.BlockTitleKeywords)
	c.MusicTitleKeywords = lower(c.MusicTitleKeywords)
	for i := range c.Rations {
		r := &c.Rations[i]
		r.Match = lower(r.Match)
		r.ExceptTitleKeywords = lower(r.ExceptTitleKeywords)
		if r.Name == "" && len(r.Match) > 0 {
			r.Name = r.Match[0]
		}
		r.EveryMinutes = max(r.EveryMinutes, 1)
		r.ForMinutes = max(r.ForMinutes, 1)
	}
	if c.AppBarHeight <= 0 {
		c.AppBarHeight = 28
	}
}

// RationRule is one entry of "rations" in focus.json.
type RationRule struct {
	Name string `json:"name"` // shown on the block screen: "Email"
	// Exe names ("outlook.exe") and domains ("outlook.office.com").
	Match        []string `json:"match"`
	EveryMinutes int      `json:"everyMinutes"`
	ForMinutes   int      `json:"forMinutes"`
	// Never covered when the title contains one of these (a Teams call).
	ExceptTitleKeywords []string `json:"exceptTitleKeywords"`
}
