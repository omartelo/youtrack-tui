package ui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

func TestApplySortReplacesTheClause(t *testing.T) {
	cases := []struct{ query, clause, want string }{
		{"#Unresolved", "", "#Unresolved"},
		{"#Unresolved", "updated desc", "#Unresolved sort by: updated desc"},
		// Cycling must replace, never stack: YouTrack takes one clause.
		{"#Unresolved sort by: updated desc", "created asc", "#Unresolved sort by: created asc"},
		// A filter written by hand can spell it any way.
		{"for: me Sort By: priority desc", "updated asc", "for: me sort by: updated asc"},
	}
	for _, c := range cases {
		if got := applySort(c.query, c.clause); got != c.want {
			t.Errorf("applySort(%q, %q) = %q, want %q", c.query, c.clause, got, c.want)
		}
	}
}

// `-q` has to reach the instance under a query typed at `s`, and survive the
// sort clause `S` pushes onto it.
func TestScopeReachesTheInstance(t *testing.T) {
	queries := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query().Get("query")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: srv.URL, Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "#Logistica", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m.screen, m.query = screenIssues, "#Unresolved"

	press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})
	if got := <-queries; got != "(#Logistica) (#Unresolved) sort by: updated desc" {
		t.Errorf("`S` sent %q", got)
	}
	if m.query != "#Unresolved" {
		t.Errorf("m.query = %q, the scope leaked into what `s` is seeded with", m.query)
	}
}

// The clause has to reach the instance, not just the model: ordering is done
// by YouTrack, over the whole result set rather than the page on screen.
func TestSortKeyRefetchesOrdered(t *testing.T) {
	queries := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query().Get("query")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: srv.URL, Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m.screen, m.query = screenIssues, "#Unresolved"

	sortKey := tea.KeyPressMsg{Code: 'S', Text: "S"}
	press(m, sortKey)
	if got := <-queries; got != "#Unresolved sort by: updated desc" {
		t.Errorf("first `S` sent %q", got)
	}
	press(m, sortKey)
	if got := <-queries; got != "#Unresolved sort by: updated asc" {
		t.Errorf("second `S` sent %q", got)
	}

	// The header says which order is on, and m.query stays what the user asked
	// for so the next cycle rewrites rather than appends.
	m.w = 120
	if want := "sort by: updated asc"; !strings.Contains(m.header(), want) {
		t.Errorf("header does not carry %q", want)
	}
	if m.query != "#Unresolved" {
		t.Errorf("m.query = %q, want the clause kept out of it", m.query)
	}

	// Round to the start again: the filter's own order, nothing injected.
	var last string
	for range len(sortOrders) - 2 {
		press(m, sortKey)
		last = <-queries
	}
	if last != "#Unresolved" {
		t.Errorf("full cycle sent %q, want the query untouched", last)
	}
}

// `S` on an open issue reorders its comments and writes the choice back, so
// the next launch opens the same way round. The list is ordered in memory —
// every comment came down with the issue, so nothing is refetched.
func TestCommentOrderSeedsFromConfigAndToggles(t *testing.T) {
	cfg := &config.Config{
		CommentsNewestFirst: true,
		Providers:           []config.Provider{{Name: "acme", URL: "https://acme.example", Token: "perm:tok"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	m, err := New(cfg, "", "", path)
	if err != nil {
		t.Fatal(err)
	}
	m.w, m.h = 80, 24
	m.layout()

	press(m, detailMsg{
		gen:      m.gen,
		issue:    &youtrack.Issue{ID: "PAY-1"},
		comments: []youtrack.Comment{{Text: "oldest"}, {Text: "newest"}},
	})
	if got := m.comments[0].Text; got != "newest" {
		t.Errorf("comments_newest_first: first comment = %q, want %q", got, "newest")
	}

	press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})
	if got := m.comments[0].Text; got != "oldest" {
		t.Errorf("after `S`: first comment = %q, want %q", got, "oldest")
	}
	view := plain(m.detail.View())
	if i, j := strings.Index(view, "oldest"), strings.Index(view, "newest"); i < 0 || j < 0 || i > j {
		t.Errorf("the detail pane was not re-rendered in the new order:\n%s", view)
	}

	// The choice outlives the issue it was made on: the next one opens the
	// same way round.
	press(m, detailMsg{
		gen:      m.gen,
		issue:    &youtrack.Issue{ID: "PAY-2"},
		comments: []youtrack.Comment{{Text: "oldest"}, {Text: "newest"}},
	})
	if got := m.comments[0].Text; got != "oldest" {
		t.Errorf("next issue: first comment = %q, want the toggled order", got)
	}

	// And outlives the program: `S` writes the config, like `f` and `w` do.
	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("`S` did not write the config: %v", err)
	}
	if saved.CommentsNewestFirst {
		t.Error("comments_newest_first was not written back as false")
	}
}
