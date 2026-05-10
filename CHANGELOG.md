# Changelog

All notable changes to this project will be documented in this file.

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
