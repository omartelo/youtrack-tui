package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// The header names the saved search as well as the clause it stands for: the
// query alone says what is being asked, not which filter asked it. A raw query
// typed at the `s` prompt has no name, and must not inherit the last one.
func TestHeaderNamesTheFilter(t *testing.T) {
	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m.w, m.screen = 200, screenFilters
	m.filters.SetItems([]list.Item{filterItem{SavedQuery: youtrack.SavedQuery{
		ID: "145-3", Name: "TO DEPLOY", Query: "#{TO DEPLOY} Sprint: {2026 - 08/1}",
	}}})

	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	// The request never answers in a test; the header would say "loading…"
	// instead of what it is loading.
	m.loading = false
	head := plain(m.header())
	if !strings.Contains(head, "TO DEPLOY  #{TO DEPLOY} Sprint: {2026 - 08/1}") {
		t.Errorf("header does not carry the filter name beside its query:\n%s", head)
	}

	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	for _, r := range "project: LOG" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.loading = false
	// The prompt prefills with the running query, so the text still mentions
	// the filter — the name beside it is what must be gone.
	if m.queryName != "" {
		t.Errorf("a raw query kept the previous filter's name: %q", m.queryName)
	}
}

// A long query plus a name must not push the row past the terminal width —
// lipgloss wraps the overflow under the title and the header becomes two rows.
func TestHeaderFitsTheWidth(t *testing.T) {
	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m.w = 80
	m.queryName = "TO DEPLOY"
	m.query = strings.Repeat("Sprint: {2026 - 08/1} ", 10)

	for i, line := range strings.Split(plain(m.header()), "\n") {
		if w := len([]rune(line)); w > m.w {
			t.Errorf("header line %d is %d columns wide, want <= %d", i, w, m.w)
		}
	}
}
