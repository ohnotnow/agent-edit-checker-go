# Technical Overview

Last updated: 2026-10-09

## What This Is

`aec` is a single Go binary that Claude Code runs as hooks: it blocks Write/Edit content and Bash commands that break regex rules, adds context to prompts, logs failed tool calls, and nudges the agent to re-read files and review its work. It replaces the PHP hook scripts in `ohnotnow/agent-edit-checker`.

## Stack

- Go 1.27 (module `github.com/ohnotnow/agent-edit-checker-go`)
- `github.com/dlclark/regexp2` - PHP/PCRE-compatible regexes (lookaheads etc.), 1s match timeout
- `github.com/BurntSushi/toml` - default rules and the user overlay
- `github.com/charmbracelet/bubbletea` + `lipgloss` - the `aec tui` toggler

## Directory Structure

```
cmd/aec/main.go          one line: os.Exit(aec.Run(...))
internal/aec/            everything else, one flat package
  rules.toml             default rules + [nudge] settings, embedded with go:embed
  testdata/fixtures/     JSON hook payloads per rule: edit/, bash/, prompt/
scripts/php-parity.sh    runs every fixture through Go and the old PHP hooks, reports disagreements
.github/workflows/release.yml   cross-compiles 6 targets on a v* tag (no test CI workflow)
```

## Core Concepts

```
rules.toml (embedded)  ──┐
                         ├─ Merge() ──> Merged{Rules, Signals, Nudge, Stale}
~/.config/aec/rules.toml ┘     (overlay.go)
  disabled = [...]             - disabled names switch off rules AND nudge signals
  [[rules]] name=...           - name of a default: override only the keys given
                               - new name with pattern+message+target: user rule
  [nudge] / [nudge.models]     - override individual thresholds
```

A **Rule** (`rules.go`) has exactly one kind of target:

| Kind | Key | Hook | Effect |
|------|-----|------|--------|
| file | `files = ["php", ".blade.php", "migration.php", "*"]` | `hook edit` | block (exit 2) |
| command | `command = '/regex/'` gates; `type = forbid\|require` | `hook bash` | block (exit 2) |
| prompt | `prompt = true` | `hook prompt` | print message to stdout (context), never blocks |

Patterns are PHP-style delimited strings (`/body/flags`, `#body#flags`, flags `i m s` only), compiled by `CompilePattern` in `pattern.go`. `max_matches` (file rules only) compares match counts in new vs old content, which is how `one-test-at-a-time` allows editing a file that already holds many tests.

A **Signal** (`signals.go`) is a nudge-hook trigger: `state-load`, `confidence-check`, `test-quality`. Signal names share the rule namespace, so rule names may not collide with them.

## The Five Hooks

Wired by `aec install` (`cmd_install.go`, `installHooks`):

| Event | Matcher | Command | Source | Output |
|-------|---------|---------|--------|--------|
| PreToolUse | `Write\|Edit` | `aec hook edit` | `hook.go`, `check.go` | stderr `❌ Blocked: msg`, exit 2 |
| PreToolUse | `Bash` | `aec hook bash` | `hook.go`, `command.go` | as above; every decision logged to `tool-use.log` |
| PostToolUseFailure | (all) | `aec hook tool-fails` | `toolfails.go` | appends to `tool-fails.log`, always exit 0 |
| UserPromptSubmit | (all) | `aec hook prompt` | `prompt.go` | matching messages on stdout |
| PostToolUse | `Write\|Edit\|Read\|Agent` | `aec hook nudge` | `nudge_hook.go`, `nudge.go`, `nudge_extras.go` | JSON `additionalContext` when a signal fires |

Fail-open everywhere: invalid JSON allows the call; a broken overlay is reported on stderr and the defaults are used (`effectiveRules`); the nudge hook swallows any error or panic.

## Nudge Hook Internals

State per session (plus `--<agent_id>` for subagents) in `~/.config/aec/state/<key>.json`, `flock`ed (`lock_unix.go`; no-op on Windows). `nudgeState` JSON field names match the PHP hook's.

- **Write/Edit** marks the path dirty and extends the edit streak. **Read** clears that path and resets the streak. Paths under `/tmp/`, `/private/`, `/var/folders/` are ignored.
- **state-load** fires when the dirty count reaches the threshold. Threshold is per model: `detectModel` scans the transcript backwards (up to 4 MiB) for the last `"model":"claude-..."`, then `ThresholdFor` picks the longest `[nudge.models]` key contained in the id. Subagents always use `dirty_threshold`.
- **confidence-check** fires at `confidence_streak` consecutive edits with no Read.
- **test-quality** counts distinct `*.php` files under `<laravel-root>/tests/` (root found by walking up to `artisan`). Fires every `test_files` new files, but only if `test_agent` exists in `~/.claude/agents/` or the project's `.claude/agents/`. An `Agent` call with that `subagent_type` resets the count.
- Each signal fires once then stays quiet until its count drops below threshold (hysteresis in `evaluate`).
- When anything fires, `aitClause` runs `ait --db <root>/.ait/ait.db list --status in_progress` (3s timeout) and appends the in-progress issues. `lookPath` handles `~/` entries in PATH.
- Firing also prunes state files older than 48h. Firings are logged to `state-nudge.log`.

## Files Written at Runtime

All under `~/.config/aec/` (or `$XDG_CONFIG_HOME/aec/`):

| File | Written by | Rotation |
|------|-----------|----------|
| `rules.toml` | `install` / `rules` / `tui` (starter is all comments) | n/a |
| `tool-use.log` | `hook bash`, every command, allowed or denied | none |
| `tool-fails.log` | `hook tool-fails` | none |
| `state-nudge.log` | `hook nudge` | none |
| `state/*.json` | `hook nudge` | pruned after 48h |

Log line format (`appendLog`): `RFC3339 | field | field`, CR/LF escaped.

## CLI Commands

Dispatch table in `app.go` (`commands`, with `hook` and `rules` as two-word groups).

| Command | File |
|---------|------|
| `rules list` / `show <name>` / `diff` | `cmd_rules.go`, TOML output via `emit.go` |
| `tui` | `tui.go`; writes only the `disabled` line via `overlay_write.go` (`setDisabled`, verifies read-back) |
| `install` | `cmd_install.go`; backs up settings, never touches non-aec hooks, reports leftover PHP hooks |
| `version`, `self-update` | `version.go`, `cmd_self_update.go`; GitHub releases, SHA256SUMS check, atomic swap, defers to Homebrew/`go install` |

Exit codes (`exit.go`): 0 ok, 1 internal, 2 blocked (hooks), 64 usage, 65 validation, 66 not found.

## Testing

- Standard `go test ./...`; tests live beside each file in `internal/aec`.
- `parity_test.go` drives every JSON fixture in `testdata/fixtures/` through the real hook path. Fixture fields: `rule`, `why`, `payload`, `expect` (`allow`/`deny` or `inject`/`quiet`), `messages_contain`, `skip_php`.
- Test seams are package vars swapped in tests: `now`, `userHomeDir`, `detectModel`, `runCommand`, `nudgeIgnoredPrefixes`, `workDir`; `useTempConfig(t)` points the config dir at a temp dir.
- `scripts/php-parity.sh <php-repo>` compares decisions against the PHP originals (needs PHP).

## Adding a Default Rule

1. Add a `[[rules]]` block to `internal/aec/rules.toml` (kebab-case name, not a signal name).
2. Add `<name>-allow.json` and `<name>-deny.json` fixtures (or `-inject`/`-quiet` for prompt rules).
3. `go test ./...` - `LoadDefaults` validation and the fixture suite both run.

## Local Development

```bash
go test ./...
go build -o aec ./cmd/aec
echo '{"tool_input":{"command":"npm install foo"}}' | ./aec hook bash; echo $?
```
