package youtrack

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A YouTrack instance can name and type its fields however it likes, so the
// renderer has to survive every value shape the API emits.
func TestCustomFieldString(t *testing.T) {
	const dueMillis = 1700000000000
	cases := []struct {
		name string
		json string
		want string
	}{
		{"enum", `{"name":"State","$type":"StateIssueCustomField","value":{"name":"In Progress"}}`, "In Progress"},
		{"user", `{"name":"Assignee","$type":"SingleUserIssueCustomField","value":{"login":"ana","fullName":"Ana Souza"}}`, "Ana Souza"},
		{"user without full name", `{"name":"Assignee","$type":"SingleUserIssueCustomField","value":{"login":"ana"}}`, "ana"},
		{"multi value", `{"name":"Tags","$type":"MultiEnumIssueCustomField","value":[{"name":"a"},{"name":"b"}]}`, "a, b"},
		{"null value", `{"name":"Due","$type":"DateIssueCustomField","value":null}`, ""},
		{"absent value", `{"name":"Due","$type":"DateIssueCustomField"}`, ""},
		{"scalar", `{"name":"Votes","$type":"SimpleIssueCustomField","value":7}`, "7"},
		{"text", `{"name":"Notes","$type":"TextIssueCustomField","value":{"text":"hi"}}`, "hi"},
		{"bool", `{"name":"Flag","$type":"SimpleIssueCustomField","value":true}`, "yes"},
		{"period", `{"name":"Spent","$type":"PeriodIssueCustomField","value":{"presentation":"3h 20m","minutes":200}}`, "3h 20m"},
		{"date", `{"name":"Due","$type":"DateIssueCustomField","value":1700000000000}`,
			time.UnixMilli(dueMillis).Format("2006-01-02 15:04")},
		{"object with no label key", `{"name":"X","$type":"SimpleIssueCustomField","value":{"id":"1-2"}}`, ""},
	}
	for _, tc := range cases {
		var f CustomField
		if err := json.Unmarshal([]byte(tc.json), &f); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := f.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// In a change a date, a period and an integer all come down as a bare number,
// and only the field's type tells them apart. Shapes taken from a live instance.
func TestChangeRendersBothSides(t *testing.T) {
	const due = 1789560000000
	cases := []struct {
		name, json, before, after string
	}{
		{"state", `{"field":{"presentation":"Fila","customField":{"fieldType":{"id":"state[1]"}}},
			"removed":[{"name":"Não Iniciada"}],"added":[{"name":"Em Andamento"}]}`, "Não Iniciada", "Em Andamento"},
		{"user from nobody", `{"field":{"customField":{"fieldType":{"id":"user[1]"}}},"removed":[],
			"added":[{"login":"victor.goncalves","fullName":"Victor Gonçalves","name":"Victor Gonçalves"}]}`, "", "Victor Gonçalves"},
		{"string cleared", `{"field":{"customField":{"fieldType":{"id":"string"}}},"removed":"4.35b855","added":null}`, "4.35b855", ""},
		{"period", `{"field":{"customField":{"fieldType":{"id":"period"}}},"removed":90,"added":960}`, "1h30m", "16h"},
		{"short period", `{"field":{"customField":{"fieldType":{"id":"period"}}},"removed":45}`, "45m", ""},
		{"date", `{"field":{"customField":{"fieldType":{"id":"date"}}},"added":1789560000000}`,
			"", time.UnixMilli(due).Format("2006-01-02 15:04")},
		{"integer stays a number", `{"field":{"customField":{"fieldType":{"id":"integer"}}},"added":45}`, "", "45"},
	}
	for _, tc := range cases {
		var c Change
		if err := json.Unmarshal([]byte(tc.json), &c); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := c.Before(); got != tc.before {
			t.Errorf("%s: before = %q, want %q", tc.name, got, tc.before)
		}
		if got := c.After(); got != tc.after {
			t.Errorf("%s: after = %q, want %q", tc.name, got, tc.after)
		}
	}
}

func TestLinkLabel(t *testing.T) {
	l := Link{Direction: "INWARD", LinkType: LinkType{Name: "Depend", SourceToTarget: "depends on", TargetToSource: "is required for"}}
	if got := l.Label(); got != "is required for" {
		t.Errorf("INWARD label = %q", got)
	}
	l.Direction = "OUTWARD"
	if got := l.Label(); got != "depends on" {
		t.Errorf("OUTWARD label = %q", got)
	}
}

func TestClientGET(t *testing.T) {
	var gotMethod, gotAuth, gotQuery, gotSkip, gotTop string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotMethod, gotAuth, gotQuery = r.Method, r.Header.Get("Authorization"), q.Get("query")
		gotSkip, gotTop = q.Get("$skip"), q.Get("$top")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"idReadable":"PRJ-1","summary":"hi"}]`))
	}))
	defer srv.Close()

	c, err := New(srv.URL+"/", "perm:tok", TLS{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	issues, err := c.Issues(t.Context(), "#Unresolved", 50, 10)
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET (read-only invariant)", gotMethod)
	}
	if gotAuth != "Bearer perm:tok" || gotQuery != "#Unresolved" {
		t.Errorf("auth=%q query=%q", gotAuth, gotQuery)
	}
	if gotSkip != "50" || gotTop != "10" {
		t.Errorf("paging: $skip=%q $top=%q, want 50 and 10", gotSkip, gotTop)
	}
	if len(issues) != 1 || issues[0].ID != "PRJ-1" {
		t.Errorf("issues = %+v", issues)
	}
	if got := c.AbsURL("/api/files/0-1?sign=x"); got != srv.URL+"/api/files/0-1?sign=x" {
		t.Errorf("AbsURL = %q", got)
	}
}

// An internal instance behind a private CA is the whole reason TLS options
// exist: the failure has to be recognisable, and both ways out have to work.
func TestSelfSignedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c, err := New(srv.URL, "perm:tok", TLS{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.SavedQueries(t.Context())
	if err == nil {
		t.Fatal("expected the untrusted certificate to be rejected")
	}
	if !IsCertError(err) {
		t.Errorf("IsCertError = false for %v — the dialog would not offer the way out", err)
	}
	if strings.Contains(err.Error(), "fields=") {
		t.Errorf("error still carries the query string: %v", err)
	}

	insecure, err := New(srv.URL, "perm:tok", TLS{Insecure: true})
	if err != nil {
		t.Fatalf("New insecure: %v", err)
	}
	if _, err := insecure.SavedQueries(t.Context()); err != nil {
		t.Errorf("insecure client still failed: %v", err)
	}

	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: srv.Certificate().Raw,
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	trusted, err := New(srv.URL, "perm:tok", TLS{CAFile: ca})
	if err != nil {
		t.Fatalf("New with ca_file: %v", err)
	}
	if _, err := trusted.SavedQueries(t.Context()); err != nil {
		t.Errorf("ca_file client failed: %v", err)
	}

	if _, err := New(srv.URL, "perm:tok", TLS{CAFile: "/nope.pem"}); err == nil {
		t.Error("a missing ca_file must fail loudly")
	}
}

func TestClientErrorHasNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized","error_description":"token expired"}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL, "perm:supersecret", TLS{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.SavedQueries(t.Context())
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.Contains(got, "token expired") || strings.Contains(got, "supersecret") {
		t.Errorf("error message leaks or lacks context: %q", got)
	}
}
