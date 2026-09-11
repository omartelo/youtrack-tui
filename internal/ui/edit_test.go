package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// customFieldsBody covers every branch of the fence: a bundle field with an
// archived value the instance would refuse, a user field whose bundle carries
// users rather than values and one of them banned, and a text field, which has
// no list of legal answers and is therefore not offered at all.
const customFieldsBody = `[
	{"name":"Fila","$type":"StateIssueCustomField","value":{"name":"IN DEV"},
	 "projectCustomField":{"bundle":{"values":[
		{"name":"IN DEV"},{"name":"TO TEST"},{"name":"RETIRADA","archived":true}]}}},
	{"name":"Atribuído","$type":"SingleUserIssueCustomField","value":{"login":"ruy.mendes"},
	 "projectCustomField":{"bundle":{"aggregatedUsers":[
		{"login":"ruy.mendes"},
		{"login":"vgoncalves","fullName":"Victor Gonçalves"},
		{"login":"ex.funcionario","fullName":"Quem Saiu","banned":true}]}}},
	{"name":"Documentação","$type":"TextIssueCustomField","value":{"text":"No documentação"}}
]`

// editModel points a model at a stub instance and hands back whatever body the
// stub was POSTed, so the test can read what went over the wire.
func editModel(t *testing.T) (*Model, *string) {
	t.Helper()
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			posted = string(b)
			_, _ = w.Write([]byte(`{"idReadable":"PAY-1"}`))
		case strings.HasSuffix(r.URL.Path, "/customFields"):
			_, _ = w.Write([]byte(customFieldsBody))
		case strings.HasSuffix(r.URL.Path, "/comments"), strings.HasSuffix(r.URL.Path, "/activities"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"idReadable":"PAY-1","summary":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{Providers: []config.Provider{{
		Name: "acme", URL: srv.URL, Token: "perm:tok",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg, "", filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m.screen = screenDetail
	m.current = &youtrack.Issue{ID: "PAY-1", Summary: "x"}
	return m, &posted
}

// `e` must offer only the fields with a list of legal answers behind them, and
// only the values the instance still accepts.
func TestEditOffersBundleFieldsOnly(t *testing.T) {
	m, _ := editModel(t)

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	if m.screen != screenEditField {
		t.Fatalf("screen = %v, want the field picker", m.screen)
	}
	if len(m.editFields) != 2 || m.editFields[0].Name != "Fila" || m.editFields[1].Name != "Atribuído" {
		t.Fatalf("editable fields = %+v, want the bundle and the user one", m.editFields)
	}
	if got := m.editFields[0].Options; len(got) != 2 || got[1].Value != "TO TEST" {
		t.Fatalf("options = %v, want the two live ones", got)
	}
	// A user reads as a name and is written as a login, and a banned one is to
	// a user bundle what an archived value is to any other.
	want := []youtrack.Option{
		{Label: "ruy.mendes", Value: "ruy.mendes"},
		{Label: "Victor Gonçalves", Value: "vgoncalves"},
	}
	if got := m.editFields[1].Options; !slices.Equal(got, want) {
		t.Fatalf("user options = %v, want %v", got, want)
	}
}

// A user field is a closed set like any other, only keyed differently: the
// picker reads as names and the write goes by login.
func TestEditWritesAUserByLogin(t *testing.T) {
	m, posted := editModel(t)

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	press(m, tea.KeyPressMsg{Code: tea.KeyDown}) // Fila -> Atribuído
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, tea.KeyPressMsg{Code: tea.KeyDown}) // ruy.mendes -> Victor Gonçalves
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	for _, want := range []string{`"$type":"SingleUserIssueCustomField"`, `"login":"vgoncalves"`} {
		if !strings.Contains(*posted, want) {
			t.Errorf("POST body %s does not carry %s", *posted, want)
		}
	}
}

// The whole point: pick a field, pick a value, and the instance is told in the
// shape it accepts — name plus the $type that says which bundle to look it up
// in. Nothing else in the program sends a POST.
func TestEditWritesTheChosenValue(t *testing.T) {
	m, posted := editModel(t)

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // Fila
	press(m, tea.KeyPressMsg{Code: tea.KeyDown})  // IN DEV -> TO TEST
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	for _, want := range []string{`"name":"Fila"`, `"$type":"StateIssueCustomField"`, `"name":"TO TEST"`} {
		if !strings.Contains(*posted, want) {
			t.Errorf("POST body %s does not carry %s", *posted, want)
		}
	}
	if m.screen != screenDetail {
		t.Errorf("screen = %v, want back on the issue", m.screen)
	}
}

// Choosing the value that is already there is not a write: a POST that changes
// nothing still runs the instance's workflow.
func TestEditSkipsAWriteThatChangesNothing(t *testing.T) {
	m, posted := editModel(t)

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // Fila
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // still IN DEV

	if *posted != "" {
		t.Errorf("posted %s, want no request at all", *posted)
	}
	if m.screen != screenDetail {
		t.Errorf("screen = %v, want back on the issue", m.screen)
	}
}

// esc on the value picker goes back to the fields, not out of the editor.
func TestEditEscapesOneStepAtATime(t *testing.T) {
	m, _ := editModel(t)

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != screenEditField {
		t.Fatalf("screen = %v, want the field picker", m.screen)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != screenDetail {
		t.Fatalf("screen = %v, want the issue", m.screen)
	}
}
