package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// plain drops the styling so an assertion is about the layout, not the colours.
func plain(s string) string { return ansi.Strip(s) }

func testClient(t *testing.T) *youtrack.Client {
	t.Helper()
	c, err := youtrack.New("https://acme.youtrack.cloud", "perm:tok", youtrack.TLS{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func field(name, typ, value string) youtrack.CustomField {
	return youtrack.CustomField{Name: name, Type: typ, Value: []byte(value)}
}

// The field block is the UI half of the dynamic-fields invariant: whatever the
// API returned, in the order it returned it, and nothing else.
func TestRenderFields(t *testing.T) {
	resolved := int64(1700000000000)
	iss := youtrack.Issue{
		Fields: []youtrack.CustomField{
			field("State", "StateIssueCustomField", `{"name":"In Progress"}`),
			field("Empty one", "SingleEnumIssueCustomField", `null`),
			field("Assignee", "SingleUserIssueCustomField", `{"fullName":"Ana Souza"}`),
		},
		Resolved: &resolved,
	}

	out := plain(renderFields(iss))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (the empty field must be dropped):\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "State") || !strings.Contains(lines[0], "In Progress") {
		t.Errorf("line 0 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "Assignee") {
		t.Errorf("API order not preserved: line 1 = %q", lines[1])
	}
	if !strings.Contains(lines[2], "Resolved") {
		t.Errorf("resolved timestamp missing: %q", lines[2])
	}

	// Labels are padded to the widest name so the value column lines up.
	if a, b := strings.Index(lines[0], "In Progress"), strings.Index(lines[1], "Ana Souza"); a != b {
		t.Errorf("value column not aligned (%d vs %d):\n%q\n%q", a, b, lines[0], lines[1])
	}

	if got := renderFields(youtrack.Issue{}); got != "" {
		t.Errorf("an issue with no fields rendered %q", got)
	}
}

func TestRenderAttachmentsAndLinks(t *testing.T) {
	c := testClient(t)

	att := renderAttachments(c, []youtrack.Attachment{
		{Name: "trace.har", URL: "/api/files/0-1?sign=x", Size: 422604},
	})
	if got := plain(att); !strings.Contains(got, "trace.har") || !strings.Contains(got, "412.7 KB") {
		t.Errorf("attachment line = %q", got)
	}
	// The OSC 8 target keeps its escapes: the relative URL must come back
	// absolute or Ctrl+Click goes nowhere.
	if !strings.Contains(att, "https://acme.youtrack.cloud/api/files/0-1?sign=x") {
		t.Errorf("attachment is not an absolute hyperlink: %q", att)
	}
	if got := renderAttachments(c, nil); got != "" {
		t.Errorf("no attachments rendered %q", got)
	}

	links := plain(renderLinks(c, []youtrack.Link{
		{Direction: "OUTWARD", LinkType: youtrack.LinkType{SourceToTarget: "depends on"},
			Issues: []youtrack.Issue{{ID: "PAY-1", Summary: "one"}}},
		// YouTrack returns every link type, including the ones with no issues.
		{Direction: "INWARD", LinkType: youtrack.LinkType{TargetToSource: "is required for"}},
	}))
	if strings.Count(links, "\n") != 0 {
		t.Errorf("the empty link type produced a row:\n%s", links)
	}
	if !strings.Contains(links, "depends on") || !strings.Contains(links, "PAY-1") {
		t.Errorf("link line = %q", links)
	}
}

func TestRenderComments(t *testing.T) {
	c := testClient(t)
	md := func(s string) string { return s }

	if got := plain(renderComments(c, nil, md, 32)); !strings.Contains(got, "(none)") {
		t.Errorf("no comments rendered %q", got)
	}

	out := plain(renderComments(c, []youtrack.Comment{
		{Text: "first paragraph\n\nlast paragraph", Author: &youtrack.User{FullName: "Ana Souza"}, Created: time.Now().UnixMilli()},
		{Text: "second, no author"}, // author can be null on the API
	}, md, 32))
	if !strings.Contains(out, "Ana Souza") || !strings.Contains(out, "first") {
		t.Errorf("comment missing: %q", out)
	}
	if !strings.Contains(out, "second, no author") {
		t.Errorf("a comment with a nil author was dropped: %q", out)
	}
	divider := strings.Repeat("─", 32)
	if got := strings.Count(out, divider); got != 1 {
		t.Errorf("comment divider count = %d, want 1:\n%s", got, out)
	}
	if !strings.Contains(out, "last paragraph\n\n"+divider+"\n—") {
		t.Errorf("divider does not separate complete comments:\n%s", out)
	}
}

func TestRenderIssueHasEverySection(t *testing.T) {
	c := testClient(t)
	iss := &youtrack.Issue{
		ID: "PAY-1421", Summary: "Checkout retries duplicate the charge",
		Description: "the body",
		Fields:      []youtrack.CustomField{field("State", "StateIssueCustomField", `{"name":"Open"}`)},
		Attachments: []youtrack.Attachment{{Name: "trace.har", URL: "/f", Size: 10}},
		Links: []youtrack.Link{{Direction: "OUTWARD",
			LinkType: youtrack.LinkType{SourceToTarget: "relates to"},
			Issues:   []youtrack.Issue{{ID: "PAY-1", Summary: "one"}}}},
	}
	head, body := renderIssue(c, iss, []youtrack.Comment{{Text: "hi"}}, 90)
	out := plain(head + body)

	for _, want := range []string{"PAY-1421", "Fields", "Description", "Attachments", "Links", "Comments (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("section %q missing from the detail view", want)
		}
	}
	// Sections with nothing in them are dropped, not rendered empty.
	_, bareBody := renderIssue(c, &youtrack.Issue{ID: "PAY-2"}, nil, 90)
	bare := plain(bareBody)
	for _, gone := range []string{"Fields", "Attachments", "Links"} {
		if strings.Contains(bare, gone) {
			t.Errorf("empty section %q was rendered anyway", gone)
		}
	}
	if !strings.Contains(bare, "Comments (0)") || !strings.Contains(bare, "(empty)") {
		t.Errorf("a bare issue should still show its description and comment count:\n%s", bare)
	}
}

func TestHumanBytes(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0 B"}, {512, "512 B"}, {1023, "1023 B"},
		{1024, "1.0 KB"}, {422604, "412.7 KB"},
		{1024 * 1024, "1.0 MB"}, {3 * 1024 * 1024 * 1024, "3.0 GB"},
	} {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRelTime(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"never", time.UnixMilli(0), "—"},
		{"seconds", now.Add(-10 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m"},
		{"hours", now.Add(-3 * time.Hour), "3h"},
		{"days", now.Add(-50 * time.Hour), "2d"},
	} {
		ms := tc.at.UnixMilli()
		if tc.name == "never" {
			ms = 0
		}
		if got := relTime(ms); got != tc.want {
			t.Errorf("%s: relTime = %q, want %q", tc.name, got, tc.want)
		}
	}
	// Past a month it falls back to a date rather than counting days forever.
	old := now.AddDate(0, -6, 0)
	if got := relTime(old.UnixMilli()); got != old.Format("2006-01-02") {
		t.Errorf("old timestamp = %q, want a date", got)
	}
}
