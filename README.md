# ftail

A terminal UI tool for tailing one or more files with live filtering and scrolling.

## Features

- Tail one or more files simultaneously
- Live word-based filtering as you type
- Scrollable history with keyboard navigation
- Export filtered output to a file
- Colour-coded separator indicating whether the view is following or scrolled

## Installation

```
go install
```

## Usage

```
ftail [flags] <file> [file ...]
```

### Flags

| Flag | Long form | Default | Description |
|------|-----------|---------|-------------|
| `-f` | `--filename` | off | Prefix each line with the source filename |
| `-n` | `--lines` | 0 | Number of existing lines to show on start |
| `-m` | `--max` | 10000 | Maximum number of lines to keep in the buffer |

Flags may appear anywhere in the argument list:

```
ftail -n 100 /var/log/syslog
ftail /var/log/syslog /var/log/auth.log -f
ftail --lines 500 --max 50000 /var/log/nginx/access.log
```

## Interface

```
[log lines]
──────────────────────────────────────────────
/ filter query█                      342/10000
```

- The **separator line** is green when the view is following new output, and orange when scrolled up.
- The **counter** on the right shows filtered lines / buffer limit.

## Filtering

Type at any time to filter the visible lines. All words must match (case-insensitive).

| Syntax | Meaning |
|--------|---------|
| `foo` | Lines containing `foo` |
| `foo bar` | Lines containing both `foo` and `bar` (any order) |
| `-foo` | Exclude lines containing `foo` |
| `!foo` | Same as `-foo` |
| `"foo bar"` | Lines containing the exact phrase `foo bar` (including the space) |
| `foo -bar` | Lines containing `foo` but not `bar` |

The filter cursor supports full editing:

| Key | Action |
|-----|--------|
| `←` `→` | Move cursor |
| `Home` / `End` | Jump to start / end |
| `Backspace` | Delete character to the left |
| `Delete` | Delete character under cursor |
| `Ctrl+C` / `Esc` | Clear filter (if set), or exit |

## Navigation

| Key | Action |
|-----|--------|
| `↑` / `↓` | Scroll one line |
| `Page Up` / `Page Down` | Scroll one page |

When scrolled up, new lines continue to be tailed but the view stays static. Scrolling back to the bottom resumes following.

## Saving

Press `Ctrl+S` to open the save prompt. Type a filename and press `Enter` to write all currently filtered lines to that file. Press `Esc` or `Ctrl+C` to cancel.
