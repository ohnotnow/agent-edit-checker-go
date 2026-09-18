package aec

import (
	"fmt"
	"io"
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
	m := tuiModel{rules: merged.Rules, path: path, width: 80, height: 24}
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(stdin), tea.WithOutput(stdout)).Run()
	return err
}

type tuiModel struct {
	rules  []Rule
	path   string
	cursor int
	width  int
	height int
	status string
	failed bool
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
			if m.cursor < len(m.rules)-1 {
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

// toggle flips the current rule and writes the overlay. A failed write
// puts the rule back and reports the error.
func (m tuiModel) toggle() tuiModel {
	r := &m.rules[m.cursor]
	r.Enabled = !r.Enabled
	if err := WriteDisabled(m.path, disabledNames(m.rules)); err != nil {
		r.Enabled = !r.Enabled
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
	for i := offset; i < len(m.rules) && i < offset+visible; i++ {
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
	r := m.rules[i]
	mark := "[x]"
	if !r.Enabled {
		mark = "[ ]"
	}
	tag := ""
	if r.Origin != OriginDefault {
		tag = " (" + r.Origin.String() + ")"
	}
	head := fmt.Sprintf("%s %-24s%s", mark, r.Name, tag)
	room := m.width - lipgloss.Width(head) - 4
	msg := truncate(r.Message, room)

	prefix := "  "
	if i == m.cursor {
		prefix = "> "
	}
	changed := !r.Enabled || r.Origin != OriginDefault
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
