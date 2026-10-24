package ui

import (
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/list"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// launch hands the URL to a desktop-wide dispatcher, so the scheme is a trust
// boundary even though every URL we build is https today.
func TestLaunchRejectsNonHTTP(t *testing.T) {
	for _, bad := range []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://example.com/x",
		"",
		"/etc/passwd",
	} {
		if err := launch(bad); err == nil {
			t.Errorf("launch(%q) was allowed", bad)
		}
	}
}

func TestSelectedIssueID(t *testing.T) {
	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: "https://acme.youtrack.cloud", Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The filters screen has no issue to open.
	if got := m.selectedIssueID(); got != "" {
		t.Errorf("on filters, selectedIssueID = %q, want empty", got)
	}

	m.issues.SetItems([]list.Item{
		issueItem{issue: youtrack.Issue{ID: "PAY-1", Summary: "one"}},
		issueItem{issue: youtrack.Issue{ID: "PAY-2", Summary: "two"}},
	})
	m.screen = screenIssues
	m.issues.Select(1)
	if got := m.selectedIssueID(); got != "PAY-2" {
		t.Errorf("on the list, selectedIssueID = %q, want PAY-2", got)
	}

	m.screen = screenDetail
	m.current = &youtrack.Issue{ID: "PAY-9"}
	if got := m.selectedIssueID(); got != "PAY-9" {
		t.Errorf("on detail, selectedIssueID = %q, want PAY-9", got)
	}

	// Detail with nothing loaded must not open the list's selection instead.
	m.current = nil
	if got := m.selectedIssueID(); got != "" {
		t.Errorf("on empty detail, selectedIssueID = %q, want empty", got)
	}

	if got := m.client.IssueURL("PAY-2"); got != "https://acme.youtrack.cloud/issue/PAY-2" {
		t.Errorf("IssueURL = %q", got)
	}
}
