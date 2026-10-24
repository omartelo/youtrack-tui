package ui

import (
	"path/filepath"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// press drives the model the way the runtime does: every command an Update
// returns is executed and its message fed back in. Calling Update alone is not
// enough — the bubbles that answer their own commands would never see the
// answer, which is exactly the bug this file guards.
func press(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	drain(m, cmd, 0)
}

func drain(m *Model, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 3 {
		return
	}
	// Timer commands (spinner ticks, status-message expiry) sleep inside the
	// command. Nothing here depends on them, so give up rather than wait.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var out tea.Msg
	select {
	case out = <-done:
	case <-time.After(20 * time.Millisecond):
		return
	}

	switch out := out.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range out {
			drain(m, c, depth+1)
		}
	default:
		_, next := m.Update(out)
		drain(m, next, depth+1)
	}
}

func visibleIssueIDs(m *Model) []string {
	out := []string{}
	for _, it := range m.issues.VisibleItems() {
		out = append(out, it.(issueItem).issue.ID)
	}
	return out
}

func TestIssueListFilterApplies(t *testing.T) {
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
	m.screen = screenIssues
	m.issues.SetItems([]list.Item{
		issueItem{issue: youtrack.Issue{
			ID:      "LOG-1538",
			Summary: "[Auditoria][SEGURANÇA] AutotracHelper.java:286 — TrustManager aceita qualquer certificado TLS",
		}},
		issueItem{issue: youtrack.Issue{
			ID:      "AGRO-73",
			Summary: `Tipo de Movimento "P-Pedido de Venda" não estão tendo crítica ao Confirmar`,
		}},
	})

	press(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.issues.SettingFilter() {
		t.Fatal("`/` did not open the filter input")
	}
	for _, r := range "AGRO-73" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	if got := visibleIssueIDs(m); len(got) != 1 || got[0] != "AGRO-73" {
		t.Errorf("while typing, visible = %v, want [AGRO-73]", got)
	}

	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.issues.FilterState() != list.FilterApplied {
		t.Errorf("enter left the filter in state %v", m.issues.FilterState())
	}
	if got := visibleIssueIDs(m); len(got) != 1 || got[0] != "AGRO-73" {
		t.Errorf("after enter, visible = %v, want [AGRO-73]", got)
	}

	// esc clears the filter instead of leaving the screen: otherwise there is
	// no way back to the full list.
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != screenIssues {
		t.Errorf("esc left the issues screen while a filter was applied")
	}
	if got := visibleIssueIDs(m); len(got) != 2 {
		t.Errorf("esc did not clear the filter, visible = %v", got)
	}

	// With no filter left, esc goes back as usual.
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != screenFilters {
		t.Errorf("second esc did not go back, screen = %v", m.screen)
	}
}
