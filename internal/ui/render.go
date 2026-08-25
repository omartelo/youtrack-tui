package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

var (
	styTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62"))
	styProvider = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("208"))
	styDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styRule     = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	stySection  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	styLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styValue    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styLink     = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Underline(true)
	styHead     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))
	styAuthor   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
	styFav      = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	styNew      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("46"))

	// A ticked issue is done with, so its glyph recedes rather than competes
	// with the ● of one that just arrived.
	styMark = lipgloss.NewStyle().Foreground(lipgloss.Color("108"))

	// Watching is ambient state, not an alert: a glyph in the gutter, no filled
	// chip. The header already carries two of those for identity.
	styWatch     = lipgloss.NewStyle().Foreground(lipgloss.Color("44"))
	styWatchFail = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))

	// A new release is news, not a warning: dim enough to ignore while
	// reading, legible enough to notice on the way past.
	styUpdate = lipgloss.NewStyle().Foreground(lipgloss.Color("140"))

	styQueryLabel = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("232")).Background(lipgloss.Color("62"))

	// The dialog paints no background: every line is padded to the inner width
	// instead, which covers the layer below without a background style that
	// would need re-asserting on each row. Ported from omartelo/lazyovpn.
	styDialogBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styDialogTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	styDialogBody   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styDialogHint   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	styInsecure = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("203"))
)

// link wraps text in an OSC 8 hyperlink. Ctrl+Click is the terminal's job —
// which is exactly why this program never enables mouse tracking.
func link(text, url string) string {
	if url == "" {
		return text
	}
	return styLink.Hyperlink(url).Render(text)
}

// --- list items -----------------------------------------------------------

type filterItem struct {
	youtrack.SavedQuery
	fav     bool
	watched bool
}

// Title reserves one gutter column per marker, always, so pinning or watching
// something never shifts the list sideways. Repeating the word "watching" on
// every row was louder than the names it was decorating.
func (f filterItem) Title() string {
	fav, watch := " ", " "
	if f.fav {
		fav = styFav.Render("★")
	}
	if f.watched {
		watch = styWatch.Render("◉")
	}
	return fav + " " + watch + " " + f.Name
}

func (f filterItem) Description() string { return "    " + f.Query }
func (f filterItem) FilterValue() string { return f.Name + " " + f.Query }

type issueItem struct {
	issue youtrack.Issue
	// fields names the custom fields to show on the summary line. Empty means
	// "the first few that have a value" — field names differ per instance.
	fields []string
	// isNew marks an issue a watched filter picked up since this session
	// started, until it is opened.
	isNew bool
	// marked is the user's own tick, persisted in the config. It carries no
	// meaning of its own — see Provider.Marked.
	marked bool
}

// Title reserves one gutter column per marker, the way the filters list does,
// so ticking an issue or a new one arriving never shifts the rows sideways.
func (i issueItem) Title() string {
	tick, fresh := " ", " "
	if i.marked {
		tick = styMark.Render("✓")
	}
	if i.isNew {
		fresh = styNew.Render("●")
	}
	return tick + " " + fresh + " " + i.issue.ID + "  " + i.issue.Summary
}
func (i issueItem) FilterValue() string { return i.issue.ID + " " + i.issue.Summary }

func (i issueItem) Description() string {
	parts := make([]string, 0, 4)
	if len(i.fields) > 0 {
		for _, name := range i.fields {
			if v := i.issue.Field(name); v != "" {
				parts = append(parts, v)
			}
		}
	} else {
		for _, f := range i.issue.Fields {
			if len(parts) == 3 {
				break
			}
			if v := f.String(); v != "" {
				parts = append(parts, v)
			}
		}
	}
	parts = append(parts, "updated "+relTime(i.issue.Updated))
	return "    " + strings.Join(parts, " · ")
}

var _ list.DefaultItem = issueItem{}
var _ list.DefaultItem = filterItem{}

// --- issue detail ---------------------------------------------------------

// renderIssue composes the detail pane in two halves: head — which issue this
// is — and the body that scrolls under it. app.go pins the head above the
// viewport, so the title stays on screen however far down the comments go.
//
// Both head lines are truncated rather than wrapped: pinned, the head has to
// be exactly the height the layout subtracts for it, or the footer falls off
// the bottom of a narrow terminal.
func renderIssue(c *youtrack.Client, iss *youtrack.Issue, comments []youtrack.Comment, width int) (head, body string) {
	inner := max(20, width-2)
	md := newMarkdown(inner - 2)

	head = styHead.Render(link(ansi.Truncate(iss.ID+"  "+iss.Summary, inner, "…"), c.IssueURL(iss.ID))) +
		// A blank line under the summary: with the dates against it the two
		// read as one paragraph.
		"\n\n" +
		styDim.Render(ansi.Truncate(fmt.Sprintf("reported by %s · created %s · updated %s",
			fallback(iss.Reporter.String(), "—"), relTime(iss.Created), relTime(iss.Updated)), inner, "…")) +
		"\n" + styRule.Render(strings.Repeat("─", inner))

	var b strings.Builder

	if s := renderFields(*iss); s != "" {
		b.WriteString(section("Fields", s))
	}
	b.WriteString(section("Description", md(iss.Description)))
	if s := renderAttachments(c, iss.Attachments); s != "" {
		b.WriteString(section("Attachments", s))
	}
	if s := renderLinks(c, iss.Links); s != "" {
		b.WriteString(section("Links", s))
	}
	b.WriteString(section(fmt.Sprintf("Comments (%d)", len(comments)), renderComments(c, comments, md, inner-2)))
	return head, b.String()
}

func section(title, body string) string {
	return "\n" + stySection.Render("▌ "+title) + "\n" + indent(body, 2) + "\n"
}

// renderFields lays out every custom field the API returned, in API order.
// Nothing here knows the name of a single field: see the dynamic-fields
// invariant in CLAUDE.md.
func renderFields(iss youtrack.Issue) string {
	type row struct{ k, v string }
	rows := make([]row, 0, len(iss.Fields)+1)
	for _, f := range iss.Fields {
		if v := f.String(); v != "" {
			rows = append(rows, row{f.Name, v})
		}
	}
	if iss.Resolved != nil {
		rows = append(rows, row{"Resolved", time.UnixMilli(*iss.Resolved).Format("2006-01-02 15:04")})
	}
	if len(rows) == 0 {
		return ""
	}

	pad := 0
	for _, r := range rows {
		pad = max(pad, lipgloss.Width(r.k))
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(styLabel.Render(r.k + strings.Repeat(" ", pad-lipgloss.Width(r.k))))
		b.WriteString("  " + styValue.Render(r.v) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderAttachments(c *youtrack.Client, as []youtrack.Attachment) string {
	var b strings.Builder
	for _, a := range as {
		b.WriteString("◆ " + link(a.Name, c.AbsURL(a.URL)))
		b.WriteString(styDim.Render("  "+humanBytes(a.Size)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderLinks(c *youtrack.Client, ls []youtrack.Link) string {
	var b strings.Builder
	for _, l := range ls {
		for _, iss := range l.Issues {
			b.WriteString(styLabel.Render(l.Label()) + "  ")
			b.WriteString(link(iss.ID, c.IssueURL(iss.ID)))
			b.WriteString("  " + styValue.Render(iss.Summary) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderComments(c *youtrack.Client, cs []youtrack.Comment, md func(string) string, width int) string {
	if len(cs) == 0 {
		return styDim.Render("(none)")
	}
	var b strings.Builder
	for i, cm := range cs {
		if i > 0 {
			b.WriteString("\n" + styRule.Render(strings.Repeat("─", width)) + "\n")
		}
		b.WriteString(styAuthor.Render(fallback(cm.Author.String(), "—")))
		b.WriteString(styDim.Render("  "+relTime(cm.Created)) + "\n")
		b.WriteString(md(cm.Text) + "\n")
		if s := renderAttachments(c, cm.Attachments); s != "" {
			b.WriteString(s + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// newMarkdown returns a reusable renderer bound to a width. GLAMOUR_STYLE
// picks the theme; the default is dark.
func newMarkdown(width int) func(string) string {
	r, err := glamour.NewTermRenderer(glamour.WithEnvironmentConfig(), glamour.WithWordWrap(width))
	return func(src string) string {
		src = strings.TrimSpace(src)
		if src == "" {
			return styDim.Render("(empty)")
		}
		if err != nil {
			return src
		}
		out, rerr := r.Render(src)
		if rerr != nil {
			return src
		}
		return strings.Trim(out, "\n")
	}
}

// --- small helpers --------------------------------------------------------

func indent(s string, n int) string {
	p := strings.Repeat(" ", n)
	return p + strings.ReplaceAll(s, "\n", "\n"+p)
}

func fallback(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

func relTime(ms int64) string {
	if ms == 0 {
		return "—"
	}
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return time.UnixMilli(ms).Format("2006-01-02")
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
