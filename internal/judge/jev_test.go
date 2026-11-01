package judge

import "testing"

func TestNoJudgeWhenJevDisabled(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "k")
	cfg := testConfig()
	cfg.Jev.Enabled = false // an older work-laptop config
	if j := New(cfg); j != nil {
		t.Fatalf("judge should be off, got %q", j.Name())
	}
}
