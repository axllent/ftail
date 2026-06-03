# Changelog

All notable changes to this project will be documented in this file.

## [1.0.0]

### Changed

- Incremental filtering: typing more characters into a plain-text filter now narrows the previous result set instead of re-scanning every entry, keeping large buffers responsive while typing
- Filter tokens are lowercased once during parsing, eliminating repeated per-line `strings.ToLower` allocations in the match and highlight hot paths
- Trimming old entries when the buffer cap is reached now releases the dropped lines' bytes to the garbage collector immediately rather than retaining them until the backing array is reallocated
- Filter result slice is pre-sized for the common empty-query case, avoiding repeated regrowth on startup and when clearing the filter
- History modal no longer re-runs the filter on navigation, delete, or close — only when an entry is actually applied
- Per-entry footprint reduced by removing an unused `matched` flag

### Fixed

- A trailing line written without a newline is no longer emitted prematurely and then re-emitted as a separate line once the writer completes it
- Lines longer than 64 KB were silently truncated by the underlying scanner; the limit is now 1 MB for piped stdin and effectively unbounded for tailed files

## [0.0.8]

### Changed

- Filtering is now performed asynchronously in a goroutine, keeping the UI responsive while typing against large datasets; the search bar updates instantly on every keypress

### Fixed

- Initial line load with multiple files could exceed the `--limit` value; per-file quota is now distributed evenly across files and the combined result is trimmed to the limit before display
- Upgraded to Bubble Tea v2 and Lip Gloss v2

## [0.0.7]

### Added

- History picker (`Ctrl+R`) supports deleting individual entries with `d`; footer shows available actions

## [0.0.6]

### Added

- `Ctrl+R` opens a history picker modal (↑/↓ to select, Enter to apply, d to delete, Esc/q to cancel)
- `Ctrl+/` replaces `Ctrl+R` for toggling regex mode; prompt prefix changed from `r/` to `regex/`
- History entries now store regex state; picker displays `r/` indicator for regex entries and restores mode on selection
- History file format updated to prefix each line with `p ` (plain) or `r ` (regex); existing unprefixed files load as plain

### Changed

- Filter counter now shows `filtered/total` when a filter is active, and `total/limit` (or `total/∞`) when idle

### Fixed

- Duplicate `hasNewData` flag assignment removed

## [0.0.5]

### Added

- Persistent query history saved to `~/.ftailhst` between sessions
- `-l` / `--limit` flag replacing `-n` / `--lines` and `-m` / `--max`; sets the maximum number of lines to process (0 = unlimited); counter shows `∞` when unlimited
- `-n` / `--name` flag (renamed from `--filename`) to prefix each line with the source filename
- `Ctrl+N` shortcut to toggle filename prefix at runtime
- `Ctrl+T` shortcut to toggle timestamp prefix at runtime

## [0.0.4]

### Added

- `Ctrl+Q` shortcut for immediate quit
- Styled cursor display in filter and save prompts

## [0.0.3]

### Added

- Preset filter query via `-f` / `--filter` flag, applied on startup
- Updated README to reflect correct flags, defaults, and usage examples

## [0.0.2]

### Added

- Help modal (`Ctrl+H`) displaying all keyboard shortcuts with scrollable interface
- Horizontal scrolling support (`Shift+Left` / `Shift+Right`) for viewing long lines
- Word deletion with `Ctrl+W` (traditional Unix/Emacs word-delete binding)
- Word navigation with `Ctrl+Left` / `Ctrl+Right` for filter and save prompts
- Visual indicator (↓ New) in separator line when new data arrives while scrolled up
- Increased default buffer limits (10,000 entries)

### Fixed

- Scrolling offset now correctly maintained when buffer fills and old entries are trimmed
- View position stays pinned to same content when new entries arrive while scrolled up

## [0.0.1]

### Added

- Initial release
