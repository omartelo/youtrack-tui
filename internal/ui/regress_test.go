package ui

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// certError is what a server presenting an untrusted certificate produces.
func certError() error {
	return fmt.Errorf("GET /savedQueries: %w", x509.UnknownAuthorityError{})
}

// list.Items() is the unfiltered order while Select indexes the visible one.
// Following the pinned item across both spaces landed the cursor on a
// different row, so the next `f` unpinned something the user never picked.
func TestPinningWhileFilteredKeepsTheCursor(t *testing.T) {
	m := testModel(t, 50)
	m.w, m.h = 100, 30
	m.layout()
	m.savedQueries = []youtrack.SavedQuery{
		{ID: "1", Name: "alpha"}, {ID: "2", Name: "beta"}, {ID: "3", Name: "gamma"},
		{ID: "4", Name: "delta zz"}, {ID: "5", Name: "zeta zz"},
	}
	m.setFilterItems()

	press(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "zz" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.filters.VisibleItems()) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.filters.VisibleItems()))
	}

	m.filters.Select(1)
	picked := m.filters.SelectedItem().(filterItem)
	press(m, tea.KeyPressMsg{Code: 'f', Text: "f"})

	sel := m.filters.SelectedItem()
	if sel == nil {
		t.Fatal("nothing is selected after pinning")
	}
	if got := sel.(filterItem); got.ID != picked.ID {
		t.Errorf("cursor moved from %q to %q", picked.Name, got.Name)
	}
	if fav := m.cfg.Providers[0].Favorites; len(fav) != 1 || fav[0] != picked.ID {
		t.Errorf("favourites = %v, want just %q", fav, picked.ID)
	}
}

// Markers must not outlive the watching that produced them.
func TestUnwatchingClearsItsMarkers(t *testing.T) {
	m := testModel(t, 50)
	m.savedQueries = []youtrack.SavedQuery{
		{ID: "a", Name: "filter A", Query: "q"},
		{ID: "b", Name: "filter B", Query: "q"},
	}
	m.watch.watching["a"], m.watch.watching["b"] = true, true
	m.setFilterItems()

	m.watch.record("a", nil)
	m.watch.record("b", nil)
	m.watch.record("a", []youtrack.Issue{{ID: "PAY-1"}})
	m.watch.record("b", []youtrack.Issue{{ID: "PAY-2"}})
	if !m.watch.isFresh("PAY-1") || !m.watch.isFresh("PAY-2") {
		t.Fatal("both arrivals should be marked")
	}

	selectFilter(t, m, "filter A")
	m.toggleWatch()

	if m.watch.isFresh("PAY-1") {
		t.Error("unwatching left its marker behind")
	}
	if !m.watch.isFresh("PAY-2") {
		t.Error("unwatching one filter cleared another filter's marker")
	}
	if m.watch.watching["a"] {
		t.Error("the filter is still being watched")
	}
}

// Offset paging: an issue added to the filter between two requests shifts the
// window, so the next page repeats what the previous one already showed.
func TestOverlappingPagesDoNotDuplicate(t *testing.T) {
	m := testModel(t, 3)
	m.query = "#Unresolved"

	m.Update(issuesMsg{gen: m.gen, issues: page(3, 1)})
	// The window slid by one: PAY-3 comes back alongside two genuinely new ones.
	m.Update(issuesMsg{gen: m.gen, issues: page(3, 3), appendTo: true})

	seen := map[string]int{}
	for _, li := range m.issues.Items() {
		seen[li.(issueItem).issue.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times", id, n)
		}
	}
	if len(seen) != 5 {
		t.Errorf("got %d distinct issues, want 5", len(seen))
	}
}

// The first-run write path: a rejected token leaves nothing on disk, and a
// good one persists the reference rather than the resolved secret.
func TestSubmitSetupWritesOnlyAfterTheAPIAnswers(t *testing.T) {
	t.Setenv("YT_TOKEN", "perm:secret")
	path := filepath.Join(t.TempDir(), "config.yml")
	m, err := New(nil, "", "", path)
	if err != nil {
		t.Fatal(err)
	}
	if m.screen != screenSetup {
		t.Fatal("a nil config should open the setup screen")
	}

	m.setup.inputs[fieldName].SetValue("acme")
	m.setup.inputs[fieldURL].SetValue("https://acme.youtrack.cloud")
	m.setup.inputs[fieldToken].SetValue("${YT_TOKEN}")
	if cmd := m.submitSetup(); cmd == nil {
		t.Fatalf("submit did not fire a request: %v", m.dlg)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the config was written before the credentials were checked")
	}

	// The rejection path writes nothing and stays on the form.
	m.Update(errMsg{gen: m.gen, err: errors.New("HTTP 401")})
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a rejected token still wrote the config")
	}
	if m.screen != screenSetup || m.dlg == nil {
		t.Errorf("a rejection should stay on the form with the reason shown")
	}

	// The accepting path writes it, with the ${VAR} intact.
	m.dlg = nil
	m.Update(filtersMsg{gen: m.gen, queries: nil})
	if m.screen != screenFilters {
		t.Errorf("screen = %v, want the filters list", m.screen)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the config was not written: %v", err)
	}
	if !strings.Contains(string(raw), "${YT_TOKEN}") || strings.Contains(string(raw), "perm:secret") {
		t.Errorf("the resolved secret leaked into the file:\n%s", raw)
	}
}

// Accepting an untrusted certificate downgrades this provider only, and says
// so for the rest of the session.
func TestTrustAnywayIsScopedAndVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	cfg := &config.Config{Providers: []config.Provider{
		{Name: "internal", URL: "https://a.io", Token: "t"},
		{Name: "other", URL: "https://b.io", Token: "t"},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", "", path)
	if err != nil {
		t.Fatal(err)
	}

	m.dlg = errorDialog(certError())
	if !m.dlg.offerTrust {
		t.Fatal("a certificate failure must offer the way out")
	}
	// Any other key leaves the downgrade untaken.
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.insecure() {
		t.Fatal("a stray key accepted the certificate")
	}

	m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if !m.cfg.Providers[0].Insecure {
		t.Error("the provider was not downgraded")
	}
	if m.cfg.Providers[1].Insecure {
		t.Error("the downgrade leaked to the other provider")
	}
	if !m.insecure() {
		t.Error("the header would not show !insecure")
	}
	if m.dlg != nil {
		t.Error("the dialog stayed up after being answered")
	}
}
