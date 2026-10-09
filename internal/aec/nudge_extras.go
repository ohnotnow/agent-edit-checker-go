package aec

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// detectModel returns the model id of the session whose transcript is at
// path, or "" when it cannot tell. Tests replace it.
var detectModel = modelFromTranscript

var transcriptModelRe = regexp.MustCompile(`"model":"(claude-[^"]*)"`)

// modelFromTranscript scans the transcript backwards in overlapping chunks
// for the last model id. It does not read a fixed tail because one
// screenshot Read leaves a base64 line of half a megabyte, which can hide
// every model line from a small window. It gives up after 4 MiB.
func modelFromTranscript(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	const chunk, overlap, maxScan = 256 << 10, 256, 4 << 20
	pos, scanned := info.Size(), int64(0)
	for pos > 0 && scanned < maxScan {
		from := max(0, pos-chunk)
		buf := make([]byte, min(pos+overlap, info.Size())-from)
		if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
			return ""
		}
		if m := transcriptModelRe.FindAllSubmatch(buf, -1); len(m) > 0 {
			return string(m[len(m)-1][1])
		}
		scanned += pos - from
		pos = from
	}
	return ""
}

// runCommand runs a program without a shell and returns its stdout. Tests
// replace it.
var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, lookPath(name), args...)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// lookPath finds name like exec.LookPath, then retries the PATH entries
// that start with "~/" with the home directory expanded: shells accept a
// literal tilde in PATH, but Go's lookup does not. It returns name when
// nothing is found, so the run fails as it would have.
func lookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, err := userHomeDir()
	if err != nil || home == "" {
		return name
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		rest, ok := strings.CutPrefix(dir, "~/")
		if !ok {
			continue
		}
		p := filepath.Join(home, rest, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return name
}

// aitClause names the in-progress ait issues of the project holding cwd,
// for re-anchoring on the goal, or returns "" when there is nothing
// trustworthy to say.
func aitClause(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	root, ok := findUp(cwd, filepath.Join(".ait", "ait.db"))
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := runCommand(ctx, "ait", "--db", filepath.Join(root, ".ait", "ait.db"), "list", "--status", "in_progress")
	if err != nil {
		return ""
	}
	var list struct {
		Issues []struct {
			ID    any `json:"id"`
			Title any `json:"title"`
		} `json:"issues"`
	}
	if json.Unmarshal(out, &list) != nil {
		return ""
	}
	var lines []string
	for _, issue := range list.Issues {
		id, _ := issue.ID.(string)
		if id == "" {
			continue
		}
		title, _ := issue.Title.(string)
		lines = append(lines, "- "+id+": "+title+" (re-read with: ait show "+id+")")
	}
	if len(lines) == 0 {
		return ""
	}
	noun := "issue"
	if len(lines) > 1 {
		noun = "issues"
	}
	return "\n\nAlso re-read your in-progress ait " + noun + " to re-anchor on the goal and acceptance criteria:\n" + strings.Join(lines, "\n")
}

// pruneState deletes state files untouched for over 48 hours, except keep.
func pruneState(dir, keep string) {
	old, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, p := range old {
		info, err := os.Stat(p)
		if err == nil && p != keep && now().Sub(info.ModTime()) > 48*time.Hour {
			_ = os.Remove(p)
		}
	}
}
