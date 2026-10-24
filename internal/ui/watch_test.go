package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

func ids(issues []youtrack.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.ID)
	}
	return out
}

// The first poll of a filter must only seed. Otherwise starting the program
// announces every issue that already matched, which is noise, not news.
func TestWatcherFirstPollIsSilent(t *testing.T) {
	w := newWatcher([]string{"145-3"})

	if got := w.record("145-3", page(3, 1)); len(got) != 0 {
		t.Errorf("first poll reported %v, want nothing", ids(got))
	}
	if len(w.fresh) != 0 {
		t.Errorf("first poll marked %d issues new", len(w.fresh))
	}

	// Same set again: still nothing.
	if got := w.record("145-3", page(3, 1)); len(got) != 0 {
		t.Errorf("unchanged poll reported %v", ids(got))
	}

	// One arrival among the old ones.
	got := w.record("145-3", append(page(3, 1), youtrack.Issue{ID: "PAY-99", Summary: "new one"}))
	if len(got) != 1 || got[0].ID != "PAY-99" {
		t.Fatalf("reported %v, want just PAY-99", ids(got))
	}
	if !w.isFresh("PAY-99") {
		t.Error("the arrival was not marked new")
	}

	// It is only news once.
	if got := w.record("145-3", append(page(3, 1), youtrack.Issue{ID: "PAY-99"})); len(got) != 0 {
		t.Errorf("reported %v on a repeat poll", ids(got))
	}
}

// Each filter seeds independently: watching a second one later must not
// announce its whole backlog as new, nor stay silent forever.
func TestWatcherSeedsPerFilter(t *testing.T) {
	w := newWatcher(nil)
	w.record("a", page(2, 1))
	if got := w.record("b", page(2, 1)); len(got) != 0 {
		t.Errorf("second filter reported %v on its first poll", ids(got))
	}
	if got := w.record("b", append(page(2, 1), youtrack.Issue{ID: "PAY-50"})); len(got) != 1 {
		t.Errorf("second filter reported %v, want one arrival", ids(got))
	}
}

func TestNotifyCommand(t *testing.T) {
	name, args, err := notifyCommand("", "title", "body")
	if err != nil || name != "zenity" {
		t.Fatalf("default notifier = %q, %v", name, err)
	}
	if len(args) != 2 || args[0] != "--notification" || !strings.Contains(args[1], "title\nbody") {
		t.Errorf("zenity args = %q", args)
	}

	if name, _, err := notifyCommand(config.NotifierNotifySend, "t", "b"); err != nil || name != "notify-send" {
		t.Errorf("notify-send = %q, %v", name, err)
	}
	if name, _, err := notifyCommand(config.NotifierNone, "t", "b"); err != nil || name != "" {
		t.Errorf("none should be a no-op, got %q %v", name, err)
	}
	if _, _, err := notifyCommand("wat", "t", "b"); err == nil {
		t.Error("an unknown notifier was accepted")
	}
}

// A popup is not a list view: past three, it counts.
func TestNotifyBodyIsCapped(t *testing.T) {
	if cmd := notifyNew(config.NotifierNone, "Sprint 42", nil); cmd != nil {
		t.Error("notifying about nothing produced a command")
	}
	body := notifyBody(page(5, 1))
	if !strings.Contains(body, "…and 2 more") {
		t.Errorf("body did not cap at %d lines:\n%s", notifyLines, body)
	}
	if strings.Contains(body, "PAY-4") {
		t.Errorf("body listed past the cap:\n%s", body)
	}
}

// `w` is remembered: what was being watched when the program closed is what it
// polls on the next run. Matching is by ID for the same reason favourites are.
func TestWatchTogglePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
		Watch: []string{"145-3"},
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !m.watch.watching["145-3"] {
		t.Fatal("the config's watch list did not seed the session")
	}

	m.savedQueries = []youtrack.SavedQuery{
		{ID: "145-3", Name: "Sprint 42", Query: "project: PAY"},
		{ID: "145-9", Name: "Waiting on me", Query: "for: me"},
	}
	m.setFilterItems()

	// Watch one more and stop watching the seeded one.
	selectFilter(t, m, "Waiting on me")
	m.toggleWatch()
	selectFilter(t, m, "Sprint 42")
	m.toggleWatch()
	if m.watch.watching["145-3"] || !m.watch.watching["145-9"] {
		t.Fatalf("session watch list = %v", m.watch.watching)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "- 145-3") || !strings.Contains(string(raw), "- 145-9") {
		t.Errorf("`w` was not persisted as IDs:\n%s", raw)
	}
	if got := cfg.Providers[0].Watch; len(got) != 1 || got[0] != "145-9" {
		t.Errorf("watch list = %v, want just the filter still being watched", got)
	}

	// A reload sees exactly what was left running.
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := New(reloaded, "", "", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m2.watch.watching["145-3"] || !m2.watch.watching["145-9"] {
		t.Errorf("watch list did not survive the restart: %v", m2.watch.watching)
	}
}

// Every toggle must retire the previous tick chain, or two toggles leave two
// pollers hitting the instance.
func TestWatchTogglesRetireTheOldTicker(t *testing.T) {
	m := testModel(t, 50)
	m.savedQueries = []youtrack.SavedQuery{{ID: "145-3", Name: "Sprint 42", Query: "project: PAY"}}
	m.setFilterItems()

	m.filters.Select(0)
	m.toggleWatch()
	first := m.watchGen
	m.toggleWatch()
	if m.watchGen == first {
		t.Fatal("toggling did not bump the watch generation")
	}

	// A tick from the retired chain does nothing.
	_, cmd := m.Update(watchTickMsg{gen: first})
	if cmd != nil {
		t.Error("a stale tick kept polling")
	}
	// So does a result from it.
	m.watch.failed = false
	m.Update(watchResultMsg{gen: first, filterID: "145-3", err: os.ErrClosed})
	if m.watch.failed {
		t.Error("a stale poll result was recorded")
	}
}

// Opening an issue is what clears its marker.
func TestOpeningAnIssueClearsItsMark(t *testing.T) {
	m := testModel(t, 50)
	m.allIssues = page(2, 1)
	m.watch.fresh["PAY-1"] = "145-3"
	m.setIssueItems()

	if !m.issues.Items()[0].(issueItem).isNew {
		t.Fatal("PAY-1 was not marked new")
	}
	m.Update(detailMsg{gen: m.gen, issue: &youtrack.Issue{ID: "PAY-1"}})
	if m.watch.isFresh("PAY-1") {
		t.Error("reading the issue left it marked new")
	}
	if m.issues.Items()[0].(issueItem).isNew {
		t.Error("the list still shows the marker")
	}
}

func TestWatchKeyOnlyOnTheFiltersScreen(t *testing.T) {
	m := testModel(t, 50)
	m.screen = screenIssues
	if _, cmd := m.onKey(tea.KeyPressMsg{Code: 'w', Text: "w"}); cmd != nil {
		t.Error("`w` did something on the issues screen")
	}
}
