package aec

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeOverlay(t *testing.T, src string) *overlayFile {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ov, err := LoadOverlay(path)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, src)
	}
	return ov
}

func TestStarterOverlayIsAllComments(t *testing.T) {
	ov := decodeOverlay(t, starterOverlay)
	if len(ov.Disabled) != 0 || len(ov.Rules) != 0 || len(ov.Undecoded) != 0 {
		t.Errorf("starter should decode to nothing, got %+v", ov)
	}
	defaults, _ := LoadDefaults()
	merged, err := Merge(defaults, ov)
	if err != nil || len(merged.Stale) != 0 || len(merged.Rules) != len(defaults) {
		t.Errorf("merge of starter changed things: %v %+v", err, merged.Stale)
	}
	for _, l := range strings.Split(starterOverlay, "\n") {
		if len(l) > 79 {
			t.Errorf("line over 79 columns: %q", l)
		}
		if l != "" && !strings.HasPrefix(l, "#") {
			t.Errorf("uncommented line: %q", l)
		}
	}
}

func TestStarterExamplesAreValid(t *testing.T) {
	ov := decodeOverlay(t, starterExamples)
	if len(ov.Undecoded) != 0 {
		t.Errorf("examples have unknown keys: %v", ov.Undecoded)
	}
	if strings.Join(ov.Disabled, ",") != "no-try-catch,no-throw" {
		t.Errorf("disabled = %v", ov.Disabled)
	}
	defaults, _ := LoadDefaults()
	merged, err := Merge(defaults, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Stale) != 0 {
		t.Errorf("stale: %v", merged.Stale)
	}
	if r := ruleByName(t, merged.Rules, "no-try-catch"); r.Origin != OriginOverridden || r.Enabled {
		t.Errorf("no-try-catch: origin=%v enabled=%v", r.Origin, r.Enabled)
	}
	if r := ruleByName(t, merged.Rules, "no-console-log"); r.Origin != OriginUser {
		t.Errorf("no-console-log: origin=%v", r.Origin)
	}
}

func TestLoadForCLIWritesStarterOnce(t *testing.T) {
	dir := useTempConfig(t)
	var stderr bytes.Buffer
	merged, ov, err := loadForCLI(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "aec", "rules.toml")
	if !strings.Contains(stderr.String(), "aec: wrote starter overlay to "+path) {
		t.Errorf("stderr=%q", stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != starterOverlay {
		t.Errorf("starter not written: %v", err)
	}
	defaults, _ := LoadDefaults()
	if len(merged.Rules) != len(defaults) || len(ov.Rules) != 0 {
		t.Errorf("rules changed: %d merged, %d overlay rules", len(merged.Rules), len(ov.Rules))
	}
	stderr.Reset()
	if _, _, err := loadForCLI(&stderr); err != nil || stderr.Len() != 0 {
		t.Errorf("second call: err=%v stderr=%q", err, stderr.String())
	}
}

func TestLoadForCLIWarnings(t *testing.T) {
	cases := []struct {
		label, src, want string
	}{
		{"stale", `disabled = ["no-such-rule"]`, `aec: overlay: "no-such-rule" is not a default rule and is not a complete rule; ignored`},
		{"unknown key", "[[rules]]\nname = \"no-throw\"\nenabled = false\n", `aec: overlay: unknown key "rules.enabled"; ignored`},
		{"top-level typo", "disabld = []\n", `aec: overlay: unknown key "disabld"; ignored`},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			dir := useTempConfig(t)
			writeOverlay(t, dir, c.src)
			var stderr bytes.Buffer
			merged, _, err := loadForCLI(&stderr)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stderr.String(), c.want) {
				t.Errorf("stderr=%q want %q", stderr.String(), c.want)
			}
			defaults, _ := LoadDefaults()
			if len(merged.Rules) != len(defaults) {
				t.Errorf("defaults changed")
			}
		})
	}
}

func TestLoadForCLIBrokenOverlay(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "disabled = [\n")
	_, _, err := loadForCLI(&bytes.Buffer{})
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != exitValidation || !strings.Contains(ce.msg, filepath.Join(dir, "aec", "rules.toml")) {
		t.Errorf("err=%v", err)
	}
}

func TestHooksNeverWriteStarter(t *testing.T) {
	dir := useTempConfig(t)
	run(t, `{"tool_input":{"command":"ls"}}`, "hook", "bash")
	run(t, tryPHP, "hook", "edit")
	if _, err := os.Stat(filepath.Join(dir, "aec", "rules.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hook wrote an overlay: %v", err)
	}
}
