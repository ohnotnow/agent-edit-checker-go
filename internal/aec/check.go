package aec

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var migrationPathRe = regexp.MustCompile(`database/migrations/\d+_\d+_\d+_\d+_`)

// FileType classifies a path into a rule bucket: ".blade.php",
// "migration.php", or the lower-cased extension without its dot ("" when
// there is none).
func FileType(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".blade.php") {
		return ".blade.php"
	}
	if migrationPathRe.MatchString(path) {
		return "migration.php"
	}
	ext := filepath.Ext(filepath.Base(path))
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}

// CheckContent returns the messages of every enabled rule that fires on the
// new content of a Write or Edit, in rule order. Rules for every file ("*")
// run before the file type's own rules.
func CheckContent(rules []Rule, fileType, content, oldContent string) []string {
	var msgs []string
	for _, r := range rules {
		if r.Enabled && slices.Contains(r.Files, "*") && contentFires(r, content, oldContent) {
			msgs = append(msgs, r.Message)
		}
	}
	for _, r := range rules {
		if r.Enabled && slices.Contains(r.Files, fileType) && fileType != "*" && contentFires(r, content, oldContent) {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}

func contentFires(r Rule, content, oldContent string) bool {
	if r.MaxMatches == nil {
		return matches(r.re, content)
	}
	added := countMatches(r.re, content) - countMatches(r.re, oldContent)
	return added > *r.MaxMatches
}
