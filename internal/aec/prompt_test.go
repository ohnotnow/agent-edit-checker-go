package aec

import (
	"slices"
	"strings"
	"testing"
)

const promptRules = `
[[rules]]
name = "one"
prompt = true
pattern = '/\bdeploy\b/'
message = '''
deploy line one
deploy line two'''

[[rules]]
name = "two"
prompt = true
pattern = '/\bcats\b/i'
message = "cats"

[[rules]]
name = "not-prompt"
command = '/./s'
pattern = '/deploy/'
message = "command rule"
`

func TestCheckPrompt(t *testing.T) {
	rules := testRules(t, promptRules)
	if got := CheckPrompt(rules, "deploy the CATS"); !slices.Equal(got, []string{"deploy line one\ndeploy line two", "cats"}) {
		t.Errorf("got %q", got)
	}
	if got := CheckPrompt(rules, "nothing here"); len(got) != 0 {
		t.Errorf("got %q", got)
	}
	rules[0].Enabled = false
	if got := CheckPrompt(rules, "deploy cats"); !slices.Equal(got, []string{"cats"}) {
		t.Errorf("disabled rule fired: %q", got)
	}
}

func TestHookPrompt(t *testing.T) {
	useTempConfig(t)
	code, stdout, stderr := runOut(t, `{"prompt":"what do you recommend?"}`, "hook", "prompt")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	want := defaultRule(t, "question-pause").Message + "\n\n" + defaultRule(t, "grounded-recommendation").Message + "\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestHookPromptQuiet(t *testing.T) {
	for name, payload := range map[string]string{
		"no match":       `{"prompt":"carry on with the work."}`,
		"empty prompt":   `{"prompt":""}`,
		"missing prompt": `{}`,
		"invalid json":   "not json",
	} {
		t.Run(name, func(t *testing.T) {
			useTempConfig(t)
			code, stdout, stderr := runOut(t, payload, "hook", "prompt")
			if code != 0 || stdout != "" || stderr != "" {
				t.Errorf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestHookPromptDisabled(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, `disabled = ["question-pause"]`)
	code, stdout, _ := runOut(t, `{"prompt":"ready?"}`, "hook", "prompt")
	if code != 0 || stdout != "" {
		t.Errorf("code=%d stdout=%q", code, stdout)
	}
}

func TestHookPromptTakesNoArguments(t *testing.T) {
	useTempConfig(t)
	if code, _, _ := runOut(t, "{}", "hook", "prompt", "extra"); code != 64 {
		t.Errorf("code=%d, want 64", code)
	}
}

func TestDefaultPromptRules(t *testing.T) {
	checkRule(t, "question-pause", []string{"ready?"}, []string{"ready."})
	checkRule(t, "verify-claims", []string{"kk what is the limit", "is that right kk"}, []string{"backpack", "kkk"})
	checkRule(t, "grounded-recommendation",
		[]string{"What would you Recommend", "how should we do this", "which approach", "what's your take", "should I use x"},
		[]string{"recommended reading list", "carry on"})
	for _, name := range []string{"question-pause", "verify-claims", "grounded-recommendation"} {
		r := defaultRule(t, name)
		if !r.Prompt || strings.HasPrefix(r.Message, "\n") || strings.HasSuffix(r.Message, "\n") {
			t.Errorf("%s: prompt=%v message=%q", name, r.Prompt, r.Message)
		}
	}
	if lines := strings.Split(defaultRule(t, "question-pause").Message, "\n"); len(lines) != 4 {
		t.Errorf("question-pause should have 4 lines, got %d", len(lines))
	}
}
