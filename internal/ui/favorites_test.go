package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

func filterNames(m *Model) []string {
	out := make([]string, 0, len(m.filters.Items()))
	for _, li := range m.filters.Items() {
		out = append(out, li.(filterItem).Name)
	}
	return out
}

func selectFilter(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, li := range m.filters.Items() {
		if f, ok := li.(filterItem); ok && f.Name == name {
			m.filters.Select(i)
			return
		}
	}
	t.Fatalf("filter %q is not in the list: %v", name, filterNames(m))
}

func wantOrder(t *testing.T, m *Model, want ...string) {
	t.Helper()
	if got := filterNames(m); !strings.EqualFold(strings.Join(got, "|"), strings.Join(want, "|")) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFavoritesSortToTopAndPersist(t *testing.T) {
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
	m.savedQueries = []youtrack.SavedQuery{
		{ID: "builtin:my-open-issues", Name: "My open issues", Query: "for: me #Unresolved"},
		{ID: "builtin:all-unresolved", Name: "All unresolved", Query: "#Unresolved"},
		{ID: "145-3", Name: "Sprint 42", Query: "Sprint: {Sprint 42}"},
		{ID: "145-9", Name: "Waiting on me", Query: "State: {Waiting for review}"},
	}
	m.setFilterItems()

	selectFilter(t, m, "Sprint 42")
	m.toggleFavorite()
	if m.dlg != nil {
		t.Fatalf("unexpected dialog: %s", m.dlg.body)
	}
	wantOrder(t, m, "Sprint 42", "My open issues", "All unresolved", "Waiting on me")
	if !m.filters.Items()[0].(filterItem).fav {
		t.Error("pinned item is not marked as a favourite")
	}
	if got := m.filters.Index(); got != 0 {
		t.Errorf("cursor = %d, want it to follow the pinned item to 0", got)
	}

	// A second pin lands below the first: pin order, then API order.
	selectFilter(t, m, "Waiting on me")
	m.toggleFavorite()
	wantOrder(t, m, "Sprint 42", "Waiting on me", "My open issues", "All unresolved")

	raw := readConfig(t, path)
	if !strings.Contains(raw, "- 145-3") || !strings.Contains(raw, "- 145-9") {
		t.Errorf("favourites not persisted as IDs:\n%s", raw)
	}

	// Unpinning sends it back to where YouTrack had it.
	selectFilter(t, m, "Sprint 42")
	m.toggleFavorite()
	wantOrder(t, m, "Waiting on me", "My open issues", "All unresolved", "Sprint 42")
	if strings.Contains(readConfig(t, path), "- 145-3") {
		t.Error("unpinned filter is still in the config")
	}
}

// The whole reason favourites moved from names to IDs.
func TestFavoritesSurviveARename(t *testing.T) {
	m := testModel(t, 50)
	m.cfg.Providers[0].Favorites = []string{"145-3"}
	m.savedQueries = []youtrack.SavedQuery{
		{ID: "145-9", Name: "Waiting on me"},
		{ID: "145-3", Name: "Sprint 43 — backend"}, // renamed in YouTrack
	}
	m.setFilterItems()

	if got := filterNames(m)[0]; got != "Sprint 43 — backend" {
		t.Errorf("first filter = %q, want the renamed pin still on top", got)
	}
}

// Two searches sharing a name used to toggle together.
func TestFavoritesDoNotCollideOnName(t *testing.T) {
	m := testModel(t, 50)
	m.savedQueries = []youtrack.SavedQuery{
		{ID: "builtin:all-unresolved", Name: "All unresolved"},
		{ID: "145-7", Name: "All unresolved"}, // a saved search with the same name
	}
	m.setFilterItems()

	m.filters.Select(1)
	m.toggleFavorite()

	items := m.filters.Items()
	if !items[0].(filterItem).fav || items[0].(filterItem).ID != "145-7" {
		t.Fatalf("pinned the wrong one: %+v", items[0].(filterItem).SavedQuery)
	}
	if items[1].(filterItem).fav {
		t.Error("the built-in sharing the name was pinned too")
	}
}

// Configs written before IDs still work, and heal on the next pin.
func TestFavoritesMigrateFromNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
		Favorites: []string{"Sprint 42", "Deleted search"},
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	press(m, filtersMsg{gen: m.gen, queries: []youtrack.SavedQuery{
		{ID: "builtin:all-unresolved", Name: "All unresolved"},
		{ID: "145-3", Name: "Sprint 42"},
	}})

	if got := filterNames(m)[0]; got != "Sprint 42" {
		t.Errorf("a name-based favourite stopped working: order %v", filterNames(m))
	}
	got := m.cfg.Providers[0].Favorites
	if len(got) != 2 || got[0] != "145-3" {
		t.Errorf("favourites = %v, want the name rewritten to its ID", got)
	}
	if got[1] != "Deleted search" {
		t.Errorf("an unresolvable favourite was dropped: %v", got)
	}
}
