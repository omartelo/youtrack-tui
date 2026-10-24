package ui

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// notifyLines is how many issues a notification names before it starts
// counting. A desktop popup is not a list view.
const notifyLines = 3

// watcher tracks what each watched filter has already reported. What is being
// watched is persisted; what has been seen is not, by design: a restart
// re-seeds and announces nothing, which beats being greeted by fifty
// notifications for issues opened while you were away.
//
// seen only ever grows within a session — an issue that leaves a filter is not
// forgotten, which is also why one that leaves and comes back is not announced
// twice. Both are bounded by how long the program runs.
type watcher struct {
	// watching is the set of saved-search IDs polled for the active provider,
	// seeded from the config and toggled with `w`. Provider.Watch is the
	// persisted copy of it.
	watching map[string]bool
	// seen[filterID] is the issue IDs already accounted for. A filter absent
	// from the map has not been polled yet, which is what makes the first poll
	// silent.
	seen map[string]map[string]bool
	// fresh maps a marked issue ID to the filter that reported it, so
	// unwatching that filter can take its markers down with it.
	fresh map[string]string
	// failed is true when the last poll errored, so the header can say so
	// without a modal interrupting whatever is on screen.
	failed bool
}

func newWatcher(seed []string) watcher {
	w := watcher{
		watching: make(map[string]bool, len(seed)),
		seen:     map[string]map[string]bool{},
		fresh:    map[string]string{},
	}
	for _, id := range seed {
		w.watching[id] = true
	}
	return w
}

// record folds a poll result in and returns the issues never seen before.
// The first poll for a filter only seeds, so nothing is announced.
func (w *watcher) record(filterID string, issues []youtrack.Issue) []youtrack.Issue {
	seen, known := w.seen[filterID]
	if !known {
		seen = make(map[string]bool, len(issues))
		w.seen[filterID] = seen
	}

	var out []youtrack.Issue
	for _, iss := range issues {
		if seen[iss.ID] {
			continue
		}
		seen[iss.ID] = true
		out = append(out, iss)
	}
	if !known {
		return nil
	}
	for _, iss := range out {
		w.fresh[iss.ID] = filterID
	}
	return out
}

// isFresh reports whether an issue is still marked as newly arrived.
func (w *watcher) isFresh(issueID string) bool {
	_, ok := w.fresh[issueID]
	return ok
}

// stop takes a filter off the watch list along with everything it contributed,
// so its markers do not outlive the watching.
func (w *watcher) stop(filterID string) {
	delete(w.watching, filterID)
	delete(w.seen, filterID)
	maps.DeleteFunc(w.fresh, func(_, from string) bool { return from == filterID })
}

// --- background watching ---------------------------------------------------

// startWatch retires any running tick chain and begins a new one. The first
// poll only seeds, so nothing is announced for issues that were already there.
func (m *Model) startWatch() tea.Cmd {
	m.watchGen++
	if len(m.watch.watching) == 0 {
		return nil
	}
	return tea.Batch(m.pollWatched(), m.tickWatch())
}

func (m *Model) tickWatch() tea.Cmd {
	gen := m.watchGen
	return tea.Tick(m.cfg.WatchEvery, func(time.Time) tea.Msg {
		return watchTickMsg{gen: gen}
	})
}

// pollWatched fetches every watched filter once. Watched IDs that no longer
// resolve to a saved search are skipped rather than reported.
func (m *Model) pollWatched() tea.Cmd {
	c, gen, top, scope := m.client, m.watchGen, m.cfg.PageSize, m.scope
	var cmds []tea.Cmd
	for _, q := range m.savedQueries {
		if !m.watch.watching[q.ID] {
			continue
		}
		id, query, label := q.ID, scoped(scope, q.Query), q.Name
		cmds = append(cmds, func() tea.Msg {
			issues, err := c.Issues(context.Background(), query, 0, top)
			return watchResultMsg{gen: gen, filterID: id, label: label, issues: issues, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// toggleWatch starts or stops monitoring the selected filter for this session.
// Nothing is written to the config: turning a poller on and off should not
// rewrite a file.
// toggleWatch starts or stops polling the selected filter and persists it.
// Matching is by ID, so renaming a saved search in YouTrack keeps it watched
// and two searches sharing a name stay independent — the same reason
// favourites record IDs.
func (m *Model) toggleWatch() tea.Cmd {
	it, ok := m.filters.SelectedItem().(filterItem)
	if !ok {
		return nil
	}
	p := &m.cfg.Providers[m.provider]
	if m.watch.watching[it.ID] {
		m.watch.stop(it.ID)
		if i := slices.Index(p.Watch, it.ID); i >= 0 {
			p.Watch = slices.Delete(p.Watch, i, i+1)
		}
	} else {
		m.watch.watching[it.ID] = true
		if !slices.Contains(p.Watch, it.ID) {
			p.Watch = append(p.Watch, it.ID)
		}
	}
	if err := m.saveConfig(); err != nil {
		// The poller still runs, it just will not be remembered.
		m.dlg = infoDialog("Config not saved", err.Error())
	}
	return tea.Batch(m.setFilterItems(), m.refreshIssueMarks(), m.startWatch())
}

// --- messages -------------------------------------------------------------

type watchTickMsg struct{ gen int }

type watchResultMsg struct {
	gen      int
	filterID string
	label    string
	issues   []youtrack.Issue
	err      error
}

type notifiedMsg struct{ err error }

// --- the notification itself ----------------------------------------------

// notifyNew puts one notification on screen per filter that gained issues.
func notifyNew(notifier, filter string, issues []youtrack.Issue) tea.Cmd {
	if len(issues) == 0 {
		return nil
	}
	title, body := fmt.Sprintf("youtrack-tui · %s", filter), notifyBody(issues)
	return func() tea.Msg {
		return notifiedMsg{err: notify(notifier, title, body)}
	}
}

// notifyBody names the first few issues and counts the rest.
func notifyBody(issues []youtrack.Issue) string {
	var b strings.Builder
	for i, iss := range issues {
		if i == notifyLines {
			fmt.Fprintf(&b, "\n…and %d more", len(issues)-notifyLines)
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s  %s", iss.ID, iss.Summary)
	}
	return b.String()
}

// notifyTimeout bounds the wait for the notification command. Unlike the
// browser launcher, this one waits for the exit status — a notifier that is
// missing or refuses the request is worth reporting — so it needs a ceiling:
// a stuck zenity would otherwise hold this goroutine for the whole session,
// one per poll.
const notifyTimeout = 10 * time.Second

// notify shells out to the desktop's notification command. The right one
// differs per machine, which is what earns it a config knob.
func notify(notifier, title, body string) error {
	name, args, err := notifyCommand(notifier, title, body)
	if err != nil || name == "" {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	// No shell, so nothing in an issue summary can be interpreted as a command.
	return exec.CommandContext(ctx, name, args...).Run()
}

func notifyCommand(notifier, title, body string) (string, []string, error) {
	switch notifier {
	case "", config.NotifierZenity:
		return "zenity", []string{"--notification", "--text=" + title + "\n" + body}, nil
	case config.NotifierNotifySend:
		return "notify-send", []string{"--app-name=youtrack-tui", title, body}, nil
	case config.NotifierNone:
		return "", nil, nil
	}
	return "", nil, fmt.Errorf("unknown notifier %q", notifier)
}
