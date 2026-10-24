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

func TestScopedNarrowsEveryQuery(t *testing.T) {
	cases := []struct{ base, query, want string }{
		{"", "#Unresolved", "#Unresolved"},
		{"#Logistica", "", "(#Logistica)"},
		{"#Logistica", "for: me #Unresolved", "(#Logistica) (for: me #Unresolved)"},
		// `or` binds looser than juxtaposition: unparenthesised, B escapes.
		{"#Logistica", "A or B", "(#Logistica) (A or B)"},
		// The ordering clause has to stay last, outside the parentheses.
		{"#Logistica", "#Unresolved sort by: created desc", "(#Logistica) (#Unresolved) sort by: created desc"},
		{"#Logistica", "sort by: created desc", "(#Logistica) sort by: created desc"},
		// An order in the scope is a default: the filter's own one wins.
		{"#Logistica sort by: priority desc", "#Unresolved", "(#Logistica) (#Unresolved) sort by: priority desc"},
		{"#Logistica sort by: priority desc", "#Unresolved sort by: created asc", "(#Logistica) (#Unresolved) sort by: created asc"},
	}
	for _, c := range cases {
		if got := scoped(c.base, c.query); got != c.want {
			t.Errorf("scoped(%q, %q) = %q, want %q", c.base, c.query, got, c.want)
		}
	}
}
