package aec

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleCursor  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5e81ac", Dark: "#88c0d0"})
	styleHeading = lipgloss.NewStyle().Bold(true)
	styleChanged = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#b48ead", Dark: "#ebcb8b"})
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4c566a", Dark: "#7b88a1"})
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5a8a4a", Dark: "#a3be8c"})
	styleErr     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#bf616a", Dark: "#bf616a"})
)

// tuiSections are the headings over the three groups of rows, in order.
var tuiSections = [...]struct{ title, hint string }{
	{"Blocking rules", "stop a Write, Edit or Bash call"},
	{"Prompt rules", "add context when you submit a prompt"},
	{"Nudges", "add context mid-session"},
}

// tui opens the interactive rule toggle list.
func tui(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageErr("tui takes no arguments")
	}
	merged, _, err := loadForCLI(stderr)
	if err != nil {
		return err
	}
	path, err := OverlayPath()
	if err != nil {
		return err
	}
	cwd, _ := workDir()
	m := newTuiModel(merged, path, testAgentInstalled(merged.Nudge.TestAgent, cwd))
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(stdin), tea.WithOutput(stdout)).Run()
	return err
}

// tuiModel lists the blocking rules, the prompt rules, then the nudge
// signals, as one list of rows.
type tuiModel struct {
	rules        []Rule
	signals      []Signal
	nudge        NudgeSettings
	nudgeChanged []string
	agentFound   bool // the test-quality reviewer agent is installed
	path         string
	cursor       int
	width        int
	height       int
	status       string
	failed       bool
}

// newTuiModel builds the model, putting the blocking rules before the
// prompt rules and keeping each group in list order.
func newTuiModel(merged Merged, path string, agentFound bool) tuiModel {
	var blocking, prompt []Rule
	for _, r := range merged.Rules {
		if r.Prompt {
			prompt = append(prompt, r)
		} else {
			blocking = append(blocking, r)
		}
	}
	return tuiModel{
		rules:        append(blocking, prompt...),
		signals:      merged.Signals,
		nudge:        merged.Nudge,
		nudgeChanged: merged.NudgeChanged,
		agentFound:   agentFound,
		path:         path,
		width:        80,
		height:       24,
	}
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		m.status, m.failed = "", false
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.rules)+len(m.signals)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case " ", "enter":
			m = m.toggle()
		}
	}
	return m, nil
}

// toggle flips the current row and writes the overlay. A failed write
// puts the row back and reports the error.
func (m tuiModel) toggle() tuiModel {
	var enabled *bool
	if m.cursor < len(m.rules) {
		enabled = &m.rules[m.cursor].Enabled
	} else {
		m.signals = slices.Clone(m.signals)
		enabled = &m.signals[m.cursor-len(m.rules)].Enabled
	}
	*enabled = !*enabled
	names := disabledNames(m.rules)
	for _, s := range m.signals {
		if !s.Enabled {
			names = append(names, s.Name)
		}
	}
	if err := WriteDisabled(m.path, names); err != nil {
		*enabled = !*enabled
		m.status, m.failed = "write failed: "+err.Error(), true
		return m
	}
	m.status = "saved"
	return m
}

// disabledNames lists the names of every disabled rule, in list order.
func disabledNames(rules []Rule) []string {
	var names []string
	for _, r := range rules {
		if !r.Enabled {
			names = append(names, r.Name)
		}
	}
	return names
}

func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString(styleCursor.Render("aec") + styleMuted.Render("  space toggles, j/k move, q quits") + "\n\n")

	lines, cursorEnd := m.lines()
	visible := max(m.height-4, 1)
	offset := max(cursorEnd-visible+1, 0)
	for _, line := range lines[offset:min(offset+visible, len(lines))] {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	left := styleMuted.Render(m.path)
	right := ""
	switch {
	case m.failed:
		right = styleErr.Render(m.status)
	case m.status != "":
		right = styleOK.Render(m.status)
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(left + strings.Repeat(" ", gap) + right)
	return b.String()
}

// lines renders every row under its section heading, and returns the index
// of the last line belonging to the cursor's row so scrolling keeps it all
// in view.
func (m tuiModel) lines() ([]string, int) {
	var lines []string
	cursorEnd := 0
	for i := range len(m.rules) + len(m.signals) {
		if s := m.section(i); i == 0 || s != m.section(i-1) {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, styleHeading.Render(tuiSections[s].title)+styleMuted.Render("  "+tuiSections[s].hint))
		}
		lines = append(lines, m.row(i))
		if d := m.detail(i); d != "" {
			lines = append(lines, d)
		}
		if i == m.cursor {
			cursorEnd = len(lines) - 1
		}
	}
	return lines, cursorEnd
}

// section is the index into tuiSections of row i.
func (m tuiModel) section(i int) int {
	switch {
	case i >= len(m.rules):
		return 2
	case m.rules[i].Prompt:
		return 1
	}
	return 0
}

func (m tuiModel) row(i int) string {
	var label, text string
	var enabled, changed bool
	if i < len(m.rules) {
		r := m.rules[i]
		label, enabled = ruleLabel(r), r.Enabled
		text, _, _ = strings.Cut(r.Message, "\n")
		changed = !r.Enabled || r.Origin != OriginDefault
	} else {
		s := m.signals[i-len(m.rules)]
		label, enabled, text, changed = s.Name, s.Enabled, s.Summary, !s.Enabled
	}
	mark := "[x]"
	if !enabled {
		mark = "[ ]"
	}
	head := fmt.Sprintf("%s %-*s", mark, m.nameWidth(), label)
	room := m.width - lipgloss.Width(head) - 4
	msg := truncate(text, room)

	prefix := "  "
	if i == m.cursor {
		prefix = "> "
	}
	line := head + "  " + msg
	switch {
	case i == m.cursor:
		line = styleCursor.Render(line)
	case changed:
		line = styleChanged.Render(line)
	}
	return prefix + line
}

// ruleLabel is a rule's name, tagged with its origin unless it is an
// untouched default.
func ruleLabel(r Rule) string {
	if r.Origin == OriginDefault {
		return r.Name
	}
	return r.Name + " (" + r.Origin.String() + ")"
}

// nameWidth is the width of the name column: at least 24, and wide enough
// for the longest name plus its origin tag, so every message lines up.
func (m tuiModel) nameWidth() int {
	width := 24
	for _, r := range m.rules {
		width = max(width, len(ruleLabel(r)))
	}
	for _, s := range m.signals {
		width = max(width, len(s.Name))
	}
	return width
}

// detail is the line under a nudge row giving its effective thresholds,
// highlighted when the overlay tunes them, and a warning when the
// test-quality reviewer agent is missing. Rule rows have none.
func (m tuiModel) detail(i int) string {
	if i < len(m.rules) {
		return ""
	}
	n := m.nudge
	tuned := func(prefixes ...string) bool {
		return slices.ContainsFunc(m.nudgeChanged, func(key string) bool {
			return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(key, p) })
		})
	}
	var text string
	var isTuned, warn bool
	switch m.signals[i-len(m.rules)].Name {
	case "state-load":
		text = fmt.Sprintf("at %d files", n.DirtyThreshold)
		var models []string
		for _, key := range slices.Sorted(maps.Keys(n.Models)) {
			models = append(models, fmt.Sprintf("%s %d", key, n.Models[key]))
		}
		if len(models) > 0 {
			text += "; by model: " + strings.Join(models, ", ")
		}
		isTuned = tuned("dirty_threshold", "models.")
	case "confidence-check":
		text = fmt.Sprintf("at %d edits in a row", n.ConfidenceStreak)
		isTuned = tuned("confidence_streak")
	case "test-quality":
		text = fmt.Sprintf("every %d test files, reviewed by %s", n.TestFiles, n.TestAgent)
		isTuned = tuned("test_files", "test_agent")
		if !m.agentFound {
			text += " (agent not found, so this stays quiet)"
			warn = true
		}
	default:
		return ""
	}
	text = truncate(text, m.width-8)
	switch {
	case warn:
		text = styleErr.Render(text)
	case isTuned:
		text = styleChanged.Render(text)
	default:
		text = styleMuted.Render(text)
	}
	return "      " + text
}

// truncate cuts s to at most width cells, marking the cut with "...".
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
