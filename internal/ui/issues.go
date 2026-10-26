// The issue list: what is on it, what is cached, and the tick `x` leaves on a
// row. The list itself is a bubbles list.Model — this is only what fills it.
package ui

import (
	"slices"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// cachedIssues is one query's result set and when it was fetched. The page
// count is not kept: a short page is what says "that was the last one", and
// the handler works that out again from the length.
type cachedIssues struct {
	issues []youtrack.Issue
	at     time.Time
}

// issueCacheTTL is deliberately short. The point is the back-and-forth between
// a list and the issues on it, not saving a request an hour from now — the
// list on screen must not be yesterday's board.
const issueCacheTTL = 30 * time.Second

// cacheKey is the query as it goes over the wire, ordering clause included:
// the same filter sorted two ways is two result sets.
func (m *Model) cacheKey(query string) string {
	return scoped(m.scope, applySort(query, sortOrders[m.sortBy]))
}

// cacheIssues remembers everything currently on the issue list, pages and all,
// so that going back to it is free for issueCacheTTL.
func (m *Model) cacheIssues() {
	if m.query == "" || len(m.allIssues) == 0 {
		return
	}
	if m.cache == nil {
		m.cache = make(map[string]cachedIssues)
	}
	// Cloned: appendNewIssues grows allIssues in place, which would otherwise
	// reach into the entry that was just stored.
	m.cache[m.cacheKey(m.query)] = cachedIssues{issues: slices.Clone(m.allIssues), at: time.Now()}
}

// appendNewIssues adds a page, skipping anything already on the list. Paging is
// by offset, so an issue added to the filter between two requests shifts the
// window and makes the next page overlap the previous one.
func appendNewIssues(have, page []youtrack.Issue) []youtrack.Issue {
	seen := make(map[string]bool, len(have))
	for _, iss := range have {
		seen[iss.ID] = true
	}
	for _, iss := range page {
		if !seen[iss.ID] {
			have = append(have, iss)
		}
	}
	return have
}

// setIssueItems rebuilds the issue list from every page fetched so far.
func (m *Model) setIssueItems() tea.Cmd {
	p := m.cfg.Providers[m.provider]
	marked := make(map[string]bool, len(p.Marked))
	for _, id := range p.Marked {
		marked[id] = true
	}
	items := make([]list.Item, 0, len(m.allIssues))
	for _, iss := range m.allIssues {
		items = append(items, issueItem{
			issue:  iss,
			fields: p.ListFields,
			isNew:  m.watch.isFresh(iss.ID),
			marked: marked[iss.ID],
		})
	}
	return m.issues.SetItems(items)
}

// isMarked reports whether an issue carries the user's tick.
func (m *Model) isMarked(id string) bool {
	return m.cfg != nil && slices.Contains(m.cfg.Providers[m.provider].Marked, id)
}

// toggleMark ticks the selected issue off, or takes the tick back. It records
// IDs in the config next to `favorites` and `watch`, so a list worked through
// over a day survives closing the program — which is the only reason to tick
// anything off in the first place.
//
// The app assigns no meaning to it: reviewed, read, answered, deal with it
// tomorrow. Whoever presses `x` knows what they meant.
func (m *Model) toggleMark() tea.Cmd {
	id := m.selectedIssueID()
	if id == "" {
		return nil
	}
	p := &m.cfg.Providers[m.provider]
	if i := slices.Index(p.Marked, id); i >= 0 {
		p.Marked = slices.Delete(p.Marked, i, i+1)
	} else {
		p.Marked = append(p.Marked, id)
	}
	if err := m.saveConfig(); err != nil {
		m.dlg = infoDialog("Config not saved", err.Error())
	}
	// The list picks up the glyph; the Marked filter picks up the ID. On the
	// detail screen the header is what says it, since neither is on show.
	return tea.Batch(m.refreshIssueMarks(), m.refreshMarkedFilter())
}

// syncIssueInList replaces the list's copy of an issue with a freshly fetched
// one. Reading an issue is the only moment the program learns that a row it is
// already showing has gone out of date — which is what `e` makes possible.
func (m *Model) syncIssueInList(iss youtrack.Issue) {
	i := slices.IndexFunc(m.allIssues, func(o youtrack.Issue) bool { return o.ID == iss.ID })
	if i < 0 {
		return
	}
	m.allIssues[i] = iss
	m.cacheIssues()
}

// refreshIssueMarks redraws the list so a newly arrived issue picks up its
// marker, without disturbing where the user is.
func (m *Model) refreshIssueMarks() tea.Cmd {
	if len(m.allIssues) == 0 {
		return nil
	}
	at := m.issues.Index()
	cmd := m.setIssueItems()
	m.issues.Select(at)
	return cmd
}

// selectedIssueID is the issue the user is looking at, from either the list or
// the detail view. Empty on the screens that have none.
func (m *Model) selectedIssueID() string {
	switch m.screen {
	case screenIssues:
		if it, ok := m.issues.SelectedItem().(issueItem); ok {
			return it.issue.ID
		}
	case screenDetail:
		if m.current != nil {
			return m.current.ID
		}
	}
	return ""
}
