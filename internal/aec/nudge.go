package aec

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// nudgeState is one session's (or subagent's) record between hook calls.
// The JSON names are those the PHP hook used.
type nudgeState struct {
	Dirty           map[string]int64 `json:"dirty"`
	Armed           bool             `json:"armed"`
	Streak          int              `json:"streak"`
	StreakStarted   *int64           `json:"streak_started,omitempty"`
	ConfidenceArmed bool             `json:"confidence_armed"`
	TestFiles       map[string]int64 `json:"test_files"`
	TestNextFire    int              `json:"test_next_fire"`
	TestFired       int              `json:"test_fired"`
}

func newNudgeState(cfg NudgeSettings) *nudgeState {
	return &nudgeState{
		Dirty:           map[string]int64{},
		Armed:           true,
		ConfidenceArmed: true,
		TestFiles:       map[string]int64{},
		TestNextFire:    cfg.TestFiles,
	}
}

// record applies one successful Write, Edit or Read of path at now. A Read
// clears the file and resets the edit streak; a Write or Edit marks the
// file dirty, keeping its first-edited time, and extends the streak.
func (s *nudgeState) record(tool, path string, now int64) {
	if tool == "Read" {
		delete(s.Dirty, path)
		s.Streak = 0
		s.StreakStarted = nil
		return
	}
	if _, ok := s.Dirty[path]; !ok {
		s.Dirty[path] = now
	}
	s.Streak++
	if s.Streak == 1 {
		s.StreakStarted = &now
	}
}

// prune drops dirty and test files that no longer exist, so a deleted file
// cannot pin a count at its threshold.
func (s *nudgeState) prune(exists func(string) bool) {
	maps.DeleteFunc(s.Dirty, func(p string, _ int64) bool { return !exists(p) })
	maps.DeleteFunc(s.TestFiles, func(p string, _ int64) bool { return !exists(p) })
}

// evaluate applies the fire-once hysteresis to both counters: a signal
// re-arms while its count is below the threshold, and fires (disarming)
// when armed at or over it.
func (s *nudgeState) evaluate(dirtyThreshold, confidenceStreak int) (dirty, confidence bool) {
	if len(s.Dirty) < dirtyThreshold {
		s.Armed = true
	}
	if s.Armed && len(s.Dirty) >= dirtyThreshold {
		s.Armed, dirty = false, true
	}
	if s.Streak < confidenceStreak {
		s.ConfidenceArmed = true
	}
	if s.ConfidenceArmed && s.Streak >= confidenceStreak {
		s.ConfidenceArmed, confidence = false, true
	}
	return dirty, confidence
}

// byAge returns the paths oldest first, ties by path.
func byAge(m map[string]int64) []string {
	return slices.SortedFunc(maps.Keys(m), func(a, b string) int {
		return cmp.Or(cmp.Compare(m[a], m[b]), cmp.Compare(a, b))
	})
}

func minutesAgo(ts, now int64) string {
	minutes := max(0, now-ts) / 60
	if minutes < 1 {
		return "just now"
	}
	return strconv.FormatInt(minutes, 10) + "m ago"
}

func stateLoadMessage(s *nudgeState, threshold int, now int64) string {
	var b strings.Builder
	b.WriteString("State-load nudge (automatic: your count of edited-but-not-reread files just reached " + strconv.Itoa(len(s.Dirty)) + ").\n\n")
	b.WriteString("You are carrying these files as remembered state:\n")
	for _, p := range byAge(s.Dirty) {
		b.WriteString("- " + p + " (first edited " + minutesAgo(s.Dirty[p], now) + ", not re-read since)\n")
	}
	b.WriteString("\nDrift is silent - an internal model goes stale before any action visibly fails, " +
		`so do not trust a feeling of "I remember these fine". Re-read each file above before ` +
		"editing further; a Read clears a file from this set, and this nudge stays quiet " +
		"while the set stays below " + strconv.Itoa(threshold) + ".")
	return b.String()
}

func confidenceMessage(s *nudgeState, now int64) string {
	started := ""
	if s.StreakStarted != nil {
		started = " - the first of them " + minutesAgo(*s.StreakStarted, now)
	}
	return "Confidence check (automatic: " + strconv.Itoa(s.Streak) + " consecutive Write/Edit calls without a single Read" + started + ").\n\n" +
		"A long friction-free streak of edits is the exact condition where confident-momentum forms: " +
		"concerns get flagged then flowed past, and the session that feels like it's going best is the riskiest. " +
		"Before the next edit, do two things:\n" +
		"1. Name the riskiest thing you have asserted-but-not-verified during this streak, and verify it now - run the command, read the code. Do not reassure yourself from memory.\n" +
		"2. Sweep what you have written during the streak for exposure: real hostnames, internal IPs, people's names, secrets - none of those belong in code, tests, or notes.\n\n" +
		`Report briefly what you checked and what you found before carrying on. A bare "all fine" with nothing named is itself the flagged-then-flowed-past tell.`
}

// testPhrasings rotate between test-quality firings, since an identical
// banner every time is easy to stop reading.
var testPhrasings = []string{
	"A fresh pair of eyes catches the anti-patterns you have stopped seeing - and a habit corrected now is one the rest of the session does not repeat.",
	"From the inside every test you write looks fine; the suite-level tells (overlap, weak assertions, happy-path bias) are only visible cold.",
	"A run of green tests can look like rigour while sharing a single blind spot. A cold reviewer spots it while it is still cheap to fix.",
}

// ratchetTests moves the next test-quality firing a full threshold of new
// test files away once the count reaches it, and reports whether it did.
func (s *nudgeState) ratchetTests(testFiles int) bool {
	if len(s.TestFiles) == 0 || len(s.TestFiles) < s.TestNextFire {
		return false
	}
	s.TestNextFire += testFiles
	return true
}

func testQualityMessage(s *nudgeState, agent string, now int64) string {
	var b strings.Builder
	b.WriteString("Test-quality nudge (automatic: " + strconv.Itoa(len(s.TestFiles)) + " distinct test files edited since the last fresh-eyes review).\n\n")
	b.WriteString(testPhrasings[(s.TestFired-1)%len(testPhrasings)] + "\n\n")
	b.WriteString("The test files in question:\n")
	for _, p := range byAge(s.TestFiles) {
		b.WriteString("- " + p + " (first edited " + minutesAgo(s.TestFiles[p], now) + ")\n")
	}
	b.WriteString("\nLaunch the " + agent + " subagent now: hand it this file list and tell it this is a mid-feature WIP review, so it should judge the tests that exist rather than flag incompleteness. " +
		`Act on any "you really should fix" findings before writing more tests - launching the checker is what resets this counter.`)
	return b.String()
}
