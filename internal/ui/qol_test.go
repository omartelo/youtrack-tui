package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/youtrack"
)

// serverModel is a model pointed at a stub instance, plus the request counter
// of whatever that stub serves.
func serverModel(t *testing.T, body string) (*Model, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(body))
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
	return m, &hits
}

// Walking out of an issue and back into the list is the move this cache exists
// for: the same query inside the TTL must not go to the instance again, and
// `r` must go anyway.
func TestIssueListIsCachedUntilReload(t *testing.T) {
	m, hits := serverModel(t, `[{"idReadable":"PAY-1","summary":"x"}]`)
	m.screen = screenIssues

	drain(m, m.loadIssues("#Unresolved"), 0)
	if got := hits.Load(); got != 1 {
		t.Fatalf("first load made %d requests, want 1", got)
	}
	if got := len(m.allIssues); got != 1 {
		t.Fatalf("issues = %d, want 1", got)
	}

	drain(m, m.loadIssues("#Unresolved"), 0)
	if got := hits.Load(); got != 1 {
		t.Errorf("second load made %d requests, want the cached answer", got)
	}
	if got := len(m.allIssues); got != 1 {
		t.Errorf("the cached answer did not reach the list: %d issues", got)
	}

	// A different ordering is a different result set, cached separately.
	m.sortBy = 1
	drain(m, m.loadIssues("#Unresolved"), 0)
	if got := hits.Load(); got != 2 {
		t.Errorf("sorted load made %d requests total, want 2", got)
	}

	press(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if got := hits.Load(); got != 3 {
		t.Errorf("`r` made %d requests total, want it to bypass the cache", got)
	}
}

// The same query means different issues on another instance, so switching
// providers cannot leave the old answers behind.
func TestProviderSwitchDropsTheCache(t *testing.T) {
	m, _ := serverModel(t, `[]`)
	m.cache = map[string]cachedIssues{"#Unresolved": {issues: page(1, 1)}}
	m.cfg.Providers = append(m.cfg.Providers, m.cfg.Providers[0])
	m.cfg.Providers[1].Name = "internal"

	if err := m.setProvider(1); err != nil {
		t.Fatal(err)
	}
	if len(m.cache) != 0 {
		t.Errorf("cache survived a provider switch: %v", m.cache)
	}
}

// `y` copies through the terminal (OSC 52) and says so in the header — a
// clipboard write nobody sees is indistinguishable from a dead key.
func TestCopyIssueURL(t *testing.T) {
	m, _ := serverModel(t, `[]`)
	m.w, m.h = 120, 24
	m.screen = screenDetail
	m.current = &youtrack.Issue{ID: "PAY-1421"}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	want := m.client.IssueURL("PAY-1421")
	if !strings.Contains(fmt.Sprint(collect(cmd)), want) {
		t.Errorf("`y` did not put %q on the clipboard: %v", want, collect(cmd))
	}
	if got := plain(m.header()); !strings.Contains(got, "PAY-1421 URL copied") {
		t.Errorf("header does not confirm the copy: %q", got)
	}

	// The confirmation goes away on its own, and only the newest timer wins.
	m.Update(flashExpiredMsg{gen: m.flashGen - 1})
	if m.flash == "" {
		t.Error("a stale timer cleared the current flash")
	}
	m.Update(flashExpiredMsg{gen: m.flashGen})
	if m.flash != "" {
		t.Errorf("flash = %q, want it cleared", m.flash)
	}
}

// collect runs a command tree and returns every message it produced. Timer
// commands sleep inside the command — the flash expiry is one — so they are
// given up on rather than waited for, the same way drain does.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(20 * time.Millisecond):
		return nil
	}
	switch out := msg.(type) {
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, c := range out {
			msgs = append(msgs, collect(c)...)
		}
		return msgs
	case nil:
		return nil
	default:
		return []tea.Msg{out}
	}
}

// `c` jumps to the comments and `g`/`G` walk the ends. Long issues are the
// whole reason: reading the newest comment should not mean holding ↓.
func TestDetailJumps(t *testing.T) {
	m, _ := serverModel(t, `[]`)
	m.w, m.h = 100, 12
	m.layout()

	comments := make([]youtrack.Comment, 20)
	for i := range comments {
		comments[i] = youtrack.Comment{Text: fmt.Sprintf("comment %d", i)}
	}
	press(m, detailMsg{
		gen:      m.gen,
		issue:    &youtrack.Issue{ID: "PAY-1", Description: strings.Repeat("body\n\n", 40)},
		comments: comments,
	})
	if m.commentsLine == 0 {
		t.Fatal("the comments heading was not found in the rendered issue")
	}

	press(m, tea.KeyPressMsg{Code: 'c', Text: "c"})
	if got := m.detail.YOffset(); got != m.commentsLine {
		t.Errorf("`c` left the view at line %d, want %d", got, m.commentsLine)
	}

	press(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if got := m.detail.YOffset(); got != 0 {
		t.Errorf("`g` left the view at line %d, want the top", got)
	}

	press(m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	bottom := m.detail.YOffset()
	if bottom == 0 {
		t.Fatal("`G` did not move to the bottom")
	}

	// ctrl+u/ctrl+d are the viewport's own half-page bindings; they only work
	// because Update forwards what it does not handle.
	press(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	half := m.detail.YOffset()
	if half >= bottom {
		t.Errorf("ctrl+u left the view at %d, want above %d", half, bottom)
	}
	press(m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if got := m.detail.YOffset(); got <= half {
		t.Errorf("ctrl+d left the view at %d, want below %d", got, half)
	}
}

// The head is pinned above the viewport, not scrolled with the body: reading
// the last comment of a long issue should still say which issue it is.
func TestDetailHeadStaysPinned(t *testing.T) {
	m, _ := serverModel(t, `[]`)
	m.w, m.h = 100, 12
	m.layout()

	press(m, detailMsg{gen: m.gen, issue: &youtrack.Issue{ID: "PAY-1", Summary: "pinned"},
		comments: []youtrack.Comment{{Text: strings.Repeat("long\n\n", 40)}}})
	press(m, tea.KeyPressMsg{Code: 'G', Text: "G"})

	if !strings.Contains(plain(m.detailHead), "PAY-1") {
		t.Fatal("the pinned head does not carry the issue id")
	}
	if strings.Contains(plain(m.detail.View()), "PAY-1") {
		t.Error("the head is still inside the scrolling body")
	}
	// And it is paid for: the viewport gives up exactly the rows the head takes,
	// otherwise the footer scrolls off the bottom.
	if got := lipgloss.Height(m.detailHead) + m.detail.Height(); got != m.h-chromeLines {
		t.Errorf("head plus body is %d rows, the pane is %d", got, m.h-chromeLines)
	}
}
