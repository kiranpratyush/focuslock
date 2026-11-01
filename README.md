# FocusLock v1

A Windows focus timer you can't casually stop. While a session runs it:

- shows a countdown bar across the top of the screen (an AppBar, so windows can't cover it)
- covers browser tabs that look like entertainment with a dark overlay; if you stay on that tab, it closes it with Ctrl+W after 2 minutes
- kills blocked apps (Steam, Epic, etc.) wherever they're running
- lets music play in the background, but covers a music tab/app that sits in front for more than 60 seconds
- reads the active tab's **real URL** from the address bar (UI Automation, the accessibility API; no browser extension, no admin)
- optionally takes a **task** ("fix pagination bug") and covers anything unrelated to it, browser or app
- uses a **judge** for what the rules don't cover: Jev, a small local model (Ollama, LM Studio), or any OpenAI-compatible API

The only ways out are the timer ending or restarting the PC.

## Build

Needs Go 1.24+ on Windows. One dependency: the GitHub Copilot SDK, for the `"copilot"` judge.

```
go build -ldflags "-s -w" -o focuslock.exe .
```

The exe isn't in the repo; build it as above (from WSL: `GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o focuslock.exe .`).

## Use

```
focuslock start                                  opens the start window (below)
focuslock start 50m "fix pagination bug in API"  same window, pre-filled
focuslock start 50m "task" --no-prompt           start without the window
focuslock status
focuslock config                                 prints path to focus.json
focuslock judge-check ["task"]                   try the judge on a few sample pages (and list Copilot models)
```

### The start window

`focuslock start` opens a small window that asks:
- **How long?** (`50m`, `1h30m`, `45`)
- **What are you working on?** More detail helps the judge.
- **Sites this session needs**, besides `focus.json`: `portal.azure.com, dev.azure.com/myorg, console.aws.amazon.com`
- **Apps this session needs**: `Postman, docker desktop, ssms`

These extras apply to **this session only**. Your answers are remembered (`last-session.json`) and filled in next time. General settings (judge, block lists, rationing) stay in `focus.json`.

Keys: Ctrl+Enter (or Enter in the duration or apps box) starts, Tab moves between boxes, Esc cancels.

## Rationing email and chat

Outlook and Teams are fine for work, but checking them every five minutes isn't. With `"rations"`, each gets **check windows**:
```json
"rations": [
  { "name": "Email", "match": ["outlook.exe", "olk.exe", "outlook.office.com", "mail.google.com"],
    "everyMinutes": 30, "forMinutes": 5 },
  { "name": "Teams", "match": ["ms-teams.exe", "teams.exe", "teams.microsoft.com"],
    "everyMinutes": 20, "forMinutes": 5, "exceptTitleKeywords": ["meeting", "call", "sharing", "presenting"] }
]
```
(These are the defaults.)
- **Your first visit opens a window.** Coming back during it is free.
- **Afterwards it's covered** until the next window: *"Next check at 14:30 (in 12 min). You've gone to it 9 times this session."* The next window opens on your next visit once `everyMinutes` have passed.
- **Need it now?** Type why and it opens for `forMinutes`. That uses one of your explanations; no judge is involved, so it also works with `"provider": "none"`. It doesn't move the regular schedule.
- **Teams meetings and calls** (titles with "meeting", "call"...) are never covered. Check that your meeting windows' titles match, and add words to `exceptTitleKeywords` if not.
- `match` takes exe names and domains, so Outlook on the web is rationed too.
- Remove an entry, or set `"rations": []`, to turn it off.

## Multiple monitors

Every tick, FocusLock also checks the **window in front on each of your other monitors**, not just the one you're typing in:
- A blocked window there gets its own overlay, so a video on the second screen no longer slips through.
- They're **covered, never closed**: keys only reach the focused window.
- Click the overlay to bring that window to your current screen, where you can explain it.
- Music there is fine (it's in the background).
- Email on a side screen is visible only during a check window.
- Blocked apps (Steam...) are killed wherever they are, as before.

Limits:
- Only the **front** window per monitor is checked. Two windows side by side on one monitor: the one behind isn't.
- The top bar is on the main monitor only.
- With different scaling per monitor (say 150% and 100%), overlays on the other monitor may be slightly off. Not tested yet.

## Choosing a judge

Set `"judge": { "provider": ... }` in `focus.json`:

| provider | What | Setup |
|---|---|---|
| `"jev"` (default) | TypeSafe Jev, fast and cheap, real probabilities | `setx TYPESAFE_API_KEY your-key`, new terminal |
| `"openai"` | Any OpenAI-compatible chat API | Fill the `"openai"` section (below) |
| `"copilot"` | GitHub Copilot, through the Copilot CLI you're signed in to. Meant for the work laptop | `copilot` on PATH and signed in; pick a model (below) |
| `"none"` | Rules only; nothing leaves the machine | — |

**Local model with Ollama** (nothing leaves the machine):
```
ollama pull qwen2.5:3b
```
```json
"judge":  { "provider": "openai", ... },
"openai": { "baseUrl": "http://localhost:11434/v1", "model": "qwen2.5:3b", "jsonMode": true }
```
LM Studio: `"baseUrl": "http://localhost:1234/v1"`; if it rejects `response_format`, set `"jsonMode": false`.
A hosted API (OpenAI, Groq, OpenRouter...): set its `baseUrl` and `model`, and put the key in the env var named by `apiKeyEnv`.

`focuslock start` tells you which judge is active, and warns if a local server isn't reachable.

Small models (1–3B) are fine at "is this fun?" and decent at "is this my task?". If they misjudge too often, try a 7B model or raise `judge.threshold`. LLMs report their own confidence, which is rougher than Jev's probabilities.

**Copilot** (work laptop): FocusLock starts the Copilot CLI once and asks each question in a fresh session with **all tools off and every permission refused**, so the agent only reads the prompt and answers.
```json
"judge":   { "provider": "copilot", ... },
"copilot": { "model": "gpt-5-mini", "cliPath": "" }
```
- `cliPath` empty means `copilot` on PATH.
- **Every new tab or app judged is one Copilot request.** Answers are cached for the session, but pick a cheap or included model. `focuslock judge-check` lists your models with their premium-request cost.
- If your org has turned the Copilot CLI off, the judge can't start, and FocusLock runs on rules only.

**Before a real session, run `focuslock judge-check`.** It shows which judge is active, asks it about four sample pages and prints the answers and timings. Then try `focuslock start 2` for a two-minute test.

## How a tab is judged

1. **URL rules** (browsers): `allowDomains`, then `musicDomains`, then `blockDomains`. `youtube.com` matches `m.youtube.com` too; `youtube.com/shorts` matches only that path. Browser pages like `chrome://settings` are allowed.
2. Process lists: `blockedProcesses` -> killed, `musicProcesses` -> music. Other non-browser apps are allowed.
3. Browser title keywords, in order: `allowTitleKeywords`, then `musicTitleKeywords`, then `blockTitleKeywords`.
4. **Strict sites** (`strictDomains`: youtube.com, x.com, twitter.com, reddit.com). These apply only when a judge is set:
   - A title that names only the site, like `YouTube - Brave`, `Home / X`, or a bare URL, is blocked without asking the judge. Browsers show these while a page loads, and on home or feed pages.
   - A page here is allowed only if the judge is at least `threshold` sure it's work or learning (or, with a task, on task). Unsure means **blocked** here, so a coin-flip "learning 0.55" on YouTube no longer passes.
   - While a new page on the same site is being judged, the overlay stays up instead of flickering off. It never closes a tab that hasn't been judged yet.
   - A placeholder title also has to settle first, so a video's brief `youtube.com/watch?v=... - Brave` title never flashes a block.
5. Anything else goes to the judge.
   - Without a task: work / learning / music / entertainment / social.
   - With a task: on_task / supporting / off_task / music / entertainment. Apps are judged too, except `alwaysAllowProcesses` (editors, terminals, Teams, Outlook...).
     - An app the judge isn't sure about is covered (`judge.strictApps`, on by default; set it to `false` to allow unsure apps). WhatsApp at "off_task 0.51" no longer slips through.
     - A page whose title is only the site's name ("Hello Interview") must be judged as surely on task, because there's nothing else to go on.
   - Browser pop-ups and bubbles (an "extension added" notice, a permission prompt) aren't judged. Installed web apps (YouTube as an app) still are.
   - It blocks only if the judge is at least `threshold` (0.75) sure. Unsure means allow (except on strict sites, above). Answers are cached, and things are allowed while the judge is still answering.
   - A title is judged only after it has stayed the same for `judge.settleMs` (1200 ms). Pages flip through placeholder titles while loading, and those used to get judged, giving answers that flipped a second later. Allow and block rules still apply instantly.
6. If the judge is down or unset: allowed.

The judge gets the **app**, the **window title**, the **domain + path** (for example `youtube.com/watch`) and your **task**. The `?query` part is never sent, since it can contain tokens or IDs. Set `"judge": {"sendUrl": false}` to send titles only. Every decision is written to the log.

**What gets blocked how:**
- Apps on `blockedProcesses`: killed.
- Browser tabs: overlay below the tab strip, then Ctrl+W after `closeTabAfterSeconds`.
- Other apps the judge flags (with a task): overlay only, title bar left free. They are **never killed or sent keys**, since they might hold unsaved work.

While you're typing in the address bar, FocusLock keeps using the last real URL, so half-typed text is never judged. If the URL can't be read (an unusual browser, for example), it falls back to title-only, as before.

## Explaining a block

When the **judge** blocks something (not your block lists), the block screen has a box: *Need this for your task? Say why.*
- Type a specific reason and press Enter. The judge reads it together with the page and your task. Accepted means:
  - On strict sites (YouTube, X...): **that page** is allowed for the rest of the session. One good video doesn't unlock YouTube.
  - Elsewhere: **that site** is allowed. For an app: **that app**.
  - The reason becomes a **note for the session**. Every later question includes it, so the judge learns more about your task as you go.
- Vague reasons ("research", "just a minute") are turned down.
- You get **3 tries per session** (`explain.maxPerSession`); each try counts, accepted or not. If the judge fails to answer, the try doesn't count.
- While you type, and while the judge is reading, the tab's close countdown stops.
- Block lists, Shorts, and home or feed pages can't be explained away.
- Accepted notes are saved in `state.json`, so a restarted worker keeps them. Every explanation is logged.
- Turn it off with `"explain": {"enabled": false}`.

The block screen also shows what was blocked, who decided and how sure, your task, the countdown, and the session's stats.

## Work laptop mode

Set `"judge": { "provider": "copilot" }` so titles only go to Copilot, which your company already approved. Or use `"none"` (or a local Ollama model), so nothing leaves the machine. Put your company's tools in `allowDomains` (`atlassian.net`, `dev.azure.com` and similar are already there) and `alwaysAllowProcesses`.

Note: security software may flag an unsigned exe that kills processes and sends keystrokes. Check with IT before installing.

## Files

Everything lives in `%LOCALAPPDATA%\FocusLock\`:

| File | What |
|---|---|
| `focus.json` | All lists and settings. Edit freely; applies to the next session |
| `focuslock.log` | What got blocked and why, Jev answers, restarts |
| `state.json` | Current session: end time, boot time, task, this session's extra sites and apps, accepted explanations |
| `last-session.json` | Your last answers in the start window, to pre-fill the next one |
| `focuslock-guard.exe`, `focuslock-worker.exe` | Copies of the exe that run the session |

## Why it's annoying to kill

`start` launches two processes under different names. The **worker** does the blocking and draws the windows. The **guard** has no window. Each one waits on the other and restarts it within about half a second if it dies. Killing `focuslock.exe` by name does nothing, and Task Manager is too slow to kill both.

**Emergency exit** (if something misbehaves): restart the PC, or run in one command:
```
taskkill /f /im focuslock-guard.exe /im focuslock-worker.exe
```

## Known v1 limits

- If the browser runs as admin, Windows won't let a normal process send it Ctrl+W.
- Fullscreen video or games can sit above the overlay. Blocked apps still get killed.
- Only the **active** tab of each browser window is checked.
- URL reading works best in Chrome, Edge, and Brave. Firefox falls back to the first text box it finds, which is usually the address bar.
- No hosts-file site blocking (that needs admin; v2 with the Windows Service).

## Code map

- `main_windows.go`: CLI, session start, guard/worker watchdog pair
- `ui_windows.go`: AppBar, overlay, and the 500ms monitor loop
- `classify.go`: URL/title/process rules, caching, verdicts (portable, tested)
- `judge.go`: what a judge is asked and answers, plus the judge registry (portable, tested)
- `judge_jev.go`, `judge_openai.go`, `judge_copilot.go`: one file per judge. Each registers itself with `registerJudge`; a new judge is one new file
- `judge_check.go`: `focuslock judge-check`
- `explain.go`: explaining blocks, session notes (portable, tested)
- `ration.go`: check windows for email and chat (portable, tested)
- `session_setup.go`, `setup_windows.go`: the start window, and this session's extra sites and apps
- `uia_windows.go`: reads the address bar via UI Automation (COM)
- `winapi_windows.go`: Win32 calls via `syscall`
- `config.go`, `state.go`: settings and session file
