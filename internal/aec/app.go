package aec

import (
	"fmt"
	"io"
)

const usage = `usage: aec <command>

commands:
  hook edit    PreToolUse hook for the Write|Edit matcher (payload on stdin)
  hook bash    PreToolUse hook for the Bash matcher (payload on stdin)
  rules list   Print the effective rules as TOML, annotated with their origin
  rules show <name>
               Print one default rule as TOML, ready to paste into the overlay
  rules diff   Show what the overlay changes from the defaults
  tui          Toggle rules interactively
  install      Wire the hooks into Claude Code's settings
               [--scope user|project|local] [--settings <path>] [--dry-run] [--yes]
  version      Show version and check for updates
  self-update  Download and install the latest release [--check] [--yes]
  help         Show this text
`

// command is one subcommand. args are the arguments after the command
// name; a returned error decides the exit code, see report.
type command func(args []string, stdin io.Reader, stdout, stderr io.Writer) error

// groups are the first words that take a second word to name the command.
var groups = map[string]bool{"hook": true, "rules": true}

var commands = map[string]command{
	"hook edit":   hookCommand(hookEdit),
	"hook bash":   hookCommand(hookBash),
	"rules list":  rulesList,
	"rules show":  rulesShow,
	"rules diff":  rulesDiff,
	"tui":         tui,
	"install":     install,
	"version":     version,
	"self-update": selfUpdate,
}

// Run dispatches a command line and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			fmt.Fprint(stdout, usage)
			return exitOK
		case "--version":
			args = append([]string{"version"}, args[1:]...)
		}
	}
	cmd, rest, ok := lookup(args)
	if !ok {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	return report(cmd(rest, stdin, stdout, stderr), stderr)
}

// lookup resolves args to a command and its remaining arguments.
func lookup(args []string) (command, []string, bool) {
	if len(args) == 0 {
		return nil, nil, false
	}
	name, rest := args[0], args[1:]
	if groups[name] {
		if len(rest) == 0 {
			return nil, nil, false
		}
		name, rest = name+" "+rest[0], rest[1:]
	}
	cmd, ok := commands[name]
	return cmd, rest, ok
}
