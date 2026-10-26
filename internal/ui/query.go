package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// queryCharLimit caps the raw query. YouTrack queries with several field
// clauses run long, so this is looser than the setup fields; it exists to stop
// a runaway paste, not to constrain a real query.
const queryCharLimit = 500

// queryPrompt is the one-line input for typing a YouTrack query by hand rather
// than picking a saved search. It is a peer of the list's own `/` filter, which
// only narrows what is already on screen.
type queryPrompt struct {
	input  textinput.Model
	active bool
}

// scoped ANDs query under base, the `-q` flag. Each side is parenthesised
// because YouTrack binds `or` looser than juxtaposition — `#Logistica A or B`
// reads as `(#Logistica A) or B` — and the `and` is explicit because two
// groups side by side do not parse. Nor does a `sort by:` after a `)`, so
// every clause stays inside the query's group. Of two clauses YouTrack takes
// the first, so base's goes last: the session's default, which the filter's
// own order and `S` both beat without having to be recognised — `ordenar por:`
// included.
//
// shortcut: a localised order in base stays in base's group, first, and beats
// the filter's. Recognising it means a regexp per locale; nobody has asked.
func scoped(base, query string) string {
	if base == "" {
		return query
	}
	order := strings.TrimSpace(sortByRe.FindString(base))
	base = strings.TrimSpace(sortByRe.ReplaceAllString(base, ""))
	inner := strings.TrimSpace(query + " " + order)
	switch {
	case base == "":
		return inner
	case inner == "":
		return "(" + base + ")"
	}
	return "(" + base + ") and (" + inner + ")"
}

func newQueryPrompt() queryPrompt {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = queryCharLimit
	in.Placeholder = "project: PAY #Unresolved for: me"
	return queryPrompt{input: in}
}

// open focuses the prompt, seeded with the query currently on screen so an
// existing one can be tweaked instead of retyped.
func (q *queryPrompt) open(seed string) tea.Cmd {
	q.active = true
	q.input.SetValue(seed)
	cmd := q.input.Focus()
	q.input.CursorEnd()
	return cmd
}

func (q *queryPrompt) close() {
	q.active = false
	q.input.Blur()
}

func (q queryPrompt) value() string { return strings.TrimSpace(q.input.Value()) }

func (q *queryPrompt) setWidth(w int) { q.input.SetWidth(max(10, w-12)) }

func (q *queryPrompt) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	q.input, cmd = q.input.Update(msg)
	return cmd
}

// lines is what the layout has to subtract from the body height.
func (q queryPrompt) lines() int {
	if q.active {
		return 1
	}
	return 0
}

func (q queryPrompt) view() string {
	return styQueryLabel.Render(" query ") + " " + q.input.View()
}
