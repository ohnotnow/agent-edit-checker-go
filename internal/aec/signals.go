package aec

import "slices"

// Signal is one of the nudge hook's signals. Its name shares the rule
// namespace so the overlay's disabled list and the TUI switch it off like
// a rule.
type Signal struct {
	Name    string
	Summary string
	Enabled bool
}

var defaultSignals = []Signal{
	{Name: "state-load", Summary: "Re-read files edited but not re-read once enough pile up"},
	{Name: "confidence-check", Summary: "Verify something after a long run of edits with no Read"},
	{Name: "test-quality", Summary: "Suggest a fresh-eyes test review after enough test files (Laravel)"},
}

func isSignalName(name string) bool {
	return slices.ContainsFunc(defaultSignals, func(s Signal) bool { return s.Name == name })
}

// mergeSignals returns every signal, enabled unless disabled names it.
func mergeSignals(disabled []string) []Signal {
	out := slices.Clone(defaultSignals)
	for i := range out {
		out[i].Enabled = !slices.Contains(disabled, out[i].Name)
	}
	return out
}
