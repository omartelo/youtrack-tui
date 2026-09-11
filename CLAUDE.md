# youtrack-tui

## Objective

A **read-mostly** TUI for browsing YouTrack issues without leaving the
terminal.

Flow: pick a saved filter → list issues → open one → read its body, comments
and fields. Attachments and issue links are emitted as OSC 8 hyperlinks, so
Ctrl+Click hands them to the browser. The one thing written back is a single
custom field, so that finishing a review does not mean opening YouTrack to
move the card.

Multiple providers (YouTrack instances) are declared in a single `config.yml`.
With no config file the program opens on a setup screen and writes one — it
never exits on the user's first run.

## Layout

```
main.go                      flags, config load, program boot
internal/config/config.go    config.yml parsing, validation and saving
internal/youtrack/client.go  REST client, GET only
internal/youtrack/write.go   the one POST: setting a custom field
internal/youtrack/types.go   API types + dynamic custom-field rendering
internal/ui/app.go           root bubbletea model (6 screens, one state machine)
internal/ui/header.go        the header line, the help footer and the flash
internal/ui/filters.go       the filters list: built-ins, favourites, Marked
internal/ui/issues.go        the issue list, its cache and the `x` marks
internal/ui/setup.go         first-run configuration form
internal/ui/query.go         raw-query prompt (`s`)
internal/ui/sort.go          the `sort by:` clause pushed onto issue queries
internal/ui/edit.go          the `e` pickers: which field, which value
internal/ui/dialog.go        modal error popup and the layer overlay
internal/ui/browser.go       handing a URL to the desktop (`o`)
internal/ui/notify.go        background watcher and the notification command
internal/ui/update.go        the startup release check and its message
internal/update/update.go    GitHub release lookup and version comparison
internal/update/manager.go   which package manager owns this binary, if any
internal/update/apply.go     download, checksum, unpack, replace
internal/update/run.go       `youtrack-tui update` end to end
internal/ui/requests.go      every call to a YouTrack instance and its messages
internal/ui/render.go        styles, list items, issue detail composition
internal/ui/keys.go          key bindings and per-screen help
```

One package, files named after the thing they hold — the same shape
`charmbracelet/crush` uses for its own root model. `app.go` keeps the state
machine itself (`Model`, `Init`, `Update`, `onKey`, `View`, `layout`); a file
next to it owns one screen's data and the commands that shape it. Splitting
into sub-packages would buy nothing: there is one `Model` and every one of
these functions is a method on it.

One state machine, one screen at a time: `screenSetup` (first run only) →
`screenFilters` → `screenIssues` → `screenDetail`, plus `screenEditField` →
`screenEditValue` hanging off the detail screen. No side-by-side panes, no
focus management.

## Hard invariants

These are not style preferences. Breaking any one of them breaks the product.

1. **One field is the whole write surface.** `internal/youtrack/write.go`
   holds the only non-GET request in the program: `POST /api/issues/{id}`
   setting one single-value, bundle-backed custom field. Nothing else. No
   comment, no summary, no description, no `PUT`, `PATCH` or `DELETE`, and no
   second POST in `client.go` — that file stays GET only, which is what keeps
   the exception auditable.

   `editableTypes` is the fence: it maps a writable field type to the JSON key
   its values are addressed by, and a type absent from it is not offered and
   cannot be sent. Bundle-backed fields go by `name`; a user field goes by
   `login`, because two people can share a full name — which is also why
   `Option` carries a Label and a Value separately, and why a user bundle is
   read from `aggregatedUsers` rather than `values`. What is left out is left
   out on purpose: a multi-value field (Sprint, on most instances) takes an
   array rather than one object, and text, date and period have no list of
   legal answers at all. Widening the map is not a config change; it is a new
   value shape in `SetField` and a new way to be rejected.

   The reason the line sits here and not further along: a bundle value is a
   closed set the instance itself hands us, so the worst a mistyped write can
   do is move a card to a state that exists. Free text is a different risk
   and a different project.

2. **Mouse tracking stays off.** Never enable mouse reporting on the
   `tea.Program` or the `tea.View`. With mouse tracking on, the terminal stops
   handling Ctrl+Click on OSC 8 hyperlinks — the only way this app lets you
   download an attachment. Losing wheel scroll is the price; the keyboard
   covers everything.

3. **Tokens are never literals in the repo.** `token` in the YAML goes through
   `os.ExpandEnv`. No default, no built-in fallback, no logging of the value.
   Network and HTTP errors must never echo the `Authorization` header.

   `Config.Validate` expands `${VAR}` **in place** but stashes the original in
   `Provider.RawToken` / `RawCAFile`, and `config.Save` writes those back.
   Without that, any save — the first-run one, or the rewrite that accepting a
   certificate triggers — would replace `${YOUTRACK_TOKEN}` with the secret it
   resolved to and leave it on disk. Code that clears `CAFile` must clear
   `RawCAFile` too, or Save resurrects it. See
   `TestSaveAfterLoadKeepsReferences`. `Save` writes 0600 inside a 0700
   directory — on POSIX. Go maps a file mode to nothing but the read-only bit
   on Windows, so the same call lands as 0666 there and `TestSaveRoundTrip`
   skips that assertion; the reference-not-secret half of the invariant is
   checked on every OS.

4. **A missing config opens setup; a broken config still fails.** `main.run`
   only swallows `fs.ErrNotExist`. A file that exists but does not parse, or
   whose `${VAR}` is unset, exits with the reason — silently overwriting
   somebody's config is worse than an error message.

5. **The config file is written only after the credentials work.**
   `submitSetup` parks the config in `Model.pendingSave` and fires a real API
   call; the `filtersMsg` handler is what writes the file. A token that gets
   rejected leaves nothing on disk.

6. **Issue fields are dynamic.** The field list comes from whatever
   `customFields` the API returns, in the order it returns them. There is no
   struct with hardcoded `State`, `Priority` or `Assignee` — every YouTrack
   instance names its fields differently. Rendering is generic over `$type`
   plus the JSON shape.

7. **Every network call is a `tea.Cmd`.** No I/O inside `Update` or `View`,
   and commands never read `Model` state — `begin()` hands them everything
   they need. `View` is pure: same state, same output.

8. **Any open text input owns every key while it is up.** `onKey` routes to
   `setupKey` and `promptKey` before any global binding, otherwise `q`, `p`,
   `/`, `f`, `o` and `s` would trigger commands instead of typing characters —
   `for: me` alone hits four of them. `forward` gives the prompt the same
   precedence so a query can be pasted.

9. **`Update` forwards every message it does not handle to the active
   sub-model.** Never `return m, nil` as the fallthrough. The bubbles answer
   their own commands, and a whitelist of message types silently swallows
   those answers: bracketed paste arrives as `tea.PasteMsg` rather than key
   presses, and the list runs its filter in a `tea.Cmd` and only applies the
   result when the `FilterMatchesMsg` comes back. Both failures look like "the
   feature does nothing" with no error anywhere. See
   `TestPasteReachesTheSetupForm` and `TestIssueListFilterApplies`.

   Consequence for tests: calling `Update` alone proves nothing about anything
   command-driven. `press()` in `filter_test.go` runs the returned commands and
   feeds their messages back, which is the only way that class of bug shows up.

10. **TLS trust is per-provider, explicit and visible.** Never a global flag,
    never an environment variable read behind the user's back, never a silent
    fallback: trusting one internal host must not weaken the others. `ca_file`
    is the answer to reach for first, `insecure_skip_verify` is the last
    resort, and the two are mutually exclusive. Both live in the config next to
    the provider they affect; the dialog that offers the downgrade says what it
    costs, and the header carries `!insecure` for as long as it is on.

    The setup form prefills the CA field from `NODE_EXTRA_CA_CERTS` /
    `SSL_CERT_FILE` when set. That is a *suggestion on screen*, editable and
    persisted as the `${VAR}` reference — not the same thing as reading the
    variable at connect time. Do not make it the latter.

11. **What is watched is persisted; what has been seen is not.** `watch:`
    seeds `watcher.watching`, `w` toggles it and writes the config back, the
    same way `f` does for `favorites` — both record IDs, so a rename in
    YouTrack keeps the pin and the poller. `watcher.seen` and `watcher.fresh`
    stay in memory: a restart re-seeds silently rather than announcing every
    issue that arrived while the program was closed. See
    `TestWatchTogglePersists`.

12. **The first poll of a filter only seeds.** `watcher.record` returns nothing
    the first time it sees a filter ID. Without that, launching the program
    announces every issue that already matched, which is noise, not news. The
    same rule is why `w` on a filter drops its `seen` entry: re-watching starts
    from silence.

13. **A background poll never raises a modal.** It sets `watch.failed` and the
    header says so. An error from something the user did not ask for right now
    must not cover what they are reading.

14. **Every watch toggle bumps `watchGen`.** Ticks and results carry the
    generation they were scheduled under and are dropped if it has moved on.
    Without it each toggle leaves another `tea.Tick` chain polling the
    instance forever. See `TestWatchTogglesRetireTheOldTicker`.

15. **The footer is a glance and `?` is the manual.** `ShortHelp` names at
    most five bindings per screen — the way in, the way out, the one thing the
    screen is for, and `?`. Everything else lives in `FullHelp`, which `?`
    renders into the modal rather than into a taller footer: growing the chrome
    pushes the thing being read off the top. `FullHelp` is per screen for the
    same reason the footer is — a screen that advertises `f` when `f` does
    nothing there is worse than one that says nothing. See
    `TestHelpOnlyListsWhatTheScreenAnswers`.

16. **Errors go to the dialog, never to the header.** A wrapped TLS error is
    three lines long; the header is one. `errMsg` builds a `*dialog`, which
    owns the keyboard until dismissed. `Model.flash` is not a counterexample:
    it confirms something that succeeded, fits in a word, and clears itself —
    `flashGen` retires the timer of a flash a newer one replaced.

17. **`overlay` must go through `lipgloss.NewCompositor`.** `Canvas.Compose`
    on a parent layer calls `Layer.Draw`, which paints that layer's own content
    and ignores its children — you get the base alone or the box alone, never
    both. See `TestOverlayDrawsOverBase`.

18. **The dialog paints no background.** `titledBox` — ported from
    `omartelo/lazyovpn`, which is where this app's popup look comes from — puts
    the title in the top border and pads every line to the inner width, and
    that padding is what covers the screen underneath. Do not reach for a
    `Background()` on the box: a styled run nested inside a background style
    emits an SGR reset that clears the background for the rest of that row, so
    every line would have to re-assert it and the help columns, which carry
    their own styles, could not. See `TestDialogBoxIsRectangular`.

19. **`internal/ui` does not speak HTTP and `internal/youtrack` does not speak
    lipgloss.** The client returns data; the UI decides color and layout.

20. **A package-managed binary is never replaced in place.** `update.Run`
    calls `DetectManager` before `Apply`: a file under Homebrew's Cellar or
    Caskroom, or one pacman reports as owned, is upgraded by running that
    manager. Overwriting it would leave the manager holding a version that is
    not there and its next upgrade would put the old binary back. See
    `TestHomebrewInstallIsDelegated`.

21. **Elevation is asked for out loud, and never by us.** The AUR helper runs
    unprivileged — `paru` and `yay` refuse to run as root — and elevates for
    the install step alone. We point it at `pkexec` (`--sudo pkexec
    --nosudoloop`) so the password lands in a polkit dialog naming what it
    authorises, and `Run` prints what is about to happen first: a password
    prompt nobody announced is indistinguishable from a phishing attempt.
    Homebrew owns its prefix and is elevated for nothing.

22. **The release check runs once, at startup, and cannot raise anything.**
    `Init` fires it; there is no tick, no retry and no second request. A
    failure, a rate limit and an up-to-date binary all return a nil message —
    same rule as a background watch poll, for the same reason. A build that
    does not know its own version (`dev`) is never told it is behind. See
    `TestUpdateCheckIsOnlyScheduledAtStartup`.

23. **A write is proposed by the instance, never by us.** `EditableFields`
    asks YouTrack which fields are writable and what values they take, per
    issue — the bundle is per project, and two projects name the states of
    their workflow differently. The picker offers exactly that answer, minus
    archived values, which still render on issues that carry them but are
    refused as new ones. There is no hardcoded list of field names anywhere;
    invariant 6 applies to writing as much as to reading.

    After a successful `POST` the issue is read back rather than patched in
    memory: a workflow on the far side may have moved more than the field we
    sent. `editedMsg` clears `Model.cache` and refetches, and `detailMsg` is
    what syncs the fresh copy into `Model.allIssues` so the row behind the
    issue stops showing the old value. See `TestEditWritesTheChosenValue`.

    Choosing the value that is already there sends nothing — a POST that
    changes no field still fires the instance's workflow. See
    `TestEditSkipsAWriteThatChangesNothing`.

24. **A mark means nothing in particular.** `x` appends an issue ID to
    `Provider.Marked` and takes it away again; nothing else in the program
    reads it. No filtering by it, no sorting by it, no "reviewed" written
    anywhere on screen, and never a word sent to YouTrack — invariant 1 covers
    the last part, and the rest is what makes the feature usable by somebody
    whose workflow is not code review. The persisted-versus-volatile split of
    invariant 11 applies: what is marked is in the config, and the glyph on
    screen is derived from it, never the other way round.

    The `Marked` filter is the other half. Marks that cannot be listed cannot
    be cleared: the filter they were read under changes, and `PAY-1421` on its
    own says nothing. It is synthesised from `Provider.Marked` on every toggle
    — `refreshMarkedFilter` — so it never shows a query one tick out of date,
    and it is absent when nothing is marked rather than empty. See
    `TestMarkedFilterListsWhatIsTicked`.

## Known ceilings

Deliberate simplifications. Each one has its upgrade path written down. None
of them needs "fixing" before somebody actually complains.

- **Editing is one field at a time, from the detail screen only.** There is no
  `e` on the issue list, because the field list is fetched per issue and the
  list screen would need a request per row to offer anything. No multi-field
  form either: pick a field, pick a value, done. *Upgrade:* an `e` on the list
  that opens the issue first, if moving several cards in a row ever gets
  tiring.

- **A field with no closed set of answers is not offered.** Period fields
  (Estimativa, Duração), text, dates and anything multi-value are absent from
  the picker with no explanation on screen — the list is simply shorter than
  the card. *Upgrade:* multi-value is a multi-select over the same bundle and
  an array in `SetField`; period and text are a different risk and want the
  confirm dialog the ceiling below names.

- **A field cannot be cleared, only changed.** The picker lists what the
  bundle offers, never "no value", so `e` cannot unassign anybody or empty a
  state. *Upgrade:* read `canBeEmpty`/`emptyFieldText` off the project field
  and send `"value": null` for that row.

- **A write cannot be undone from here.** No confirmation before the POST and
  no way back except picking the old value again — which the picker starts on,
  so it is one keypress. *Upgrade:* a confirm/cancel dialog, which needs the
  `actions []dialogAction` slice the dialog ceiling below already names.

- **The token that reads is the token that writes.** One credential per
  provider, so turning on `e` means granting write scope to the same token the
  browsing uses. There is no read-only mode to fall back to short of removing
  the permission in YouTrack. *Upgrade:* a `read_only: true` per provider that
  hides the key, if somebody wants the guarantee back.

- **Setup only covers the first run.** There is no UI to add a second
  provider, edit an existing one or set `list_fields` — that is hand-editing
  the YAML. Submitting the form writes the whole file, so exposing it on an
  existing config would clobber the other providers. *Upgrade:* make
  `submitSetup` append to `m.cfg.Providers` instead of replacing them, then
  reach the screen from a key on the filters list.

- **Paging is forward-only and has no total.** `m` appends the next page;
  there is no way back and no "showing 50 of 812", because a count is a second
  request. A short page is the only end-of-list signal, which also means a
  filter whose size is an exact multiple of `page_size` offers one empty
  fetch. *Upgrade:* `/api/issuesGetter/count` for the total, if the extra round
  trip is worth it.

- **Provider switching cycles, it does not offer a picker.** `p` advances to
  the next provider. Better than a modal with 2–3 providers, annoying with 8.
  *Upgrade:* a fourth screen reusing the filters' `list.Model`.

- **Favourites are ours, not YouTrack's.** The REST API exposes no
  favourite/pinned flag on `SavedQuery` — `StarWatchFolder` stars *issues*, not
  searches — so `favorites` lives in our config. It records IDs, which buys
  surviving a rename at the cost of readability: `- 145-3` says nothing to
  someone editing the file by hand. *Upgrade:* an `{id, name}` mapping, which
  needs a custom `UnmarshalYAML` to keep reading the two older formats.

- **Sorting offers built-in fields only.** `S` cycles `updated` and `created`,
  both directions, because those two exist on every instance — a custom field
  does not, and the field list is dynamic by invariant 6. `sort by: Priority
  desc` is typed into the `s` prompt instead, and survives the cycle as long as
  it is left on the filter's own order. The choice is per session and not
  persisted, like the query it decorates. *Upgrade:* a `sort_options:` list on
  the provider, the same knob `list_fields` already is.

- **Comment order is global, not per-issue.** `S` on the detail screen writes
  `comments_newest_first` and every issue opened afterwards follows. The
  config is the state — there is no `Model` copy to keep in step — and the
  open issue is reordered by reversing `Model.comments` in place, because the
  comments all arrived with it and there is nothing to refetch. *Upgrade:* per
  provider, if two instances ever deserve different answers.

- **Marks never expire and never get cleared in bulk.** `Provider.Marked` only
  grows until somebody presses `x` again on each entry, from the `Marked`
  filter or from wherever the issue turns up. A year of review is a few hundred
  lines of YAML, and that whole list travels as one `issue id: A, B, C…` query,
  which a server will eventually refuse on length. *Upgrade:* age entries out
  by date, or a confirmed "clear these" key on the `Marked` list — both need a
  timestamp per mark, which the current `[]string` does not carry.

- **A raw query is not saved anywhere.** `s` runs it for the session; it is
  gone on restart and cannot be pinned, because `favorites` records IDs and an
  ad-hoc query has none. *Upgrade:* a `queries:` list on the provider, which
  would then need synthetic IDs like the built-ins have.

- **Watching only compares the first page.** A poll fetches `page_size`
  results and diffs those, so an issue that lands outside the first page never
  registers. Fine for a filter sorted newest-first, wrong for one sorted by
  priority. *Upgrade:* sort the watch query by `updated desc` explicitly rather
  than trusting the saved search's own order.

- **Only the active provider is polled.** Switching providers resets the
  watcher — what was seen, what was marked and what was being watched. *Upgrade:*
  a client and watcher per provider, which means keeping them all alive.

- **A watched issue is announced once, ever.** `watcher.seen` only grows within
  a session, so an issue that leaves a filter and comes back — resolved, then
  reopened — never notifies again. That is also what keeps a filter whose order
  churns from re-announcing the same issues. *Upgrade:* age entries out of
  `seen`, which trades the quiet for a second notification on every flap.

- **The notification is fire-and-forget.** No click-to-open, no grouping across
  filters, no rate limit beyond `watch_interval`. Two watched filters gaining
  issues in the same poll produce two popups. *Upgrade:* zenity cannot do
  actionable notifications; that needs `notify-send --action` and a listener.

- **The issue cache is 30 seconds, whole-list and in memory.** `Model.cache`
  keys on the query as sent — ordering clause included — and holds every page
  loaded so far. It is emptied by `r` and by a provider switch, never written
  to disk, and an issue's own detail is not cached at all. The window is short
  on purpose: the point is walking a list and back, not a board that is an
  hour old. *Upgrade:* an ETag or `updated` probe, if 30 seconds ever feels
  both too long and too short.

- **A date stored in `SimpleIssueCustomField` renders as a number.** The API
  only distinguishes date from integer via
  `projectCustomField.field.fieldType`, which we do not request.
  `DateIssueCustomField` and `PeriodIssueCustomField` are handled correctly.
  *Upgrade:* add `projectCustomField(field(fieldType(id)))` to the fields spec.

- **History lists custom field edits only.** `History` asks `/activities` for
  `CustomFieldCategory` alone, so a changed summary, description, link or
  attachment is not in it. Each edit is one line cut at the pane width — a text
  field's whole new text does not fit — and a period reads in hours, because
  YouTrack's `2d` depends on an admin's workday length. *Upgrade:* more
  `categories`, each with its own shape for `added` and `removed`.

- **The app does not download attachments itself.** It builds the signed URL
  and emits OSC 8; the terminal and browser do the rest. A terminal without
  OSC 8 support (or with mouse tracking left on by another app) cannot click.
  *Upgrade:* a `d` key calling `openInBrowser` on the selected attachment —
  the launcher already exists, what is missing is a cursor inside the detail
  viewport to select one.

- **`o` needs a desktop.** Over SSH or on a headless box `xdg-open` fails; the
  dialog then shows the URL to copy out. `y` is the way around it: OSC 52 goes
  through the terminal rather than the machine the program runs on. It cannot
  be confirmed — nothing answers an OSC 52 write — so the header flash says
  what was sent, not that it arrived.

- **Descriptions and comments render through glamour at a fixed width.** Wide
  markdown tables overflow. *Upgrade:* none that is cheap; live with it.

- **Permissions of a config we did not write are not checked.** `Save` creates
  0600, but a hand-written 0644 file with a plaintext token draws no warning.
  *Upgrade:* `os.Stat` in `Load` plus a warning when `mode&0o077 != 0`.

- **The setup screen verifies with `savedQueries`, not a dedicated endpoint.**
  A token valid but lacking that permission reads as a bad token. *Upgrade:*
  probe `/api/users/me` first and report the two failures differently.

- **The dialog is the only modal and has one action.** No confirm/cancel pair,
  no stacking, no queue — a second error replaces the first, and opening an
  error over the `?` popup replaces that too. Enough for the errors this
  program can produce. *Upgrade:* give `dialog` an `actions []dialogAction`
  slice when a second one shows up.

- **Accepting an untrusted certificate does not pin it.** `insecure_skip_verify`
  keeps skipping verification forever, for any certificate that host presents.
  *Upgrade:* store the certificate fingerprint and verify against it, which is
  what accepting once should really mean.

- **Update detection is one GitHub call per launch, unauthenticated.** A
  machine behind a shared IP can hit the 60/hour limit, and the check then
  quietly reports nothing. `check_updates: false` turns it off. *Upgrade:*
  cache the answer with a timestamp so a relaunch inside the hour skips the
  request.

- **The updater only knows Homebrew and pacman.** Scoop, winget, nix and a
  distro package someone else built all read as unmanaged and get the
  self-replacing path — which for a root-owned file fails on the write rather
  than doing damage. *Upgrade:* ask each manager whether it owns the path, at
  the cost of a process spawn per manager.

- **`ca_file` is not watched.** A rotated CA bundle is picked up on the next
  launch, not live. Fine; restarting a TUI is cheap.

- **Stale responses are dropped by a generation counter, not cancelled.** A
  provider switch bumps `Model.gen`; the in-flight request still completes and
  its result is discarded. *Upgrade:* a `context.CancelFunc` on `Model`, if
  request volume ever justifies it.

## Commands

Everything goes through [Task](https://taskfile.dev) — `task` on its own lists
the targets.

```sh
task check                    # lint + test, what CI runs
task test                     # go test -race ./...
task cover                    # coverage summary
task build                    # bin/youtrack-tui, stamped with `git describe`
task run -- -provider acme    # build then run; `interactive: true` keeps the TUI usable
task fmt tidy clean install
```

`task run` must stay `interactive: true`. Without it Task buffers stdio and the
TUI never gets a raw terminal.
