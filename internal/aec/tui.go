package aec

import (
	"fmt"
	"io"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleCursor  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5e81ac", Dark: "#88c0d0"})
	styleChanged = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#b48ead", Dark: "#ebcb8b"})
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4c566a", Dark: "#7b88a1"})
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5a8a4a", Dark: "#a3be8c"})
	styleErr     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#bf616a", Dark: "#bf616a"})
)

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
	m := tuiModel{rules: merged.Rules, signals: merged.Signals, path: path, width: 80, height: 24}
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(stdin), tea.WithOutput(stdout)).Run()
	return err
}

// tuiModel lists the rules, then the nudge signals, as one list of rows.
type tuiModel struct {
	rules   []Rule
	signals []Signal
	path    string
	cursor  int
	width   int
	height  int
	status  string
	failed  bool
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
	b.WriteString(styleCursor.Render("aec rules") + styleMuted.Render("  space toggles, j/k move, q quits") + "\n\n")

	visible := m.height - 4
	if visible < 1 {
		visible = 1
	}
	offset := 0
	if m.cursor >= visible {
		offset = m.cursor - visible + 1
	}
	for i := offset; i < len(m.rules)+len(m.signals) && i < offset+visible; i++ {
		b.WriteString(m.row(i) + "\n")
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

func (m tuiModel) row(i int) string {
	var name, text, tag string
	var enabled, changed bool
	if i < len(m.rules) {
		r := m.rules[i]
		name, enabled = r.Name, r.Enabled
		text, _, _ = strings.Cut(r.Message, "\n")
		if r.Origin != OriginDefault {
			tag = " (" + r.Origin.String() + ")"
		}
		changed = !r.Enabled || r.Origin != OriginDefault
	} else {
		s := m.signals[i-len(m.rules)]
		name, enabled, text, changed = s.Name, s.Enabled, s.Summary, !s.Enabled
	}
	mark := "[x]"
	if !enabled {
		mark = "[ ]"
	}
	head := fmt.Sprintf("%s %-24s%s", mark, name, tag)
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
