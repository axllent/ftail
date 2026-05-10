# Changelog

All notable changes to this project will be documented in this file.

## [0.0.6]

### Added

- `Ctrl+R` opens a history picker modal (↑/↓ to select, Enter to apply and save to history, Esc/q to cancel)
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
