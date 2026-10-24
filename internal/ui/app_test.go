package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Bracketed paste arrives as tea.PasteMsg, not as key presses. Routing only
// KeyPressMsg drops every paste on the floor, which makes the token field
// unusable — nobody types a permanent token by hand.
func TestPasteReachesTheSetupForm(t *testing.T) {
	m, err := New(nil, "", "", "/tmp/youtrack-tui-test.yml")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.Init()

	const token = "perm:AAAA.BBBB.CCCC"
	m.Update(tea.PasteMsg{Content: "acme"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.PasteMsg{Content: token})

	if got := m.setup.value(fieldName); got != "acme" {
		t.Errorf("name field = %q, want %q", got, "acme")
	}
	if got := m.setup.value(fieldToken); got != token {
		t.Errorf("token field = %q, want %q", got, token)
	}
}

// The form owns every key while it is up, otherwise typing a provider named
// "prod" would fire the next-provider and reload commands.
func TestSetupFormSwallowsGlobalKeys(t *testing.T) {
	m, err := New(nil, "", "", "/tmp/youtrack-tui-test.yml")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.Init()

	for _, r := range "prod" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.setup.value(fieldName); got != "prod" {
		t.Errorf("name field = %q, want %q", got, "prod")
	}
	if m.screen != screenSetup {
		t.Errorf("screen = %v, want screenSetup", m.screen)
	}
}
