package aec

import (
	"strings"
	"testing"
)

func mustCompile(t *testing.T, p string) func(string) bool {
	t.Helper()
	re, err := CompilePattern(p)
	if err != nil {
		t.Fatalf("CompilePattern(%q): %v", p, err)
	}
	return func(s string) bool {
		ok, err := re.MatchString(s)
		if err != nil {
			t.Fatalf("match %q: %v", s, err)
		}
		return ok
	}
}

func TestCompilePatternDelimiters(t *testing.T) {
	if !mustCompile(t, `/try\s*\{/i`)("TRY {") {
		t.Error("slash delimiter with i flag should match")
	}
	if !mustCompile(t, `#^\s*rm #`)("  rm -rf x") {
		t.Error("hash delimiter should match")
	}
	if mustCompile(t, `/a/b/`)("ab") || !mustCompile(t, `/a/b/`)("a/b") {
		t.Error("closing delimiter must be the last one")
	}
}

func TestCompilePatternRejects(t *testing.T) {
	for _, p := range []string{"", "abc", "/abc", "/abc/x", "/(/"} {
		if _, err := CompilePattern(p); err == nil {
			t.Errorf("CompilePattern(%q) should fail", p)
		}
	}
	_, err := CompilePattern("/abc/x")
	if err == nil || !strings.Contains(err.Error(), `'x'`) {
		t.Errorf("unknown flag error should name the flag, got %v", err)
	}
}

func TestCompilePatternLookahead(t *testing.T) {
	m := mustCompile(t, `/DB::(?!transaction\b)/i`)
	if !m("DB::table('x')") {
		t.Error("should match DB::table")
	}
	if m("DB::transaction(fn() => 1)") {
		t.Error("should not match DB::transaction")
	}
}

func TestCompilePatternMultilineFlag(t *testing.T) {
	input := "it('a');\ntest('b');"
	with, err := CompilePattern(`/^\s*(it|test)\s*\(/m`)
	if err != nil {
		t.Fatal(err)
	}
	without, err := CompilePattern(`/^\s*(it|test)\s*\(/`)
	if err != nil {
		t.Fatal(err)
	}
	if n := countMatches(t, with, input); n != 2 {
		t.Errorf("with m flag: want 2 matches, got %d", n)
	}
	if n := countMatches(t, without, input); n != 1 {
		t.Errorf("without m flag: want 1 match, got %d", n)
	}
}

func TestCompilePatternSinglelineFlag(t *testing.T) {
	if mustCompile(t, `/a.b/`)("a\nb") {
		t.Error("dot should not match newline without s")
	}
	if !mustCompile(t, `/a.b/s`)("a\nb") {
		t.Error("dot should match newline with s")
	}
}

func TestCompilePatternCodePoints(t *testing.T) {
	m := mustCompile(t, `/[\x{2012}-\x{2015}]/`)
	if !m("a " + string(rune(0x2014)) + " b") {
		t.Error("should match an em dash written as a code point")
	}
	if m("a - b") {
		t.Error("should not match a plain hyphen")
	}
}

func TestCompilePatternSetsTimeout(t *testing.T) {
	re, err := CompilePattern(`/x/`)
	if err != nil {
		t.Fatal(err)
	}
	if re.MatchTimeout != matchTimeout {
		t.Errorf("MatchTimeout = %v, want %v", re.MatchTimeout, matchTimeout)
	}
}
