package aec

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetDisabled(t *testing.T) {
	names := []string{"a", "b"}
	cases := []struct {
		label, src, want string
	}{
		{"no line", "# hi\n[[rules]]\nname = \"x\"\n", "disabled = [\"a\", \"b\"]\n\n# hi\n[[rules]]\nname = \"x\"\n"},
		{"single line", "# hi\ndisabled = [\"z\"]\n[[rules]]\n", "# hi\ndisabled = [\"a\", \"b\"]\n[[rules]]\n"},
		{"multi line", "disabled = [\n  \"z\",\n  \"y\",\n]\n# after\n", "disabled = [\"a\", \"b\"]\n# after\n"},
		{"trailing comment", "disabled = [\"z\"] # was [old]\nx = 1\n", "disabled = [\"a\", \"b\"]\nx = 1\n"},
		{"bracket in string", "disabled = [\"z]\", \"[y\"]\nx = 1\n", "disabled = [\"a\", \"b\"]\nx = 1\n"},
		{"indented key", "  disabled=[]\n", "disabled = [\"a\", \"b\"]\n"},
		{"inside rules block", "[[rules]]\nname = \"r\"\ndisabled = [\"z\"]\n", "disabled = [\"a\", \"b\"]\n\n[[rules]]\nname = \"r\"\ndisabled = [\"z\"]\n"},
		{"commented line ignored", "# disabled = [\"z\"]\n", "disabled = [\"a\", \"b\"]\n\n# disabled = [\"z\"]\n"},
		{"empty file", "", "disabled = [\"a\", \"b\"]\n\n"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got, err := setDisabled(c.src, names)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
	got, _ := setDisabled("disabled = [\"z\"]\n", nil)
	if got != "disabled = []\n" {
		t.Errorf("empty names: %q", got)
	}
	if _, err := setDisabled("disabled = [\n\"z\",\n", names); err == nil {
		t.Error("unclosed list should fail")
	}
}

func TestWriteDisabledCreatesStarter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aec", "rules.toml")
	if err := WriteDisabled(path, []string{"no-throw"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "disabled = [\"no-throw\"]\n\n"+starterHeader) {
		t.Errorf("unexpected file:\n%s", data)
	}
	ov, err := LoadOverlay(path)
	if err != nil || !slices.Equal(ov.Disabled, []string{"no-throw"}) {
		t.Errorf("read back %v %v", ov, err)
	}
}

func TestWriteDisabledKeepsEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.toml")
	src := "# my overlay\ndisabled = [\"no-spy\"]\n\n# override\n[[rules]]\nname = \"no-throw\"\nmessage = \"mine\" # trailing\n"
	os.WriteFile(path, []byte(src), 0o644)
	if err := WriteDisabled(path, []string{"no-throw", "no-spy"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	strip := func(s string) string {
		var keep []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.HasPrefix(l, "disabled") {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	if strip(string(data)) != strip(src) {
		t.Errorf("other lines changed:\n%s", data)
	}
	if !strings.Contains(string(data), "disabled = [\"no-throw\", \"no-spy\"]\n") {
		t.Errorf("disabled line wrong:\n%s", data)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
}

func TestWriteDisabledVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.toml")
	os.WriteFile(path, []byte("disabled = []\n"), 0o644)
	old := writeFile
	writeFile = func(name string, _ []byte, perm os.FileMode) error {
		return os.WriteFile(name, []byte("disabled = [\"other\"]\n"), perm)
	}
	t.Cleanup(func() { writeFile = old })
	err := WriteDisabled(path, []string{"no-throw"})
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "other") {
		t.Errorf("err=%v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "disabled = [\"other\"]\n" {
		t.Errorf("file should be left as written for inspection: %q", data)
	}
}
