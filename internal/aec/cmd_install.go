package aec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Seams for tests.
var (
	executablePath = os.Executable
	workDir        = os.Getwd
	now            = time.Now
)

// installHook is one hook entry the installer manages.
type installHook struct {
	matcher string
	sub     string // "edit" or "bash"
}

var installHooks = []installHook{
	{matcher: "Write|Edit", sub: "edit"},
	{matcher: "Bash", sub: "bash"},
}

// phpHooks are the predecessor scripts; the installer reports them and
// leaves them alone.
var phpHooks = []string{"check.php", "tool-use.php"}

const installEvent = "PreToolUse"

// install wires the hooks into a Claude Code settings file.
func install(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dryRun, yes bool
	var scope, settings string
	fs.BoolVar(&dryRun, "dry-run", false, "")
	fs.BoolVar(&yes, "yes", false, "")
	fs.BoolVar(&yes, "y", false, "")
	fs.StringVar(&scope, "scope", "user", "")
	fs.StringVar(&settings, "settings", "", "")
	if err := fs.Parse(args); err != nil {
		return usageErr("install: %v", err)
	}
	if fs.NArg() != 0 {
		return usageErr("install takes no arguments")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path, err := settingsPath(scope, settings, home)
	if err != nil {
		return err
	}
	exe, err := hookCommandPrefix(home)
	if err != nil {
		return err
	}
	overlay, err := OverlayPath()
	if err != nil {
		return err
	}

	doc, existed, err := readSettings(path)
	if err != nil {
		return err
	}
	plan, changes, err := planHooks(doc, exe, path)
	if err != nil {
		return err
	}
	plan = append(plan, phpNotes(doc)...)

	fmt.Fprintln(stdout, "aec installer")
	created := ""
	if !existed {
		created = " (will be created)"
	}
	fmt.Fprintf(stdout, "  settings: %s%s\n", path, created)
	fmt.Fprintf(stdout, "  rules:    %s\n\n", overlay)
	fmt.Fprintln(stdout, "Planned hook changes:")
	for _, line := range plan {
		fmt.Fprintf(stdout, "  %s\n", line)
	}
	fmt.Fprintln(stdout)

	if dryRun {
		fmt.Fprintln(stdout, "Dry run, no files were changed.")
		return nil
	}
	if changes == 0 {
		fmt.Fprintln(stdout, "Everything is already wired up. Nothing to do.")
		return finishInstall(overlay, stdout)
	}

	if yes {
		fmt.Fprintln(stdout, "Proceeding without confirmation (--yes).")
	} else {
		fmt.Fprintf(stdout, "Proceed? This will back up and update %s. [y/N] ", path)
		if !readYes(stdin) {
			fmt.Fprintln(stdout, "Aborted, nothing changed.")
			return nil
		}
	}

	if existed {
		backup := path + ".backup-" + now().Format("20060102-150405")
		if err := copyFile(path, backup); err != nil {
			return fmt.Errorf("could not write a backup to %s: %w", backup, err)
		}
		fmt.Fprintf(stdout, "Backed up existing settings to %s\n", backup)
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeSettings(path, doc); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated %s\n", path)
	return finishInstall(overlay, stdout)
}

func finishInstall(overlay string, stdout io.Writer) error {
	wrote, err := ensureOverlay(overlay)
	if err != nil {
		return err
	}
	if wrote {
		fmt.Fprintf(stdout, "Wrote starter overlay to %s\n", overlay)
	}
	fmt.Fprintln(stdout, "\nDone. Restart Claude Code (or start a new session) for the hooks to take effect.")
	return nil
}

// settingsPath picks the settings file from --scope, or --settings when
// given.
func settingsPath(scope, explicit, home string) (string, error) {
	if explicit != "" {
		if strings.HasPrefix(explicit, "~/") {
			return filepath.Join(home, explicit[2:]), nil
		}
		return explicit, nil
	}
	switch scope {
	case "user":
		return filepath.Join(home, ".claude", "settings.json"), nil
	case "project", "local":
		cwd, err := workDir()
		if err != nil {
			return "", err
		}
		name := "settings.json"
		if scope == "local" {
			name = "settings.local.json"
		}
		return filepath.Join(cwd, ".claude", name), nil
	}
	return "", usageErr("install: --scope must be user, project or local, not %q", scope)
}

// hookCommandPrefix is the path of the running binary, tilde-relative
// when it lies under the home directory.
func hookCommandPrefix(home string) (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if home != "" && strings.HasPrefix(exe, home+string(filepath.Separator)) {
		return "~" + exe[len(home):], nil
	}
	return exe, nil
}

// readSettings decodes the settings file. A missing file gives an empty
// document and existed == false.
func readSettings(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, true, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, true, validationErr("%s: not valid JSON (%v); leaving it untouched", path, err)
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, true, validationErr("%s: top level is not a JSON object; leaving it untouched", path)
	}
	return doc, true, nil
}

// planHooks adds or updates the aec hooks in doc and returns the plan
// lines and the number of changes made.
func planHooks(doc map[string]any, exe, path string) ([]string, int, error) {
	hooks, ok := doc["hooks"].(map[string]any)
	if doc["hooks"] != nil && !ok {
		return nil, 0, validationErr("%s: \"hooks\" is not a JSON object; leaving it untouched", path)
	}
	if hooks == nil {
		hooks = map[string]any{}
		doc["hooks"] = hooks
	}
	groups, ok := hooks[installEvent].([]any)
	if hooks[installEvent] != nil && !ok {
		return nil, 0, validationErr("%s: \"hooks.%s\" is not a JSON array; leaving it untouched", path, installEvent)
	}

	var plan []string
	changes := 0
	for _, h := range installHooks {
		desired := exe + " hook " + h.sub
		found := false
		var updates []string
		for _, g := range groups {
			group, _ := g.(map[string]any)
			entries, _ := group["hooks"].([]any)
			for _, e := range entries {
				entry, _ := e.(map[string]any)
				cmd, _ := entry["command"].(string)
				if !isAecHook(cmd, h.sub) {
					continue
				}
				found = true
				if cmd != desired {
					entry["command"] = desired
					updates = append(updates, "path -> "+desired)
				}
				if len(entries) == 1 && group["matcher"] != h.matcher {
					group["matcher"] = h.matcher
					updates = append(updates, "matcher -> "+h.matcher)
				}
			}
		}
		switch {
		case found && len(updates) > 0:
			plan = append(plan, fmt.Sprintf("[update] %s: hook %s: %s", installEvent, h.sub, strings.Join(updates, ", ")))
			changes++
		case found:
			plan = append(plan, fmt.Sprintf("[ok]     %s: hook %s: already present and correct", installEvent, h.sub))
		default:
			groups = append(groups, map[string]any{
				"matcher": h.matcher,
				"hooks":   []any{map[string]any{"type": "command", "command": desired}},
			})
			plan = append(plan, fmt.Sprintf("[add]    %s (matcher: %s) -> %s", installEvent, h.matcher, desired))
			changes++
		}
	}
	hooks[installEvent] = groups
	return plan, changes, nil
}

// isAecHook reports whether cmd is "<something>/aec hook <sub>".
func isAecHook(cmd, sub string) bool {
	prefix, ok := strings.CutSuffix(cmd, " hook "+sub)
	if !ok {
		return false
	}
	base := prefix[strings.LastIndexAny(prefix, `/\`)+1:]
	return strings.TrimSuffix(base, ".exe") == "aec"
}

// phpNotes lists the predecessor PHP hooks still present anywhere in doc.
func phpNotes(doc map[string]any) []string {
	var notes []string
	hooks, _ := doc["hooks"].(map[string]any)
	for event, v := range hooks {
		groups, _ := v.([]any)
		for _, g := range groups {
			group, _ := g.(map[string]any)
			entries, _ := group["hooks"].([]any)
			for _, e := range entries {
				entry, _ := e.(map[string]any)
				cmd, _ := entry["command"].(string)
				for _, php := range phpHooks {
					if strings.Contains(cmd, php) {
						notes = append(notes, fmt.Sprintf("[note]   %s: %s is still installed; it will run alongside aec. Remove it by hand if you no longer want it.", event, cmd))
					}
				}
			}
		}
	}
	return notes
}

func readYes(stdin io.Reader) bool {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// writeSettings encodes doc and moves it over path via a sibling temp
// file, so a half-written file never replaces the real one.
func writeSettings(path string, doc map[string]any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
