# agent-edit-checker-go

**Work in progress.** Nothing here is usable yet.

`aec` is a single Go binary that screens Claude Code tool calls before they run. It checks Write and Edit content against per-file-type regex rules, and Bash commands against require/forbid rules, and blocks the call with a message when a rule fires. It is the successor to the PHP hook scripts in [agent-edit-checker](https://github.com/ohnotnow/agent-edit-checker).

Why a rewrite: the PHP rules live inside the source, so switching one off for five minutes means editing the script and remembering to put it back, and other people who pull the repo for rule improvements get merge conflicts with their own local tweaks.

The plan:

- Default rules are embedded in the binary as a TOML file.
- Users keep only an overlay: a `disabled` list, overrides of a default's keys, and rules of their own. Anything not mentioned comes fresh from the binary.
- A small TUI toggles rules and shows what differs from the defaults.
- A `rules` command lists, diffs and shows rules; `install` wires the hook into Claude Code's settings.

## Status

Rule loading and pattern compilation work. The default rules have not been ported yet, and there is no hook, CLI or TUI.

```bash
git clone https://github.com/ohnotnow/agent-edit-checker-go.git
cd agent-edit-checker-go
go test ./...
```

## License

MIT, see [LICENSE](LICENSE).
