# agent-edit-checker-go

`aec` is a single Go binary that screens Claude Code tool calls before they run. It checks Write and Edit content against per-file-type regex rules, and Bash commands against require/forbid rules, and blocks the call with a message when a rule fires. It is the successor to the PHP hook scripts in [agent-edit-checker](https://github.com/ohnotnow/agent-edit-checker).

Why a rewrite: the PHP rules live inside the source, so switching one off for five minutes means editing the script and remembering to put it back, and other people who pull the repo for rule improvements get merge conflicts with their own local tweaks.

How it works instead:

- The default rules are embedded in the binary. Updating the binary updates the rules.
- You keep a small overlay file that only says what you change: rules to switch off, keys of a default to override, and rules of your own. Anything you do not mention comes from the binary.
- A TUI toggles rules with a keypress. It only ever rewrites one line of the overlay.

## Install

Download the binary for your platform from the [latest release](https://github.com/ohnotnow/agent-edit-checker-go/releases/latest), make it executable and put it somewhere on your path. Or, with Go installed:

```bash
go install github.com/ohnotnow/agent-edit-checker-go/cmd/aec@latest
```

Then wire the hooks into Claude Code:

```bash
aec install
```

This backs up `~/.claude/settings.json` to a timestamped copy, adds five hooks (`PreToolUse` ones for `Write|Edit` and for `Bash`, a `PostToolUseFailure` one for every tool, a `UserPromptSubmit` one, and a `PostToolUse` one for `Write|Edit|Read|Agent`), shows the plan and asks before writing. It never removes or edits other hooks. If the old PHP hooks are still there it says so and leaves them alone, since you may have customised them.

Flags: `--dry-run` shows the plan and changes nothing, `--yes` skips the prompt, `--scope project` or `--scope local` writes to the current project's `.claude/settings.json` or `.claude/settings.local.json` instead of the user file, and `--settings <path>` names any other file. The rules overlay is global whatever the scope.

Restart Claude Code, or start a new session, for the hooks to take effect.

## The overlay

The overlay lives at `~/.config/aec/rules.toml` (or under `$XDG_CONFIG_HOME` if set). The first `aec install` or `aec rules` command writes a starter file that is entirely comments. A rule is a regex plus a message: file rules check the content an agent is about to write or edit, command rules check the Bash command it is about to run, and a match blocks the action and shows the agent the message. Prompt rules check the prompt you submit and add their message to the agent's context instead of blocking. The starter walks through the six things you can do:

```toml
# 1. Switch a default off by name.
disabled = ["no-throw"]

# 2. Reword a default. Only the keys you give are replaced.
[[rules]]
name = "one-test-at-a-time"
message = "Write one test, run it, then write the next."

# 3. Add a file rule of your own.
[[rules]]
name = "no-dd"
files = ["php"]
pattern = '/\bdd\(/'
message = "Don't use dd(). Use Log::debug() and check the log."

# 4. Add a command rule.
[[rules]]
name = "no-svn"
command = '/\bsvn\s/'
pattern = '/\bsvn\s+(checkout|co|commit|ci|update|up)\b/'
message = "It's 2026 - take a good look at yourself."

# 5. Add a prompt rule. This one matches every prompt; text that should
#    always apply usually belongs in CLAUDE.md instead.
[[rules]]
name = "british-english"
prompt = true
pattern = '/.*/s'
message = "Use British English spelling."

# 6. Tune the nudges. Give only the settings you want changed.
[nudge.models]
"opus-5-5" = 9
```

Patterns are PHP-style regexes with delimiters and flags, so anything from the PHP hooks pastes in unchanged. Use single quotes around them in TOML so backslashes are taken literally.

A rule with `files` runs on Write and Edit content for those file types (`php`, `.blade.php`, `migration.php`, or `*` for every file). A rule with `command` runs on Bash commands that match that regex; `type = "forbid"` (the default) blocks when `pattern` matches, `type = "require"` blocks when it does not. `max_matches` on a file rule blocks only when the new content adds more than that many matches. A rule with `prompt = true` runs on the prompt you submit and, when `pattern` matches, adds its message to the agent's context; it never blocks and cannot also have `files` or `command`. For context that needs a script, such as a nudge on one prompt in ten, add your own `UserPromptSubmit` hook beside `aec`.

The nudge hook has three signals, each of which adds a message to the agent's context once and stays quiet until it re-arms:

- `state-load`: too many files edited but not re-read. It lists them and asks for a re-read.
- `confidence-check`: a long run of edits with no Read in between. It asks the agent to verify one thing it has assumed and sweep its recent output for real hostnames, names and secrets.
- `test-quality`: in a Laravel project, enough test files edited since the last review. It asks the agent to launch the reviewer subagent, and launching it resets the count.

Signal names go in `disabled` like rule names, and the TUI lists them after the rules. The thresholds live in a `[nudge]` table; `aec rules list` prints the effective values:

```toml
[nudge]
dirty_threshold = 5      # state-load, for models not listed below
confidence_streak = 12   # confidence-check
test_files = 8           # test-quality
test_agent = "test-quality-checker"

[nudge.models]           # state-load threshold per model
haiku = 5
opus = 7
fable = 10
mythos = 10
```

A model key matches any model id containing it, and the longest matching key wins, so `"opus-5-5" = 9` tunes one release without touching the rest. When something fires, the message also lists the in-progress `ait` issues of the project, if it has an `.ait` database.

If the overlay names a rule that no longer exists, or has a key `aec` does not know, the `rules` and `tui` commands warn on stderr. The hooks stay quiet and use whatever they can read.

## Commands

```
aec rules list         the effective rules as TOML, each marked default, overridden or user
aec rules show <name>  one default rule as TOML, ready to paste into the overlay
aec rules diff         what your overlay changes from the defaults
aec tui                toggle rules interactively (space toggles, j/k move, q quits)
aec install            wire the hooks into Claude Code
aec version            print the version and check for a newer release
aec self-update        download and install the latest release
aec hook edit          the Write|Edit hook (Claude Code runs this, you do not)
aec hook bash          the Bash hook
aec hook tool-fails    the PostToolUseFailure hook, logs failed tool calls
aec hook prompt        the UserPromptSubmit hook, adds matching prompt rule messages to context
aec hook nudge         the PostToolUse hook, nudges the agent to re-read and review
```

`aec version` prints the running version and, for release builds, whether a newer one exists. `aec self-update` fetches the release for your platform, checks it against the published `SHA256SUMS`, shows the release notes and asks before swapping the binary. `--yes` skips the prompt and `--check` only reports, exiting 0 when current, 1 when an update exists and 2 when it could not look. Binaries installed by Homebrew or `go install` are pointed at those tools instead. Dev builds never self-update.

Rules in the TUI are written to the overlay on every toggle, so the next tool call sees the change. It only ever replaces the `disabled` line and checks the file reads back correctly after writing.

## Hook contract

Claude Code sends the tool call as JSON on stdin. The hook exits 0 to allow the call, or prints one line per rule that fired on stderr (a cross mark, then `Blocked: <message>`) and exits 2 to block it. Invalid JSON allows the call. Every Bash decision is appended to `~/.config/aec/tool-use.log`.

The tool-fails hook never blocks and always exits 0. It appends each failed tool call, except ones you interrupted with escape, to `~/.config/aec/tool-fails.log` as `timestamp | tool | command, file path or tool input | error`.

The prompt hook prints the message of every enabled prompt rule that matches the submitted prompt on stdout, with a blank line between them, which Claude Code adds to the agent's context. It always exits 0.

The nudge hook keeps per-session state in `~/.config/aec/state/` (files untouched for 48 hours are pruned), logs each firing to `~/.config/aec/state-nudge.log`, prints JSON `additionalContext` when a signal fires, and always exits 0.

Other commands exit 0 on success, 64 for a usage error, 65 for a bad file, 66 when a name is not found and 1 for anything else, with one `aec: <message>` line on stderr.

## Building from source

```bash
git clone https://github.com/ohnotnow/agent-edit-checker-go.git
cd agent-edit-checker-go
go test ./...
go build -o aec ./cmd/aec
```

Releases are built by GitHub Actions on a `v*` tag, with the version stamped in at build time.

## License

MIT, see [LICENSE](LICENSE).
