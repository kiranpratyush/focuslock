package judge

import "testing"

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
	v := Decide(answer(map[string]float64{"music": 0.6, "entertainment": 0.3, "work": 0.1}), 0.75, 0.9, false)
	if v.Cat != CatEntertainment {
		t.Fatalf("got %+v", v)
	}
}

func TestSureMusicIsMusic(t *testing.T) {
	v := Decide(answer(map[string]float64{"music": 0.95, "entertainment": 0.05}), 0.75, 0.9, false)
	if v.Cat != CatMusic {
		t.Fatalf("got %+v", v)
	}
}

func TestLearningStaysAllowed(t *testing.T) {
	v := Decide(answer(map[string]float64{"learning": 0.7, "entertainment": 0.2, "music": 0.1}), 0.75, 0.9, false)
	if v.Cat != CatAllow {
		t.Fatalf("got %+v", v)
	}
}

func TestOffTask(t *testing.T) {
	v := Decide(answer(map[string]float64{"off_task": 0.8, "on_task": 0.2}), 0.75, 0.9, false)
	if v.Cat != CatOffTask {
		t.Fatalf("got %+v", v)
	}
	v = Decide(answer(map[string]float64{"on_task": 0.6, "off_task": 0.4}), 0.75, 0.9, false)
	if v.Cat != CatAllow {
		t.Fatalf("unsure should allow, got %+v", v)
	}
	// LLM-style answer: single confidence, no full distribution.
	v = Decide(Answer{Choice: "off_task", Confidence: 0.85, Probabilities: map[string]float64{"off_task": 0.85}}, 0.75, 0.9, false)
	if v.Cat != CatOffTask {
		t.Fatalf("got %+v", v)
	}
}

// ---- task (intent) mode ----

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
		if got := Decide(answer(tc.probs), 0.75, 0.9, true).Cat; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
	// LLM-style answer with one confidence.
	v := Decide(Answer{Choice: "learning", Confidence: 0.6, Probabilities: map[string]float64{"learning": 0.6}}, 0.75, 0.9, true)
	if v.Cat != CatEntertainment {
		t.Errorf("LLM unsure learning on strict site: got %+v", v)
	}
}
