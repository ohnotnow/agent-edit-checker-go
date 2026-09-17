package aec

import (
	"slices"
	"testing"
)

func TestFileType(t *testing.T) {
	cases := map[string]string{
		"app/User.php":                "php",
		"resources/views/x.blade.php": ".blade.php",
		"RESOURCES/X.BLADE.PHP":       ".blade.php",
		"database/migrations/2024_01_01_000000_create_users.php": "migration.php",
		"database/migrations/helper.php":                         "php",
		"notes.md":                                               "md",
		"Makefile":                                               "",
		".env":                                                   "env",
		"foo.tar.gz":                                             "gz",
		"dir.with.dots/README":                                   "",
	}
	for path, want := range cases {
		if got := FileType(path); got != want {
			t.Errorf("FileType(%q) = %q, want %q", path, got, want)
		}
	}
}

// testRules parses TOML and marks every rule enabled, as LoadDefaults does.
func testRules(t *testing.T, src string) []Rule {
	t.Helper()
	rules, err := parseRules([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

const contentRules = `
[[rules]]
name = "php-only"
files = ["php"]
pattern = '/foo/'
message = "php foo"

[[rules]]
name = "everywhere"
files = ["*"]
pattern = '/foo/'
message = "star foo"

[[rules]]
name = "command-only"
command = '/./s'
pattern = '/foo/'
message = "command foo"

[[rules]]
name = "one-test"
files = ["php"]
pattern = '/^\s*it\s*\(/m'
max_matches = 1
message = "one test"
`

func TestCheckContent(t *testing.T) {
	rules := testRules(t, contentRules)
	check := func(fileType, content, old string) []string {
		t.Helper()
		return CheckContent(rules, fileType, content, old)
	}
	if got := check("md", "foo", ""); !slices.Equal(got, []string{"star foo"}) {
		t.Errorf("md: got %v", got)
	}
	if got := check("", "foo", ""); !slices.Equal(got, []string{"star foo"}) {
		t.Errorf("no extension: got %v", got)
	}
	if got := check("php", "foo", ""); !slices.Equal(got, []string{"star foo", "php foo"}) {
		t.Errorf("star rules should come first: got %v", got)
	}
	if got := check("php", "bar", ""); len(got) != 0 {
		t.Errorf("clean content: got %v", got)
	}
	if got := check("php", "foo", ""); slices.Contains(got, "command foo") {
		t.Errorf("command-only rule applied to content: got %v", got)
	}
}

func TestCheckContentMaxMatches(t *testing.T) {
	rules := testRules(t, contentRules)
	two := "it('a');\nit('b');"
	one := "it('a');"
	cases := []struct {
		label, content, old string
		fires               bool
	}{
		{"write two", two, "", true},
		{"write one", one, "", false},
		{"edit one to two", two, one, false},
		{"edit zero to two", two, "", true},
	}
	for _, c := range cases {
		got := CheckContent(rules, "php", c.content, c.old)
		if slices.Contains(got, "one test") != c.fires {
			t.Errorf("%s: fires=%v, want %v", c.label, !c.fires, c.fires)
		}
	}
}

func TestCheckContentSkipsDisabled(t *testing.T) {
	rules := testRules(t, contentRules)
	rules[1].Enabled = false
	if got := CheckContent(rules, "php", "foo", ""); !slices.Equal(got, []string{"php foo"}) {
		t.Errorf("got %v", got)
	}
}
