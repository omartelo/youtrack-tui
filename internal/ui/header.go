// The chrome around every screen: the one-line header that says which
// instance, which filter and what is switched on, and the footer that is the
// help bar. Both are pure — View calls them, nothing else does.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// flashFor is how long a header confirmation stays up.
const flashFor = 2 * time.Second

// flashExpiredMsg clears a header confirmation, unless a newer one replaced it.
type flashExpiredMsg struct{ gen int }

// flashCmd puts a confirmation in the header and takes it away again. Only
// for things that otherwise leave no trace: a clipboard write nobody sees is
// indistinguishable from a key that does nothing. Errors are still dialogs.
func (m *Model) flashCmd(text string) tea.Cmd {
	m.flashGen++
	m.flash = text
	gen := m.flashGen
	return tea.Tick(flashFor, func(time.Time) tea.Msg { return flashExpiredMsg{gen} })
}

// commentsNewestFirst is the reading order of an issue's comments. It lives
// in the config so that `S` on the detail screen is remembered, and is read
// through here because there is no config at all on the setup screen.
func (m *Model) commentsNewestFirst() bool {
	return m.cfg != nil && m.cfg.CommentsNewestFirst
}

func (m *Model) insecure() bool {
	return m.cfg != nil && m.cfg.Providers[m.provider].Insecure
}

func (m *Model) providerName() string {
	if m.cfg == nil {
		return "setup"
	}
	return m.cfg.Providers[m.provider].Name
}

func (m *Model) header() string {
	left := styTitle.Render(" youtrack-tui ") + styProvider.Render(" "+m.providerName()+" ")
	if m.insecure() {
		// A downgraded connection stays on screen for as long as it is on.
		left += styInsecure.Render(" !insecure ")
	}
	if n := len(m.watch.watching); n > 0 {
		// The glyph alone does not say what it counts, and this is the only
		// place the ◉ in the filters gutter gets explained.
		style, label := styWatch, fmt.Sprintf("◉ watching %d", n)
		if m.watch.failed {
			// A background poll cannot raise a modal, so it says so here.
			style, label = styWatchFail, fmt.Sprintf("◉ watching %d (failed)", n)
		}
		left += " " + style.Render(label)
	}

	if m.scope != "" {
		// Never in the `s` prompt, so this is the only place it shows.
		left += " " + styDim.Render("scope: "+m.scope)
	}

	if clause := sortOrders[m.sortBy]; clause != "" {
		// Exactly what was appended to the query, not a prettier name for it:
		// the same string works when typed into the `s` prompt.
		left += " " + styDim.Render("sort by: "+clause)
	}

	if m.screen == screenDetail && m.current != nil && m.isMarked(m.current.ID) {
		// The list glyph is two screens away; without this `x` on an open
		// issue looks like a key that does nothing.
		left += " " + styMark.Render("✓ marked")
	}

	if m.screen == screenDetail && m.commentsNewestFirst() {
		// Written to the config, so it stays on until it is turned off —
		// worth a word on screen, like the sort clause above.
		left += " " + styDim.Render("comments: newest first")
	}

	if m.flash != "" {
		left += " " + styWatch.Render(m.flash)
	}

	if m.newVersion != "" {
		// Ambient, like the watch counter: it says a newer release exists and
		// names the command that installs it, and nothing about it interrupts.
		left += " " + styUpdate.Render("↑ "+m.newVersion+" — run `youtrack-tui update`")
	}

	var right string
	switch {
	case m.loading:
		right = m.spin.View() + " loading…"
	case m.screen == screenSetup:
		right = styDim.Render("first run")
	case m.screen == screenDetail && m.current != nil:
		right = styDim.Render(m.current.ID)
	case m.query != "":
		right = styDim.Render(m.query)
		if m.queryName != "" {
			right = styValue.Render(m.queryName) + "  " + right
		}
	default:
		right = styDim.Render("pick a filter")
	}

	// The name pushed an already long clause further right; without this the
	// row overflows and lipgloss wraps it under the title.
	if avail := m.w - lipgloss.Width(left) - 1; avail > 0 && lipgloss.Width(right) > avail {
		right = ansi.Truncate(right, avail, "…")
	}

	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right + "\n" +
		styRule.Render(strings.Repeat("─", m.w))
}

func (m *Model) footer() string {
	if m.dlg != nil {
		// The dialog carries its own keys; repeating them down here just
		// leaves the hint peeking out beside the box.
		return ""
	}
	if m.prompt.active {
		return styDim.Render("enter  run the query  ·  esc  cancel")
	}
	return m.help.View(screenKeys{m.keys, m.screen})
}
