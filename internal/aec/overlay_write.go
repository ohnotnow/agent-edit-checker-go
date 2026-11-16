package aec

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// setDisabled returns src with its top-level disabled line replaced by one
// listing names, or with that line inserted at the top when src has none.
// Every other byte of src is unchanged.
func setDisabled(src string, names []string) (string, error) {
	lines := strings.Split(src, "\n")
	start, end := -1, -1
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, "[") {
			break
		}
		if !isDisabledKey(t) {
			continue
		}
		start = i
		end = arrayEnd(lines, i)
		if end < 0 {
			return "", fmt.Errorf("disabled list starting on line %d is never closed", i+1)
		}
		break
	}
	line := disabledLine(names)
	if start < 0 {
		return line + "\n\n" + src, nil
	}
	out := append(slices.Clone(lines[:start]), line)
	out = append(out, lines[end+1:]...)
	return strings.Join(out, "\n"), nil
}

func isDisabledKey(trimmed string) bool {
	rest, ok := strings.CutPrefix(trimmed, "disabled")
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(rest, " \t"), "=")
}

// arrayEnd returns the index of the line on which the array value that
// starts on line i closes, or -1. Brackets inside strings and comments do
// not count.
func arrayEnd(lines []string, i int) int {
	depth := 0
	seen := false
	pos := strings.Index(lines[i], "=") + 1
	for ; i < len(lines); i++ {
		l := lines[i]
		var quote byte
		for j := pos; j < len(l); j++ {
			c := l[j]
			switch {
			case quote != 0:
				if c == quote {
					quote = 0
				}
			case c == '"' || c == '\'':
				quote = c
			case c == '#':
				j = len(l)
			case c == '[':
				depth++
				seen = true
			case c == ']':
				depth--
				if seen && depth == 0 {
					return i
				}
			}
		}
		pos = 0
	}
	return -1
}

func disabledLine(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = basicString(n)
	}
	return "disabled = [" + strings.Join(quoted, ", ") + "]"
}

// writeFile is os.WriteFile; tests replace it to corrupt a write.
var writeFile = os.WriteFile

// WriteDisabled rewrites the overlay at path so its disabled list is names,
// writes atomically, then decodes the file again and checks the list
// matches. A missing file is created from the starter overlay first.
func WriteDisabled(path string, names []string) error {
	if _, err := ensureOverlay(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := setDisabled(string(data), names)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := replaceFile(path, out); err != nil {
		return err
	}
	ov, err := LoadOverlay(path)
	if err != nil {
		return fmt.Errorf("verify after write: %w", err)
	}
	if !sameSet(ov.Disabled, names) {
		return fmt.Errorf("%s: wrote disabled = %v but read back %v", path, names, ov.Disabled)
	}
	return nil
}

// replaceFile writes data to a sibling temp file and renames it over path.
func replaceFile(path, data string) error {
	tmp := path + ".tmp"
	if err := writeFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// publishNote goes above a published rule. %s is the rule name.
const publishNote = `# Copied from the default %s rule. Keep the name, change what you
# like, and delete any key you leave alone so it keeps following the
# default when aec is updated.
`

// PublishRule appends r to the overlay at path as a complete [[rules]]
// block under publishNote, so it can be edited there. It refuses when the
// overlay already has a block with r's name, since a duplicate name makes
// the whole overlay invalid. A missing file is created from the starter
// overlay first. After writing it decodes the file again and checks the
// block reads back with r's pattern and message.
func PublishRule(path string, r Rule) error {
	if _, err := ensureOverlay(path); err != nil {
		return err
	}
	ov, err := LoadOverlay(path)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(ov.Rules, func(o overlayRule) bool { return o.Name == r.Name }) {
		return fmt.Errorf("%s is already in %s", r.Name, path)
	}
	var block strings.Builder
	if err := emitRule(&block, r); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := strings.TrimRight(string(data), "\n")
	if src != "" {
		src += "\n\n"
	}
	out := src + fmt.Sprintf(publishNote, r.Name) + strings.TrimRight(block.String(), "\n") + "\n"
	if err := replaceFile(path, out); err != nil {
		return err
	}
	ov, err = LoadOverlay(path)
	if err != nil {
		return fmt.Errorf("verify after write: %w", err)
	}
	i := slices.IndexFunc(ov.Rules, func(o overlayRule) bool { return o.Name == r.Name })
	if i < 0 || ov.Rules[i].Pattern == nil || *ov.Rules[i].Pattern != r.Pattern ||
		ov.Rules[i].Message == nil || *ov.Rules[i].Message != r.Message {
		return fmt.Errorf("%s: wrote rule %s but it did not read back the same", path, r.Name)
	}
	return nil
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}
