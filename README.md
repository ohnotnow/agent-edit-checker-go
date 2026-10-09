# agent-edit-checker-go

`aec` is a single Go binary that sits in Claude Code's hooks and keeps an agent on your conventions. It does five jobs:

- **Blocks edits** that break a rule, such as a `try/catch` in PHP, a Mockery mock, or an em dash in any file.
- **Blocks Bash commands** that break a rule, such as reading `.env`, installing a package without asking, or a commit message with an AI attribution line.
- **Adds context to your prompts**, such as a reminder to answer rather than act when your prompt contains a question mark.
- **Nudges the agent** mid-session to re-read files it has been editing from memory, to stop and verify after a long run of edits, and to get its tests reviewed.
- **Logs failed tool calls** so you can see where agents keep calling tools wrongly.

The default rules are built into the binary, and you keep a small overlay file holding only the changes you want. Many of the defaults are Laravel and PHP conventions; switch off whatever does not suit you.

## What each part is for

### Blocking rules

Before Claude Code runs a Write or Edit, `aec` checks the new content against the rules for that file type. Before it runs a Bash command, `aec` checks the command. If a rule matches, the call is blocked and the agent is shown the rule's message, so it learns why and can try again properly. `aec rules list` shows every rule and its message.

### Prompt rules

These match the prompt you submit and add their message to the agent's context. They never block. The defaults are:

- `question-pause`: a `?` reminds the agent to answer and recommend, not start editing.
- `verify-claims`: typing `kk` tells it to check facts against a real source before answering.
- `grounded-recommendation`: "which approach" or "what do you recommend" tells it to read the code before giving an opinion.

### Nudges

Long-running agents lose track of the state they are working in, and they lose it quietly. The paper [World-Model Collapse as a Phase Transition](https://arxiv.org/abs/2606.31399) (Song and Cai) found that an agent's internal picture of the world holds up well as load increases, then collapses suddenly near a critical point. The world state goes wrong before the actions do, so the agent ends up acting confidently on a corrupted picture. Stronger models move that point further out, but it is still there.

The nudge hook watches for the conditions that lead there. When one is met, it adds a short message to the agent's context once, and stays quiet until it re-arms:

- `state-load`: too many files edited but not re-read since. It lists them and asks the agent to re-read them. The threshold depends on the model.
- `confidence-check`: a long run of edits with no Read in between. It asks the agent to name and verify the riskiest thing it has assumed, and to sweep what it has written for real hostnames, names and secrets.
- `test-quality`: in a Laravel project, once enough test files have been edited since the last review, it asks the agent to hand them to a reviewer subagent (`test-quality-checker` by default, from [agentic-stuff](https://github.com/ohnotnow/agentic-stuff)). It only fires if that agent is installed. Launching the agent resets the count.

If the project has an [`ait`](https://github.com/ohnotnow/agent-issue-tracker) issue database (`.ait/ait.db`), each nudge also lists the in-progress issues, so the agent can check it is still working towards the goal.

Agents that have been nudged mid-loop tend to say it helped. Because the nudge arrives unexpectedly, partway through a run, it gets attention where a standing instruction in CLAUDE.md would not.

### Tool failure log

Every failed tool call is added to `~/.config/aec/tool-fails.log`, except calls you interrupt with escape. Check it now and then for patterns: a flag that changed between versions of a tool, a command the agent always gets wrong the first time, a tool it misuses. Each pattern is a hint that belongs in CLAUDE.md or a skill, for example "in ripgrep 1.2.3 `--thing` became `--thang`".

## Install

Download the binary for your platform from the [latest release](https://github.com/ohnotnow/agent-edit-checker-go/releases/latest), make it executable and put it somewhere on your path. Or, with Go installed:

```bash
go install github.com/ohnotnow/agent-edit-checker-go/cmd/aec@latest
```

Then wire the hooks into Claude Code:

```bash
aec install
```

This shows what it plans to change and asks before writing anything. If you say yes, it backs up `~/.claude/settings.json` to a timestamped copy and adds five hooks:

| Event | Tools | Job |
|-------|-------|-----|
| `PreToolUse` | `Write\|Edit` | blocking file rules |
| `PreToolUse` | `Bash` | blocking command rules |
| `UserPromptSubmit` | all | prompt rules |
| `PostToolUse` | `Write\|Edit\|Read\|Agent` | nudges |
| `PostToolUseFailure` | all | tool failure log |

It never removes or edits other hooks. If the old PHP hooks are still there it says so and leaves them alone, since you may have customised them.

Flags: `--dry-run` shows the plan and changes nothing, `--yes` skips the prompt, `--scope project` or `--scope local` writes to the current project's `.claude/settings.json` or `.claude/settings.local.json` instead of the user file, and `--settings <path>` names any other file. The rules overlay is global whatever the scope.

Restart Claude Code, or start a new session, for the hooks to take effect.

## Configuring

### Switching things off

`aec tui` lists every rule and nudge signal. Press space to toggle one (j/k move, q quits). The change is written straight away, so the next tool call sees it.

### The overlay

Your changes live in `~/.config/aec/rules.toml` (or under `$XDG_CONFIG_HOME` if set). The first `aec install` or `aec rules` command writes a starter file that is entirely comments. You only write what you want to change; anything you do not mention comes from the binary, so updating `aec` updates the defaults without touching your changes.

The starter walks through the six things you can do:

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

Patterns are PHP-style regexes with delimiters and flags (`i`, `m`, `s`), so anything from the PHP hooks pastes in unchanged. Use single quotes around them in TOML so backslashes are taken literally.

Each rule targets one of three things:

- `files`: Write and Edit content for those file types (`php`, `.blade.php`, `migration.php`, or `*` for every file). `max_matches` blocks only when the new content adds more than that many matches, which is how `one-test-at-a-time` allows editing a file that already has tests in it.
- `command`: Bash commands matching that regex. `type = "forbid"` (the default) blocks when `pattern` matches; `type = "require"` blocks when it does not.
- `prompt = true`: the prompt you submit. A match adds the message to the agent's context. It never blocks and cannot be combined with `files` or `command`.

For prompt context that needs a script, such as a nudge on one prompt in ten, add your own `UserPromptSubmit` hook alongside `aec`.

### Nudge settings

Signal names go in `disabled` like rule names. The thresholds live in a `[nudge]` table, and `aec rules list` prints the effective values:

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

A model key matches any model id containing it, and the longest matching key wins, so `"opus-5-5" = 9` tunes one release without touching the rest. Subagents always use `dirty_threshold`.

If the overlay names a rule that no longer exists, or has a key `aec` does not know, the `rules` and `tui` commands warn on stderr. The hooks stay quiet and use whatever they can read.

## Commands

```
aec rules list         the effective rules as TOML, each marked default, overridden or user
aec rules show <name>  one default rule as TOML, ready to paste into the overlay
aec rules diff         what your overlay changes from the defaults
aec tui                toggle rules and nudge signals interactively
aec install            wire the hooks into Claude Code
aec version            print the version and check for a newer release
aec self-update        download and install the latest release
aec hook edit          the Write|Edit hook (Claude Code runs these, you do not)
aec hook bash          the Bash hook
aec hook prompt        the UserPromptSubmit hook
aec hook nudge         the PostToolUse hook
aec hook tool-fails    the PostToolUseFailure hook
```

`aec self-update` fetches the release for your platform, checks it against the published `SHA256SUMS`, shows the release notes and asks before swapping the binary. `--yes` skips the prompt and `--check` only reports, exiting 0 when current, 1 when an update exists and 2 when it could not look. Binaries installed by Homebrew or `go install` are pointed at those tools instead. Dev builds never self-update.

## Files

Everything lives in `~/.config/aec/`:

| File | Contents |
|------|----------|
| `rules.toml` | your overlay |
| `tool-use.log` | every Bash command the agent ran, marked allowed or denied |
| `tool-fails.log` | `timestamp \| tool \| command, file path or tool input \| error` |
| `state-nudge.log` | each nudge that fired, and why |
| `state/` | per-session nudge state; files untouched for 48 hours are pruned |

The logs are never rotated, and `tool-use.log` holds commands exactly as the agent ran them, including anything secret passed inline.

## Hook contract

Claude Code sends the tool call as JSON on stdin.

- `hook edit` and `hook bash` exit 0 to allow the call. To block it they print one `❌ Blocked: <message>` line per rule that fired on stderr and exit 2. Invalid JSON allows the call.
- `hook prompt` prints the message of every matching prompt rule on stdout, separated by blank lines, and always exits 0.
- `hook nudge` prints JSON `additionalContext` when a signal fires and always exits 0.
- `hook tool-fails` prints nothing and always exits 0.

A broken overlay never stops the hooks: they report it on stderr and fall back to the defaults.

Other commands exit 0 on success, 64 for a usage error, 65 for a bad file, 66 when a name is not found and 1 for anything else, with one `aec: <message>` line on stderr.

## Coming from the PHP hooks

`aec` replaces the PHP scripts in [agent-edit-checker](https://github.com/ohnotnow/agent-edit-checker). There, the rules live inside the source, so switching one off for five minutes means editing the script and remembering to put it back, and pulling rule improvements conflicts with your local tweaks. Here the defaults ship in the binary and your changes live in the overlay, so the two never collide. The regex syntax is the same, and `scripts/php-parity.sh <path-to-php-repo>` checks that both give the same decision on every test fixture.

## Building from source

```bash
git clone https://github.com/ohnotnow/agent-edit-checker-go.git
cd agent-edit-checker-go
go test ./...
go build -o aec ./cmd/aec
```

Releases are built by GitHub Actions on a `v*` tag, with the version stamped in at build time. See `TECHNICAL_OVERVIEW.md` for how the code fits together.

## License

MIT, see [LICENSE](LICENSE).
