package aec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
		if f.Expect != "allow" && f.Expect != "deny" {
			t.Fatalf("%s: expect must be allow or deny, got %q", e.Name(), f.Expect)
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

func TestEveryDefaultRuleHasDenyFixture(t *testing.T) {
	covered := map[string]bool{}
	for _, kind := range []string{"edit", "bash"} {
		for _, f := range loadFixtures(t, kind) {
			if f.Expect == "deny" {
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
			t.Errorf("no deny fixture for %s", r.Name)
		}
	}
}
