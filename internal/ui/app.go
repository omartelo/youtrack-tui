// Package ui holds the bubbletea program: three screens, one state machine.
package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

type screen int

const (
	screenSetup screen = iota
	screenFilters
	screenIssues
	screenDetail
	screenEditField
	screenEditValue
)

// Model is the root program state.
type Model struct {
	cfg      *config.Config
	path     string // config file location, for the first-run save
	provider int
	client   *youtrack.Client

	screen  screen
	setup   setupForm
	prompt  queryPrompt
	filters list.Model
	issues  list.Model
	detail  viewport.Model
	// detailHead is the issue title block, kept out of the viewport so it
	// stays on screen while the body scrolls under it.
	detailHead string
	// edit is the field-then-value picker `e` opens, one list.Model for both
	// steps: the delegate changes, and the screen says which step it is.
	edit       list.Model
	editFields []youtrack.Editable
	editField  int
	spin       spinner.Model
	help       help.Model
	keys       keyMap

	// dlg is the modal error popup. Non-nil means it owns the keyboard.
	dlg *dialog

	// newVersion is the tag of a release newer than this binary, empty until
	// the startup check says otherwise. It only ever gets shown in the header:
	// the TUI is read-only about itself too, and `youtrack-tui update` is what
	// installs anything.
	newVersion string

	// savePending means the config is written as soon as a request succeeds:
	// no point persisting a token that does not work. config.Save is what
	// keeps ${VAR} references intact.
	savePending bool

	// savedQueries is the filter list as YouTrack returned it, kept so
	// favouriting can re-sort without another round trip.
	savedQueries []youtrack.SavedQuery

	// allIssues accumulates across pages; it is also what the next $skip is
	// counted from. moreIssues is false once a short page comes back.
	allIssues  []youtrack.Issue
	moreIssues bool

	// cache holds each query's issues for issueCacheTTL, so that stepping out
	// of an issue and into the next one does not fetch the list again. Keyed
	// by the query as sent, ordering clause included; emptied by `r` and by a
	// provider switch, since the same query means something else elsewhere.
	cache map[string]cachedIssues

	// flash is a one-line confirmation in the header — what `y` copied, say.
	// flashGen retires the timer of a flash that was replaced by a newer one.
	flash    string
	flashGen int

	// commentsLine is where the comments start in the rendered issue, so `c`
	// can jump there. Recomputed by renderDetail, since the order flips.
	commentsLine int

	// sortBy indexes sortOrders: the `sort by:` clause pushed onto every issue
	// query. Session-only, like the query itself.
	sortBy int

	// query is what was sent; queryName is the saved search it came from, so
	// the header can say "TO DEPLOY" as well as the clause behind it. Empty
	// for a raw query typed at the `s` prompt — that one has no name.
	query     string
	queryName string

	current  *youtrack.Issue
	comments []youtrack.Comment

	// watch is the background poller for filters the user is monitoring, and
	// watchGen retires the tick chain when the provider changes or the watch
	// list is toggled — otherwise every toggle would leave another ticker
	// running alongside the first.
	watch    watcher
	watchGen int

	// gen invalidates in-flight responses after a provider switch or reload,
	// so a slow answer from the old instance never lands on the new one.
	gen     int
	loading bool
	w, h    int
}

// New builds the program. A nil cfg means there is no config file yet and the
// program opens on the setup screen instead of failing. provider selects a
// config entry by name; empty picks the first one.
func New(cfg *config.Config, provider, path string) (*Model, error) {
	m := &Model{
		path:    path,
		setup:   newSetupForm(path),
		prompt:  newQueryPrompt(),
		filters: newList("Filters"),
		issues:  newList("Issues"),
		edit:    newList("Edit field"),
		detail:  viewport.New(),
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:    help.New(),
		keys:    defaultKeys(),
	}
	m.issues.SetStatusBarItemName("issue", "issues")

	if cfg == nil {
		m.screen = screenSetup
		return m, nil
	}

	idx := 0
	if provider != "" {
		if idx = cfg.Find(provider); idx < 0 {
			return nil, fmt.Errorf("provider %q is not in the config", provider)
		}
	}
	m.cfg = cfg
	m.screen = screenFilters
	if err := m.setProvider(idx); err != nil {
		return nil, err
	}
	return m, nil
}

func newList(title string) list.Model {
	d := list.NewDefaultDelegate()
	l := list.New(nil, d, 0, 0)
	l.Title = title
	l.SetShowHelp(false)
	return l
}

// setProvider rebuilds the client for provider i. It can fail because the
// TLS settings are read here: a `ca_file` that is missing or not a PEM.
func (m *Model) setProvider(i int) error {
	p := m.cfg.Providers[i]
	c, err := youtrack.New(p.URL, p.Token, youtrack.TLS{CAFile: p.CAFile, Insecure: p.Insecure})
	if err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
	}
	m.provider, m.client = i, c
	// A cached query belongs to the instance it was asked of: the same text
	// means different issues on the next one.
	clear(m.cache)
	// Watched filters, what has been seen and what is still marked new are all
	// per-instance, so switching starts over rather than carrying state across.
	m.watch = newWatcher(p.Watch)
	return nil
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	if m.screen == screenSetup {
		return tea.Batch(m.spin.Tick, m.setup.focusOn(fieldName))
	}
	return tea.Batch(m.spin.Tick, m.loadFilters(), m.checkUpdateCmd())
}

// checkUpdateCmd is the startup update check, unless the config turned it off.
func (m *Model) checkUpdateCmd() tea.Cmd {
	if !m.cfg.ShouldCheckUpdates() {
		return nil
	}
	return checkUpdate()
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.renderDetail()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case errMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		// savePending survives on purpose: the retry offered by the dialog has
		// to still write the config once it works. Nothing is on disk yet.
		m.dlg = errorDialog(msg.err)
		return m, nil

	case filtersMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		if m.savePending {
			if err := m.saveConfig(); err != nil {
				// The session still works, it just will not be remembered.
				m.dlg = infoDialog("Config not saved", err.Error())
			}
			m.savePending = false
			m.screen = screenFilters
		}
		m.savedQueries = msg.queries
		m.migrateFavorites()
		// The watch list resolves IDs against these, so the poller can only
		// start once they have arrived.
		return m, tea.Batch(m.refreshMarkedFilter(), m.startWatch())

	case updateMsg:
		m.newVersion = msg.tag
		return m, nil

	case issuesMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		m.screen = screenIssues

		// A short page is how YouTrack says "that was the last one" — it has no
		// total count worth paying for.
		m.moreIssues = len(msg.issues) == m.cfg.PageSize
		m.keys.More.SetEnabled(m.moreIssues)

		at := m.issues.Index()
		if msg.appendTo {
			m.allIssues = appendNewIssues(m.allIssues, msg.issues)
		} else {
			m.allIssues, at = msg.issues, 0
		}
		m.cacheIssues()
		cmd := m.setIssueItems()
		m.issues.Select(at)
		return m, cmd

	case flashExpiredMsg:
		if msg.gen == m.flashGen {
			m.flash = ""
		}
		return m, nil

	case editableMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		if len(msg.fields) == 0 {
			// Text, date, period and multi-value fields are not offered, so
			// an issue built only from those has nothing to pick from.
			m.dlg = infoDialog("Nothing to edit here",
				"This issue has no single-value field backed by a list of allowed values.")
			return m, nil
		}
		m.editFields = msg.fields
		return m, m.showEditFields()

	case editedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		m.screen = screenDetail
		// A workflow runs on the far side and may have moved more than the one
		// field, so the issue is read back rather than patched here — and the
		// cached list goes with it.
		clear(m.cache)
		return m, tea.Batch(m.flashCmd(msg.field+" → "+msg.value), m.loadDetail(msg.id))

	case detailMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		m.screen = screenDetail
		if m.commentsNewestFirst() {
			slices.Reverse(msg.comments)
		}
		m.current, m.comments = msg.issue, msg.comments
		// The row behind the issue must not keep showing what a field said
		// before `e` changed it.
		m.syncIssueInList(*msg.issue)
		m.renderDetail()
		m.detail.GotoTop()
		// Reading it is what makes it no longer new.
		delete(m.watch.fresh, msg.issue.ID)
		return m, m.refreshIssueMarks()

	case watchTickMsg:
		if msg.gen != m.watchGen {
			return m, nil
		}
		return m, tea.Batch(m.pollWatched(), m.tickWatch())

	case watchResultMsg:
		if msg.gen != m.watchGen {
			return m, nil
		}
		if msg.err != nil {
			// A background poll must not throw a modal over what the user is
			// doing; the header carries a badge until the next poll works.
			m.watch.failed = true
			return m, nil
		}
		m.watch.failed = false
		fresh := m.watch.record(msg.filterID, msg.issues)
		if len(fresh) == 0 {
			return m, nil
		}
		return m, tea.Batch(m.refreshIssueMarks(),
			notifyNew(m.cfg.Notifier, msg.label, fresh))

	case notifiedMsg:
		if msg.err != nil {
			m.dlg = infoDialog("Notification failed", msg.err.Error()+
				"\n\nSet `notifier` in the config to notify-send, or to none to stop trying.")
		}
		return m, nil

	case openedMsg:
		if msg.err != nil {
			// Over SSH or on a headless box there is no handler. Show the URL
			// so it can at least be copied out.
			m.dlg = infoDialog("Could not open a browser",
				msg.err.Error()+"\n\n"+msg.url)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}

	// Everything else belongs to whichever sub-model is on screen. Routing only
	// a whitelist of message types silently breaks any bubble that answers its
	// own commands: bracketed paste arrives as tea.PasteMsg rather than key
	// presses, and the list runs its filter in a tea.Cmd and applies the result
	// when the FilterMatchesMsg comes back.
	return m, m.forward(msg)
}

func (m *Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.dlg != nil {
		return m.dialogKey(msg)
	}

	// The setup form owns every key while it is up — "q" has to type a q, not
	// quit the program.
	if m.screen == screenSetup {
		return m.setupKey(msg)
	}
	if m.prompt.active {
		return m.promptKey(msg)
	}

	// While the list's own filter input is open every key belongs to it.
	if (m.screen == screenFilters && m.filters.SettingFilter()) ||
		(m.screen == screenIssues && m.issues.SettingFilter()) ||
		(m.editing() && m.edit.SettingFilter()) {
		return m, m.forward(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		// A popup rather than a taller footer: the full list is two dozen
		// bindings, and growing the chrome pushes the thing being read off
		// the top of the screen.
		m.dlg = helpDialog(m.help, screenKeys{m.keys, m.screen})
		return m, nil

	case key.Matches(msg, m.keys.Provider):
		if len(m.cfg.Providers) < 2 {
			return m, nil
		}
		if err := m.setProvider((m.provider + 1) % len(m.cfg.Providers)); err != nil {
			m.dlg = errorDialog(err)
			return m, nil
		}
		m.screen = screenFilters
		m.filters.SetItems(nil)
		m.issues.SetItems(nil)
		m.current, m.comments = nil, nil
		return m, m.loadFilters()

	case key.Matches(msg, m.keys.Reload):
		// `r` means "ask again", so it has to outrank the cache.
		clear(m.cache)
		return m, m.reload()

	case key.Matches(msg, m.keys.More):
		if m.screen != screenIssues {
			return m, nil
		}
		return m, m.loadMoreIssues()

	case key.Matches(msg, m.keys.Search):
		if m.screen != screenFilters && m.screen != screenIssues {
			return m, nil
		}
		cmd := m.prompt.open(m.query)
		m.layout()
		return m, cmd

	case key.Matches(msg, m.keys.Sort):
		if m.screen == screenDetail {
			// Comments are ordered here, not by the instance: they all came
			// down with the issue, so reversing the slice is the whole job.
			// The config holds the choice, so the flip outlives the session
			// the same way `f` and `w` do.
			m.cfg.CommentsNewestFirst = !m.cfg.CommentsNewestFirst
			slices.Reverse(m.comments)
			if err := m.saveConfig(); err != nil {
				m.dlg = infoDialog("Config not saved", err.Error())
			}
			m.renderDetail()
			m.detail.GotoTop()
			return m, nil
		}
		if m.screen != screenFilters && m.screen != screenIssues {
			return m, nil
		}
		m.sortBy = (m.sortBy + 1) % len(sortOrders)
		if m.screen != screenIssues || m.query == "" {
			return m, nil
		}
		// Ordering is the instance's job: what is on screen is one window onto
		// the result set, so the query is run again from the first page.
		return m, m.loadIssues(m.query)

	case key.Matches(msg, m.keys.Favorite):
		if m.screen != screenFilters {
			return m, nil
		}
		return m, m.toggleFavorite()

	case key.Matches(msg, m.keys.Watch):
		if m.screen != screenFilters {
			return m, nil
		}
		return m, m.toggleWatch()

	case key.Matches(msg, m.keys.Mark):
		return m, m.toggleMark()

	case key.Matches(msg, m.keys.Browser):
		if id := m.selectedIssueID(); id != "" {
			return m, openInBrowser(m.client.IssueURL(id))
		}
		return m, nil

	case key.Matches(msg, m.keys.Copy):
		id := m.selectedIssueID()
		if id == "" {
			return m, nil
		}
		// OSC 52 hands the URL to the terminal rather than to a clipboard
		// tool, which is what makes it work over SSH — where `o` has no
		// browser to open and answers with a dialog holding the URL instead.
		return m, tea.Batch(tea.SetClipboard(m.client.IssueURL(id)),
			m.flashCmd(id+" URL copied"))

	case key.Matches(msg, m.keys.Edit):
		// The only key in the program that can change anything on the
		// instance, and it only offers what the instance says is writable.
		if m.screen != screenDetail || m.current == nil {
			return m, nil
		}
		return m, m.loadEditable(m.current.ID)

	case m.screen == screenDetail && key.Matches(msg, m.keys.Comments):
		m.detail.SetYOffset(m.commentsLine)
		return m, nil

	case m.screen == screenDetail && key.Matches(msg, m.keys.Top):
		m.detail.GotoTop()
		return m, nil

	case m.screen == screenDetail && key.Matches(msg, m.keys.Bottom):
		m.detail.GotoBottom()
		return m, nil

	case key.Matches(msg, m.keys.Back):
		// An applied filter owns esc: clearing it has to come before leaving
		// the screen, or there is no way back to the full list.
		if (m.screen == screenIssues && m.issues.IsFiltered()) ||
			(m.screen == screenFilters && m.filters.IsFiltered()) ||
			(m.editing() && m.edit.IsFiltered()) {
			return m, m.forward(msg)
		}
		switch m.screen {
		case screenDetail:
			m.screen = screenIssues
		case screenIssues:
			m.screen = screenFilters
		case screenEditField:
			m.screen = screenDetail
		case screenEditValue:
			// Back to the field list, not out of the editor: picking the
			// wrong field is the mistake this undoes.
			return m, m.showEditFields()
		}
		return m, nil

	case key.Matches(msg, m.keys.Open):
		switch m.screen {
		case screenFilters:
			if it, ok := m.filters.SelectedItem().(filterItem); ok {
				m.queryName = it.Name
				return m, m.loadIssues(it.Query)
			}
		case screenIssues:
			if it, ok := m.issues.SelectedItem().(issueItem); ok {
				return m, m.loadDetail(it.issue.ID)
			}
		case screenEditField:
			if it, ok := m.edit.SelectedItem().(editFieldItem); ok {
				return m, m.showEditValues(it.i)
			}
		case screenEditValue:
			return m, m.chooseEditValue()
		}
		return m, nil
	}

	return m, m.forward(msg)
}

// promptKey handles the raw-query input. Like the setup form it owns every key
// while it is open, otherwise typing a query containing "for" would fire the
// favourite, open-in-browser and reload commands.
func (m *Model) promptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.prompt.close()
		m.layout()
		return m, nil
	case "enter":
		q := m.prompt.value()
		m.prompt.close()
		m.layout()
		if q == "" {
			return m, nil
		}
		m.queryName = ""
		return m, m.loadIssues(q)
	}
	return m, m.prompt.update(msg)
}

// dialogKey handles the modal. It swallows everything else so a stray key
// never leaks into the screen behind it.
func (m *Model) dialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "i":
		if !m.dlg.offerTrust {
			return m, nil
		}
		m.dlg = nil
		return m, m.trustAnyway()
	case "esc", "enter", "q", "?":
		// `?` closes the help popup it opened, the way it used to untoggle
		// the full footer.
		m.dlg = nil
		return m, nil
	}
	return m, nil
}

// trustAnyway is the user accepting an untrusted certificate for this provider
// only. It is written to the config alongside everything else, so the choice
// stays visible instead of living in an environment variable.
func (m *Model) trustAnyway() tea.Cmd {
	p := &m.cfg.Providers[m.provider]
	p.CAFile, p.RawCAFile = "", ""
	p.Insecure = true
	if err := m.setProvider(m.provider); err != nil {
		m.dlg = errorDialog(err)
		return nil
	}
	// Persist the downgrade so the next run does not ask again.
	m.savePending = true
	return m.reload()
}

// reload re-runs whatever the current screen is showing.
func (m *Model) reload() tea.Cmd {
	switch m.screen {
	case screenSetup, screenFilters:
		return m.loadFilters()
	case screenIssues:
		return m.loadIssues(m.query)
	case screenDetail:
		if m.current != nil {
			return m.loadDetail(m.current.ID)
		}
	}
	return nil
}

func (m *Model) saveConfig() error {
	if err := config.Save(m.path, m.cfg); err != nil {
		return fmt.Errorf("could not write %s: %w", m.path, err)
	}
	return nil
}

// forward hands the message to whichever sub-model owns the current screen.
func (m *Model) forward(msg tea.Msg) tea.Cmd {
	// The prompt floats above the screens, so it takes precedence — this is
	// also what lets a query be pasted in.
	if m.prompt.active {
		return m.prompt.update(msg)
	}

	var cmd tea.Cmd
	switch m.screen {
	case screenSetup:
		cmd = m.setup.update(msg)
	case screenFilters:
		m.filters, cmd = m.filters.Update(msg)
	case screenIssues:
		m.issues, cmd = m.issues.Update(msg)
	case screenDetail:
		m.detail, cmd = m.detail.Update(msg)
	case screenEditField, screenEditValue:
		m.edit, cmd = m.edit.Update(msg)
	}
	return cmd
}

// chromeLines is what the layout reserves outside the body: two header lines
// (title row and rule) plus one footer line.
const chromeLines = 3

func (m *Model) layout() {
	body := max(1, m.h-chromeLines-m.prompt.lines())
	m.setup.setWidth(m.w)
	m.prompt.setWidth(m.w)
	m.filters.SetSize(m.w, body)
	m.issues.SetSize(m.w, body)
	m.edit.SetSize(m.w, body)
	m.detail.SetWidth(m.w)
	m.detail.SetHeight(max(1, body-lipgloss.Height(m.detailHead)))
}

func (m *Model) renderDetail() {
	if m.current == nil || m.w == 0 {
		return
	}
	head, body := renderIssue(m.client, m.current, m.comments, m.w)
	m.detailHead = head
	m.detail.SetContent(body)
	m.commentsLine = commentsLineOf(body)
	// The head eats rows the viewport was given: re-layout, or it scrolls past
	// the footer.
	m.layout()
}

// commentsLineOf finds the comments heading in a rendered issue, which is
// where `c` scrolls to. Zero — the top — when there is none to find.
//
// ponytail: counts lines of the rendered document, so it assumes the viewport
// shows them one for one. Everything here is already wrapped to the pane
// width, so it does; a soft-wrapping viewport would need its own line count.
func commentsLineOf(body string) int {
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Comments (") {
			return i
		}
	}
	return 0
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.w == 0 {
		return tea.NewView("")
	}

	var body string
	switch m.screen {
	case screenSetup:
		body = m.setup.view()
	case screenFilters:
		body = m.filters.View()
	case screenIssues:
		body = m.issues.View()
	case screenDetail:
		body = lipgloss.JoinVertical(lipgloss.Left, m.detailHead, m.detail.View())
	case screenEditField, screenEditValue:
		body = m.edit.View()
	}

	rows := []string{m.header()}
	if m.prompt.active {
		rows = append(rows, m.prompt.view())
	}
	rows = append(rows, body, m.footer())

	screen := lipgloss.JoinVertical(lipgloss.Left, rows...)
	if m.dlg != nil {
		screen = overlay(screen, m.dlg.view(m.w), m.w, m.h)
	}

	v := tea.NewView(screen)
	// Alt screen only. Mouse tracking stays off on purpose: it would steal
	// Ctrl+Click from the terminal and break every OSC 8 link we emit.
	v.AltScreen = true
	return v
}
