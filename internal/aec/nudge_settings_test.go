package aec

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefaultNudgeSettings(t *testing.T) {
	s, err := LoadDefaultNudge()
	if err != nil {
		t.Fatal(err)
	}
	want := NudgeSettings{
		DirtyThreshold: 5, ConfidenceStreak: 12, TestFiles: 8, TestAgent: "test-quality-checker",
		Models: map[string]int{"haiku": 5, "opus": 7, "fable": 10, "mythos": 10},
	}
	if s.DirtyThreshold != want.DirtyThreshold || s.ConfidenceStreak != want.ConfidenceStreak ||
		s.TestFiles != want.TestFiles || s.TestAgent != want.TestAgent || !maps.Equal(s.Models, want.Models) {
		t.Errorf("got %+v", s)
	}
}

func TestThresholdFor(t *testing.T) {
	s, _ := LoadDefaultNudge()
	cases := map[string]int{"claude-opus-5-5": 7, "claude-haiku-4-5": 5, "claude-fable-5-1": 10, "something-else": 5, "": 5}
	for model, want := range cases {
		if got := s.ThresholdFor(model); got != want {
			t.Errorf("ThresholdFor(%q) = %d, want %d", model, got, want)
		}
	}
	s.Models["opus-5-5"] = 9
	if got := s.ThresholdFor("claude-opus-5-5"); got != 9 {
		t.Errorf("longest match should win, got %d", got)
	}
	if got := s.ThresholdFor("claude-opus-4-1"); got != 7 {
		t.Errorf("shorter key should still apply elsewhere, got %d", got)
	}
	tie := NudgeSettings{DirtyThreshold: 5, Models: map[string]int{"ab": 2, "bc": 3}}
	if got := tie.ThresholdFor("abc"); got != 2 {
		t.Errorf("equal-length tie should go to the lexically smaller key, got %d", got)
	}
}

func TestMergeNudgeDefaults(t *testing.T) {
	defaults, _ := LoadDefaultNudge()
	for _, src := range []string{"", "disabled = []\n"} {
		m := mustMerge(t, src)
		if m.Nudge.ConfidenceStreak != defaults.ConfidenceStreak || !maps.Equal(m.Nudge.Models, defaults.Models) || len(m.NudgeChanged) != 0 {
			t.Errorf("%q: got %+v changed=%v", src, m.Nudge, m.NudgeChanged)
		}
	}
	rules, _ := LoadDefaults()
	m, err := Merge(rules, nil)
	if err != nil || m.Nudge.DirtyThreshold != 5 {
		t.Errorf("nil overlay: %v %+v", err, m.Nudge)
	}
}

func TestMergeNudgeOverrides(t *testing.T) {
	m := mustMerge(t, "[nudge]\nconfidence_streak = 20\ndirty_threshold = 5\n\n[nudge.models]\nopus = 9\n\"opus-5-5\" = 11\n")
	if m.Nudge.ConfidenceStreak != 20 || m.Nudge.TestFiles != 8 || m.Nudge.Models["opus"] != 9 ||
		m.Nudge.Models["opus-5-5"] != 11 || m.Nudge.Models["haiku"] != 5 {
		t.Errorf("got %+v", m.Nudge)
	}
	want := []string{"dirty_threshold", "confidence_streak", "models.opus", "models.opus-5-5"}
	if !slices.Equal(m.NudgeChanged, want) {
		t.Errorf("changed = %v, want %v", m.NudgeChanged, want)
	}
	again, _ := LoadDefaultNudge()
	if again.Models["opus"] != 7 {
		t.Errorf("merge modified the defaults: %v", again.Models)
	}
}

func TestMergeNudgeErrors(t *testing.T) {
	cases := map[string]string{
		"[nudge]\nconfidence_streak = 0\n": "nudge: confidence_streak must be at least 1",
		"[nudge]\ndirty_threshold = -1\n":  "nudge: dirty_threshold must be at least 1",
		"[nudge]\ntest_files = 0\n":        "nudge: test_files must be at least 1",
		"[nudge]\ntest_agent = \"\"\n":     "nudge: test_agent is empty",
		"[nudge.models]\nopus = 0\n":       "nudge: models.opus must be at least 1",
		"[nudge.models]\n\"\" = 3\n":       "nudge: model key is empty",
	}
	for src, want := range cases {
		_, err := mergeTOML(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err=%v, want %q", src, err, want)
		}
	}
}

func TestNudgeUnknownKeyWarns(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "[nudge]\nconfidense_streak = 3\n")
	code, _, stderr := runOut(t, "", "rules", "list")
	if code != 0 || !strings.Contains(stderr, `unknown key "nudge.confidense_streak"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestRulesListShowsNudge(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "[nudge]\nconfidence_streak = 20\n\n[nudge.models]\n\"opus-5-5\" = 9\n")
	_, stdout, _ := runOut(t, "", "rules", "list")
	for _, s := range []string{"[nudge]\n", "confidence_streak = 20 # overridden\n", "dirty_threshold = 5\n",
		"test_agent = \"test-quality-checker\"\n", "[nudge.models]\n", "opus = 7\n", "opus-5-5 = 9 # overridden\n"} {
		if !strings.Contains(stdout, s) {
			t.Errorf("missing %q in:\n%s", s, stdout)
		}
	}
	var f rulesFile
	if _, err := toml.Decode(stdout, &f); err != nil {
		t.Fatalf("rules list output does not decode: %v", err)
	}
	if f.Nudge.ConfidenceStreak != 20 || f.Nudge.Models["opus-5-5"] != 9 || f.Nudge.Models["fable"] != 10 {
		t.Errorf("round trip: %+v", f.Nudge)
	}
}

func TestRulesDiffNudge(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "[nudge]\nconfidence_streak = 20\n\n[nudge.models]\n\"opus-5-5\" = 9\n")
	_, stdout, _ := runOut(t, "", "rules", "diff")
	want := "nudge\n" +
		"  confidence_streak\n    default:  12\n    override: 20\n" +
		"  models.opus-5-5\n    default:  \n    override: 9\n\n"
	if stdout != want {
		t.Errorf("got:\n%q\nwant:\n%q", stdout, want)
	}
}
