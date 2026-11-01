package judge

// What a judge's answer means for the window: allow it, cover it, or give
// it the music grace period. The classifier (package classify) returns the
// same Verdict for its own rules, so the whole app speaks one language.

type Category string

const (
	CatAllow         Category = "allow"
	CatMusic         Category = "music"
	CatEntertainment Category = "entertainment"
	CatOffTask       Category = "off_task" // not fun, just not this task
	CatPending       Category = "pending"  // waiting for the judge; treated as allow
)

type Verdict struct {
	Cat    Category
	Source string // "rule", "url", "placeholder", "jev", "llm", "fallback"
	Prob   float64
	// Hold (only with CatPending): this is a strict site still being judged,
	// so an overlay already covering it should stay up instead of flickering off.
	Hold bool
}

// Decide turns a judge's answer into a verdict.
//   - "music" counts as music only when the judge is very sure (musicThreshold),
//     because a music video you're watching looks like music from its title.
//   - If entertainment + social + music together reach threshold: entertainment.
//     (A song-or-video toss-up gets blocked.)
//   - With a task: if those plus off_task reach threshold, it's off-task.
//   - Anything else (on task, supporting, work/learning, or unsure) is allowed,
//     except on strict sites: there it's allowed only if work + learning (or
//     on_task + supporting) reach threshold. Unsure on YouTube means blocked.
func Decide(a Answer, threshold, musicThreshold float64, strict bool) Verdict {
	if musicThreshold < threshold {
		musicThreshold = threshold
	}
	pr := a.Probabilities
	p := pr[a.Choice]
	if p == 0 {
		p = a.Confidence
	}
	fun := pr["entertainment"] + pr["social"] + pr["music"]
	off := pr["off_task"]
	if len(pr) == 0 {
		switch a.Choice {
		case "entertainment", "social", "music":
			fun = p
		case "off_task":
			off = p
		}
	}
	switch {
	case a.Choice == "music" && p >= musicThreshold:
		return Verdict{Cat: CatMusic, Prob: p}
	case fun >= threshold:
		return Verdict{Cat: CatEntertainment, Prob: fun}
	case fun+off >= threshold:
		return Verdict{Cat: CatOffTask, Prob: fun + off}
	}
	if strict {
		ok := pr["work"] + pr["learning"] + pr["on_task"] + pr["supporting"]
		if len(pr) == 0 {
			switch a.Choice {
			case "work", "learning", "on_task", "supporting":
				ok = p
			}
		}
		if ok < threshold {
			if off > fun {
				return Verdict{Cat: CatOffTask, Prob: 1 - ok}
			}
			return Verdict{Cat: CatEntertainment, Prob: 1 - ok}
		}
		return Verdict{Cat: CatAllow, Prob: ok}
	}
	return Verdict{Cat: CatAllow, Prob: p}
}
