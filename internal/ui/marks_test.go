package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

func markModel(t *testing.T) (*Model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.screen = screenIssues
	m.allIssues = []youtrack.Issue{
		{ID: "PAY-1", Summary: "one"},
		{ID: "PAY-2", Summary: "two"},
	}
	m.setIssueItems()
	return m, path
}

func markedIDs(m *Model) []string { return m.cfg.Providers[m.provider].Marked }

// `x` ticks an issue off and writes it to the config, the same way `f` and `w`
// persist what they toggle: a review worked through over a day is worth
// nothing if closing the program forgets it.
func TestMarkTogglePersists(t *testing.T) {
	m, path := markModel(t)

	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.dlg != nil {
		t.Fatalf("unexpected dialog: %s", m.dlg.body)
	}
	if got := markedIDs(m); len(got) != 1 || got[0] != "PAY-1" {
		t.Fatalf("marked = %v, want [PAY-1]", got)
	}
	if saved := readConfig(t, path); !strings.Contains(saved, "marked:") || !strings.Contains(saved, "PAY-1") {
		t.Errorf("config does not record the mark:\n%s", saved)
	}
	if it := m.issues.Items()[0].(issueItem); !it.marked {
		t.Error("the list item did not pick up the mark")
	}
	if title := m.issues.Items()[0].(issueItem).Title(); !strings.Contains(title, "✓") {
		t.Errorf("title = %q, want a ✓ in the gutter", title)
	}
	// The row below it is untouched, and the gutter keeps its width.
	if title := m.issues.Items()[1].(issueItem).Title(); strings.Contains(title, "✓") ||
		!strings.HasPrefix(title, "    PAY-2") {
		t.Errorf("unmarked title = %q", title)
	}

	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := markedIDs(m); len(got) != 0 {
		t.Fatalf("second x left %v marked", got)
	}
	if saved := readConfig(t, path); strings.Contains(saved, "PAY-1") {
		t.Errorf("unmarking did not reach the config:\n%s", saved)
	}
}

// Marking from an open issue works too, and says so in the header — the gutter
// glyph is a screen away, so without it `x` there looks like a dead key.
func TestMarkFromTheDetailScreen(t *testing.T) {
	m, _ := markModel(t)
	m.screen, m.current = screenDetail, &youtrack.Issue{ID: "PAY-2", Summary: "two"}
	m.w, m.h = 120, 40

	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := markedIDs(m); len(got) != 1 || got[0] != "PAY-2" {
		t.Fatalf("marked = %v, want [PAY-2]", got)
	}
	if !strings.Contains(m.header(), "marked") {
		t.Errorf("header does not mention the mark:\n%s", m.header())
	}

	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if strings.Contains(m.header(), "marked") {
		t.Errorf("header still mentions a mark that was taken back:\n%s", m.header())
	}
}

// Marks are only useful if they can be found again: the filters screen grows a
// synthetic "Marked" entry listing exactly what is ticked, which is also where
// they get cleared. It appears with the first mark and goes with the last.
func TestMarkedFilterListsWhatIsTicked(t *testing.T) {
	m, _ := markModel(t)
	m.savedQueries = append(append([]youtrack.SavedQuery(nil), builtinFilters...),
		youtrack.SavedQuery{ID: "145-3", Name: "Sprint 42", Query: "Sprint: {Sprint 42}"})
	m.refreshMarkedFilter()

	if names := filterNames(m); len(names) != 3 {
		t.Fatalf("with nothing marked the list is %v, want no Marked filter", names)
	}

	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.issues.Select(1)
	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})

	var marked *youtrack.SavedQuery
	for i, q := range m.savedQueries {
		if q.ID == markedFilterID {
			marked = &m.savedQueries[i]
		}
	}
	if marked == nil {
		t.Fatalf("no Marked filter after marking, got %v", filterNames(m))
	}
	if marked.Query != "issue id: PAY-1, PAY-2" {
		t.Errorf("query = %q, want both IDs", marked.Query)
	}
	// After the built-ins, before the instance's own saved searches.
	if got := filterNames(m); got[len(builtinFilters)] != "Marked" {
		t.Errorf("filters = %v, want Marked after the built-ins", got)
	}

	// Unticking both takes the filter away rather than leaving an empty one.
	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.issues.Select(0)
	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	for _, q := range m.savedQueries {
		if q.ID == markedFilterID {
			t.Fatalf("Marked filter outlived the last mark: %q", q.Query)
		}
	}
}
