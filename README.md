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

This backs up `~/.claude/settings.json` to a timestamped copy, adds two `PreToolUse` hooks (one for `Write|Edit`, one for `Bash`), shows the plan and asks before writing. It never removes or edits other hooks. If the old PHP hooks are still there it says so and leaves them alone, since you may have customised them.

Flags: `--dry-run` shows the plan and changes nothing, `--yes` skips the prompt, `--scope project` or `--scope local` writes to the current project's `.claude/settings.json` or `.claude/settings.local.json` instead of the user file, and `--settings <path>` names any other file. The rules overlay is global whatever the scope.

Restart Claude Code, or start a new session, for the hooks to take effect.

## The overlay

The overlay lives at `~/.config/aec/rules.toml` (or under `$XDG_CONFIG_HOME` if set). The first `aec install` or `aec rules` command writes a starter file that is entirely comments. A rule is a regex plus a message: file rules check the content an agent is about to write or edit, command rules check the Bash command it is about to run, and a match blocks the action and shows the agent the message. The starter walks through the four things you can do:

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
```

Patterns are PHP-style regexes with delimiters and flags, so anything from the PHP hooks pastes in unchanged. Use single quotes around them in TOML so backslashes are taken literally.

A rule with `files` runs on Write and Edit content for those file types (`php`, `.blade.php`, `migration.php`, or `*` for every file). A rule with `command` runs on Bash commands that match that regex; `type = "forbid"` (the default) blocks when `pattern` matches, `type = "require"` blocks when it does not. `max_matches` on a file rule blocks only when the new content adds more than that many matches.

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
```

`aec version` prints the running version and, for release builds, whether a newer one exists. `aec self-update` fetches the release for your platform, checks it against the published `SHA256SUMS`, shows the release notes and asks before swapping the binary. `--yes` skips the prompt and `--check` only reports, exiting 0 when current, 1 when an update exists and 2 when it could not look. Binaries installed by Homebrew or `go install` are pointed at those tools instead. Dev builds never self-update.

Rules in the TUI are written to the overlay on every toggle, so the next tool call sees the change. It only ever replaces the `disabled` line and checks the file reads back correctly after writing.

## Hook contract

Claude Code sends the tool call as JSON on stdin. The hook exits 0 to allow the call, or prints one line per rule that fired on stderr (a cross mark, then `Blocked: <message>`) and exits 2 to block it. Invalid JSON allows the call. Every Bash decision is appended to `~/.config/aec/tool-use.log`.

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
