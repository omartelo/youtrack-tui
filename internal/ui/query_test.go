package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The prompt has to own the keyboard while it is open: "for: me" contains f, o,
// r and s, every one of them a command on the screen underneath.
func TestQueryPromptSwallowsCommandKeys(t *testing.T) {
	m := testModel(t, 50)
	m.w, m.h = 100, 30
	m.layout()

	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if !m.prompt.active {
		t.Fatal("`s` did not open the query prompt")
	}

	for _, r := range "for: me #Unresolved" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.prompt.value(); got != "for: me #Unresolved" {
		t.Fatalf("prompt value = %q", got)
	}
	if m.screen != screenFilters {
		t.Errorf("typing navigated away, screen = %v", m.screen)
	}
	if m.dlg != nil {
		t.Errorf("typing raised a dialog: %s", m.dlg.title)
	}

	// The prompt eats a row while it is open.
	if got := m.prompt.lines(); got != 1 {
		t.Errorf("prompt.lines() = %d while open", got)
	}
}

func TestQueryPromptRunAndCancel(t *testing.T) {
	m := testModel(t, 50)
	m.w, m.h = 100, 30
	m.layout()
	tall := m.filters.Height()

	// esc cancels without touching the query in play.
	m.query = "#Unresolved"
	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.filters.Height() != tall-1 {
		t.Errorf("opening the prompt did not shrink the list: %d then %d", tall, m.filters.Height())
	}
	if got := m.prompt.value(); got != "#Unresolved" {
		t.Errorf("prompt was not seeded with the current query, got %q", got)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.prompt.active {
		t.Error("esc left the prompt open")
	}
	if m.query != "#Unresolved" {
		t.Errorf("esc changed the query to %q", m.query)
	}
	if m.filters.Height() != tall {
		t.Error("closing the prompt did not give the row back")
	}

	// enter runs it.
	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	m.prompt.input.SetValue("project: PAY")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.prompt.active {
		t.Error("enter left the prompt open")
	}
	if m.query != "project: PAY" {
		t.Errorf("query = %q, want the typed one", m.query)
	}
	if cmd == nil {
		t.Error("enter did not fire a request")
	}

	// An empty query is not a request.
	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	m.prompt.input.SetValue("   ")
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("a blank query fired a request")
	}
}

// Paste is how a long query actually gets in.
func TestQueryPromptAcceptsPaste(t *testing.T) {
	m := testModel(t, 50)
	m.w, m.h = 100, 30
	m.layout()
	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	press(m, tea.PasteMsg{Content: "Assignee: me State: -Resolved"})
	if got := m.prompt.value(); got != "Assignee: me State: -Resolved" {
		t.Errorf("pasted query = %q", got)
	}
}

// The shapes below were checked against a live instance: two parenthesised
// groups side by side are a parse error, as is a `sort by:` after a `)`, and
// of two sort clauses the first one wins.
func TestScopedNarrowsEveryQuery(t *testing.T) {
	cases := []struct{ base, query, want string }{
		{"", "#Unresolved", "#Unresolved"},
		{"#Logistica", "", "(#Logistica)"},
		{"#Logistica", "for: me #Unresolved", "(#Logistica) and (for: me #Unresolved)"},
		// `or` binds looser than juxtaposition: unparenthesised, B escapes.
		{"#Logistica", "A or B", "(#Logistica) and (A or B)"},
		// The filter's own order stays inside its group, which YouTrack takes.
		{"#Logistica", "#Unresolved sort by: created desc", "(#Logistica) and (#Unresolved sort by: created desc)"},
		{"#Logistica", "#REVIEW ordenar por: projeto", "(#Logistica) and (#REVIEW ordenar por: projeto)"},
		// The scope's order goes last, so any clause of the filter's comes
		// first and wins — localised ones included.
		{"#Logistica sort by: priority desc", "#Unresolved", "(#Logistica) and (#Unresolved sort by: priority desc)"},
		{"#Logistica sort by: priority desc", "#Unresolved sort by: created asc", "(#Logistica) and (#Unresolved sort by: created asc sort by: priority desc)"},
		{"#Logistica sort by: priority desc", "", "(#Logistica) and (sort by: priority desc)"},
		{"sort by: priority desc", "#Unresolved", "#Unresolved sort by: priority desc"},
	}
	for _, c := range cases {
		if got := scoped(c.base, c.query); got != c.want {
			t.Errorf("scoped(%q, %q) = %q, want %q", c.base, c.query, got, c.want)
		}
	}
}
