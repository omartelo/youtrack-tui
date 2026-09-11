// Requests: every call that talks to a YouTrack instance, and the messages
// they answer with. Kept apart from the state machine so the invariant is easy
// to check — a command here never reads Model, it is handed what it needs.
package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

type errMsg struct {
	gen int
	err error
}

type filtersMsg struct {
	gen     int
	queries []youtrack.SavedQuery
}

type issuesMsg struct {
	gen      int
	issues   []youtrack.Issue
	appendTo bool // a further page, not a fresh query
}

type detailMsg struct {
	gen      int
	issue    *youtrack.Issue
	comments []youtrack.Comment
	history  []youtrack.Change
}

// begin marks a new request generation and returns the values a command needs
// to be self-contained (invariant: no I/O reads Model state).
func (m *Model) begin() (*youtrack.Client, int) {
	m.gen++
	m.loading, m.dlg = true, nil
	return m.client, m.gen
}

func (m *Model) loadFilters() tea.Cmd {
	c, gen := m.begin()
	return func() tea.Msg {
		saved, err := c.SavedQueries(context.Background())
		if err != nil {
			return errMsg{gen, err}
		}
		return filtersMsg{gen, append(append([]youtrack.SavedQuery(nil), builtinFilters...), saved...)}
	}
}

func (m *Model) loadIssues(query string) tea.Cmd {
	m.query = query
	// A hit still goes back through issuesMsg rather than assigning here: it
	// is the same answer, only faster, and the handler is what knows how to
	// take one. `r` empties the cache, so this never stands between the user
	// and a refresh they asked for.
	if hit, ok := m.cache[m.cacheKey(query)]; ok && time.Since(hit.at) < issueCacheTTL {
		_, gen := m.begin()
		issues := hit.issues
		return func() tea.Msg { return issuesMsg{gen, issues, false} }
	}
	return m.fetchIssues(query, 0, false)
}

// loadMoreIssues fetches the page after everything already on screen.
func (m *Model) loadMoreIssues() tea.Cmd {
	if !m.moreIssues || m.query == "" {
		return nil
	}
	return m.fetchIssues(m.query, len(m.allIssues), true)
}

func (m *Model) fetchIssues(query string, skip int, appendTo bool) tea.Cmd {
	c, gen := m.begin()
	top := m.cfg.PageSize
	// The ordering clause is injected here rather than stored in m.query, so
	// the header keeps showing what the user asked for and cycling `S` cannot
	// stack one `sort by:` onto the last.
	q := applySort(query, sortOrders[m.sortBy])
	return func() tea.Msg {
		issues, err := c.Issues(context.Background(), q, skip, top)
		if err != nil {
			return errMsg{gen, err}
		}
		return issuesMsg{gen, issues, appendTo}
	}
}

func (m *Model) loadDetail(id string) tea.Cmd {
	c, gen := m.begin()
	return func() tea.Msg {
		ctx := context.Background()
		issue, err := c.Issue(ctx, id)
		if err != nil {
			return errMsg{gen, err}
		}
		comments, err := c.Comments(ctx, id)
		if err != nil {
			return errMsg{gen, err}
		}
		history, err := c.History(ctx, id)
		if err != nil {
			return errMsg{gen, err}
		}
		return detailMsg{gen, issue, comments, history}
	}
}

// editableMsg carries the fields of an issue the instance says can be set from
// a list, and what the legal values are.
type editableMsg struct {
	gen    int
	fields []youtrack.Editable
}

// editedMsg is the one message in the program that means something changed on
// the other side.
type editedMsg struct {
	gen   int
	id    string
	field string
	value string
}

func (m *Model) loadEditable(id string) tea.Cmd {
	c, gen := m.begin()
	return func() tea.Msg {
		fields, err := c.EditableFields(context.Background(), id)
		if err != nil {
			return errMsg{gen, err}
		}
		return editableMsg{gen, fields}
	}
}

// applyEdit is the only request in the program that writes anything. It sends
// one field and reports what it sent, because the answer worth showing is the
// issue read back afterwards, not the response body.
func (m *Model) applyEdit(id string, f youtrack.Editable, value string) tea.Cmd {
	c, gen := m.begin()
	return func() tea.Msg {
		if err := c.SetField(context.Background(), id, f, value); err != nil {
			return errMsg{gen, err}
		}
		return editedMsg{gen, id, f.Name, value}
	}
}
