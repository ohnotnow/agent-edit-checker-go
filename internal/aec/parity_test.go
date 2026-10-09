package aec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fixture struct {
	Rule            string          `json:"rule"`
	Why             string          `json:"why"`
	Payload         json.RawMessage `json:"payload"`
	Expect          string          `json:"expect"`
	MessagesContain []string        `json:"messages_contain"`
	SkipPHP         string          `json:"skip_php"`
}

// fixtureExpects lists the expect values each fixture kind accepts. Prompt
// fixtures say whether a rule's message is injected, since prompt rules
// never deny.
var fixtureExpects = map[string][]string{
	"edit":   {"allow", "deny"},
	"bash":   {"allow", "deny"},
	"prompt": {"inject", "quiet"},
}

func loadFixtures(t *testing.T, kind string) map[string]fixture {
	t.Helper()
	dir := filepath.Join("testdata", "fixtures", kind)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]fixture, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var f fixture
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if want := fixtureExpects[kind]; !slices.Contains(want, f.Expect) {
			t.Fatalf("%s: expect must be one of %v, got %q", e.Name(), want, f.Expect)
		}
		out[e.Name()] = f
	}
	return out
}

func TestFixtures(t *testing.T) {
	useTempConfig(t)
	for _, kind := range []string{"edit", "bash"} {
		for name, f := range loadFixtures(t, kind) {
			t.Run(kind+"/"+name, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				code := Run([]string{"hook", kind}, bytes.NewReader(f.Payload), &stdout, &stderr)
				want := 0
				if f.Expect == "deny" {
					want = 2
				}
				if code != want {
					t.Errorf("exit %d, want %d (%s)\nstderr: %s", code, want, f.Why, stderr.String())
				}
				for _, s := range f.MessagesContain {
					if !strings.Contains(stderr.String(), s) {
						t.Errorf("stderr lacks %q:\n%s", s, stderr.String())
					}
				}
			})
		}
	}
}

func TestPromptFixtures(t *testing.T) {
	useTempConfig(t)
	for name, f := range loadFixtures(t, "prompt") {
		t.Run("prompt/"+name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"hook", "prompt"}, bytes.NewReader(f.Payload), &stdout, &stderr)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit %d stderr %q", code, stderr.String())
			}
			injected := strings.Contains(stdout.String(), defaultRule(t, f.Rule).Message)
			if injected != (f.Expect == "inject") {
				t.Errorf("injected=%v, want %s (%s)\nstdout: %s", injected, f.Expect, f.Why, stdout.String())
			}
		})
	}
}

func TestEveryDefaultRuleHasFiringFixture(t *testing.T) {
	covered := map[string]bool{}
	for _, kind := range []string{"edit", "bash", "prompt"} {
		for _, f := range loadFixtures(t, kind) {
			if f.Expect == "deny" || f.Expect == "inject" {
				covered[f.Rule] = true
			}
		}
	}
	rules, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if !covered[r.Name] {
			t.Errorf("no deny or inject fixture for %s", r.Name)
		}
	}
}
