# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- The issue head — id, summary and the reported/created/updated line — is
  pinned above the detail viewport instead of scrolling away with the body.
  Reading the last comment of a long issue still says which issue it is. Both
  lines are truncated rather than wrapped: pinned, the head has to be exactly
  the height the layout subtracts for it.

## [0.7.0] - 2026-08-20

### Added

- `e` on an open issue sets one of its custom fields. It asks the instance
  which fields take a value from a closed list, offers exactly those values,
  and writes one — so finishing a review no longer means opening YouTrack to
  move the card. The issue is read back afterwards rather than patched in
  memory, because a workflow on the far side may have moved more than the
  field that was sent, and choosing the value already there sends nothing.
- The write surface is one field wide and stays that way.
  `internal/youtrack/write.go` holds the only non-GET request in the program;
  the rest of the client is still GET only. Bundle-backed fields are addressed
  by name and user fields by login, since two people can share a full name.
  Multi-value fields, periods, dates and free text have no closed set of
  answers and are left to YouTrack's own UI.
- `e` needs a token with write permission. The token that reads is the token
  that writes; there is no read-only mode short of removing the permission in
  YouTrack.

### Changed

- The footer names at most five keys per screen — the way in, the way out, the
  one thing the screen is for, and `?`. `?` now opens the full key list in a
  popup instead of growing the footer, and that list is per screen, so the
  filters list stops offering `o`, `y` and `x`, which need an issue, and the
  issue list stops offering `f` and `w`, which are filters-only.
- The modal box draws its title in the top border and paints no background
  behind its content, which is what let the help columns render inside it
  without tearing the box apart.
- An open issue has a blank line between its summary and the line that dates
  it, so a long summary stops running into the metadata.

### Removed

- `docs/tui-mockup.md`.

## [0.6.0] - 2026-08-19

### Added

- The header names the saved filter it is listing, beside the query that
  filter stands for. Two searches over the same project used to read
  identically; a raw query typed at the `s` prompt has no name and does not
  inherit the previous one. The right-hand side is truncated rather than
  allowed to wrap the header onto a second row.
- `x` marks the issue under the cursor, and again unmarks it. Marked issues
  carry a `✓` in the list gutter and the header says `✓ marked` on an open one.
  The mark is recorded in the config next to `favorites`, so it survives a
  restart, and it deliberately means nothing in particular: reviewed, read,
  answered, come back later — whoever pressed the key knows.
- The filters screen grows a `Marked` entry while anything is marked, running
  `issue id: …` over exactly those issues. It is how marks are found again
  across filters, and where they get cleared.

## [0.5.1] - 2026-08-19

### Fixed

- Releases are published with their notes again. The workflow always composed
  the CHANGELOG section and passed it as `--release-notes`, but GoReleaser
  reads that file inside the changelog pipe, and `changelog.disable: true`
  skipped the pipe — so v0.4.0 and v0.5.0 went out with an empty body. Both
  have been filled in by hand, and the release run now reads the published
  release back and fails if its notes are empty.

## [0.5.0] - 2026-08-19

### Added

- `S` on an open issue flips its comments between oldest-first — the order
  YouTrack returns them in — and newest-first, and writes
  `comments_newest_first` back to the config, so the choice survives a restart.
  The header says which order is on.
- `y` copies the issue URL to the clipboard through the terminal (OSC 52),
  which works over SSH where `o` has no browser to hand it to. A one-line
  header confirmation names what was copied.
- `c` jumps to the comments of an open issue, and `g`/`G` to its ends. The
  viewport's own `ctrl+u`/`ctrl+d` half-page bindings are now covered by a
  test.

### Changed

- An issue list is kept for 30 seconds, so stepping out of an issue and into
  the next one no longer fetches the list again. Two sort orders are cached
  apart; `r` and a provider switch empty it.

## [0.4.0] - 2026-08-19

### Added

- `S` cycles the order of the issue list — the filter's own order, then
  `updated` and `created` in both directions — by appending the `sort by:`
  clause YouTrack's own sort helper writes. Sorting happens on the instance, so
  the list is fetched again from the first page and the header carries the
  clause while it is on. A custom field is named differently on every instance,
  so `sort by: Priority desc` still goes through the `s` prompt.

## [0.3.0] - 2026-08-18

### Added

- `youtrack-tui update` installs the latest release. The download is verified
  against the release's `checksums.txt` before it replaces anything.
- A binary installed through the Homebrew tap or the AUR is upgraded by running
  that package manager instead of being overwritten, which would leave the
  manager holding a version that is no longer on disk. The AUR helper runs
  unprivileged and is pointed at `pkexec`, so the password is asked for in a
  polkit dialog naming what it authorises; the command is printed before it
  runs.
- The TUI checks GitHub for a newer release once at startup and reports it in
  the header. It installs nothing on its own, never retries, and stays silent
  on a failure or a rate limit. `check_updates: false` turns the check off.
- `youtrack-tui -version`, and a version stamped into release builds.

### Changed

- `w` now persists the watch list to the config, the way `f` does for
  favourites: what was being watched when you quit is what the next run polls.
  What has already been seen is still session state, so a restart stays quiet.

## [0.2.0] - 2026-08-18

### Changed

- Homebrew, AUR, and release publishing are now driven by GoReleaser's own
  `homebrew_casks` and `aurs` publishers instead of hand-rendered templates.
- Homebrew installs the macOS cask (`brew install --cask
  omartelo/tap/youtrack-tui`); the bare Linux formula is gone, so Linux is
  served by the AUR package and `install.sh`.
- CI runs the suite on Linux, macOS, and Windows, and golangci-lint now enforces
  gofmt plus `bodyclose`, `errorlint`, `nilerr`, and `revive`.

### Fixed

- `TestSaveRoundTrip` no longer asserts a 0600 config on Windows, where Go maps
  a file mode to nothing but the read-only bit.

## [0.1.0] - 2026-08-18

### Added

- Read-only TUI for saved filters, issue lists, descriptions, comments, dynamic
  custom fields, attachments, and OSC 8 links.
- Multi-provider configuration, verified first-run setup, environment-backed
  tokens, custom CA bundles, and explicit per-provider insecure TLS mode.
- Raw queries, favourite filters, pagination, provider switching, desktop
  notifications, and session-only filter watching.
- Release builds for Linux, macOS, and Windows on amd64 and arm64.
- Checksum-verified curl installer, Homebrew tap formula, and `youtrack-tui-bin`
  AUR package.
- GitHub CI with tests, race detection, golangci-lint, and release packaging.

[Unreleased]: https://github.com/omartelo/youtrack-tui/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/omartelo/youtrack-tui/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/omartelo/youtrack-tui/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/omartelo/youtrack-tui/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/omartelo/youtrack-tui/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/omartelo/youtrack-tui/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/omartelo/youtrack-tui/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/omartelo/youtrack-tui/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/omartelo/youtrack-tui/releases/tag/v0.1.0
