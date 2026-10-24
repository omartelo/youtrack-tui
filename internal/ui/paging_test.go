package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

func testModel(t *testing.T, pageSize int) *Model {
	t.Helper()
	cfg := &config.Config{PageSize: pageSize, Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func page(n int, from int) []youtrack.Issue {
	out := make([]youtrack.Issue, n)
	for i := range out {
		out[i] = youtrack.Issue{ID: fmt.Sprintf("PAY-%d", from+i), Summary: "x"}
	}
	return out
}

func TestIssuePagesAppend(t *testing.T) {
	m := testModel(t, 3)
	m.query = "#Unresolved"

	// A full page means there may be more.
	m.Update(issuesMsg{gen: m.gen, issues: page(3, 1)})
	if !m.moreIssues || !m.keys.More.Enabled() {
		t.Fatal("a full page should offer `m` to load more")
	}
	if got := len(m.issues.Items()); got != 3 {
		t.Fatalf("items = %d, want 3", got)
	}

	// Loading more keeps the cursor where it was instead of jumping.
	m.issues.Select(2)
	m.Update(issuesMsg{gen: m.gen, issues: page(3, 4), appendTo: true})
	if got := len(m.issues.Items()); got != 6 {
		t.Errorf("after page 2, items = %d, want 6", got)
	}
	if got := m.issues.Index(); got != 2 {
		t.Errorf("cursor moved to %d, want it to stay at 2", got)
	}
	if got := m.issues.Items()[3].(issueItem).issue.ID; got != "PAY-4" {
		t.Errorf("page 2 landed wrong: item 3 = %q", got)
	}

	// A short page is the end: the key goes away.
	m.Update(issuesMsg{gen: m.gen, issues: page(1, 7), appendTo: true})
	if m.moreIssues || m.keys.More.Enabled() {
		t.Error("a short page should retire the `m` key")
	}
	if got := len(m.issues.Items()); got != 7 {
		t.Errorf("final items = %d, want 7", got)
	}
	if cmd := m.loadMoreIssues(); cmd != nil {
		t.Error("loadMoreIssues still fired after the last page")
	}
}

func TestFreshQueryReplacesPages(t *testing.T) {
	m := testModel(t, 3)
	m.query = "#Unresolved"
	m.Update(issuesMsg{gen: m.gen, issues: page(3, 1)})
	m.Update(issuesMsg{gen: m.gen, issues: page(3, 4), appendTo: true})
	m.issues.Select(5)

	// Not appendTo: the accumulated pages are dropped, not added to.
	m.Update(issuesMsg{gen: m.gen, issues: page(2, 100)})
	if got := len(m.issues.Items()); got != 2 {
		t.Errorf("items = %d, want 2 — a new query must replace the pages", got)
	}
	if got := m.issues.Index(); got != 0 {
		t.Errorf("cursor = %d, want 0 on a fresh query", got)
	}
}

func TestLoadMoreNeedsAQuery(t *testing.T) {
	m := testModel(t, 3)
	m.moreIssues = true
	if cmd := m.loadMoreIssues(); cmd != nil {
		t.Error("loadMoreIssues fired with no query set")
	}
	// The key is inert outside the issue list.
	m.screen = screenFilters
	if _, cmd := m.onKey(tea.KeyPressMsg{Code: 'm', Text: "m"}); cmd != nil {
		t.Error("`m` did something on the filters screen")
	}
}
