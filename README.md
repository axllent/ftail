# ftail

A terminal UI tool for tailing one or more files with live filtering, regex search, and scrollable history.

While many file tailing tools exist, this project was created to address a specific use case and as an AI experiment. Whilst I am perfectly capable of programming in Go, I wanted to explore how well I could design and implement a user-friendly utility using AI assistance in a relatively short amount of time. With the right prompts and guidance, it was able to generate what I was wanting to build.

## Features

- Tail one or more files simultaneously
- Live filtering as you type - plain word match or regex mode
- Preset filter query via `-f` flag
- Quoted phrases and exclusion terms in filter
- Scrollable history with keyboard navigation
- Search history with Ctrl+Up / Ctrl+Down
- Colour-coded source filenames when tailing multiple files
- Optional timestamp prefixes for each line
- Export filtered output to a file
- Colour-coded separator indicating whether the view is following or scrolled
- Self-update functionality via GitHub releases

## Installation

Download a pre-built binary from the [releases page](https://github.com/axllent/ftail/releases).

Install from source (required Go):

```
go install github.com/axllent/ftail@latest
```

## Usage

```
ftail [flags] <file> [file ...]
```

Flags may appear anywhere in the argument list.

### Flags

| Flag | Long form     | Default | Description                                        |
| ---- | ------------- | ------- | -------------------------------------------------- |
| `-f` | `--filter`    | -       | Preset filter query applied on startup             |
| `-n` | `--name`      | off     | Prefix each line with the source filename          |
| `-t` | `--timestamp` | off     | Prefix each line with the received timestamp       |
| `-l` | `--limit`     | 200000  | Maximum number of lines to process (0 = unlimited) |
| `-u` | `--update`    | -       | Check for updates and self-update if available     |
| `-v` | `--version`   | -       | Display version and exit                           |

```
ftail -l 100 /var/log/syslog
ftail /var/log/syslog /var/log/auth.log --name
ftail --limit 50000 /var/log/nginx/access.log
ftail -f "error" /var/log/app.log
ftail -f "warn|error" --name /var/log/app.log
ftail -l 0 /var/log/app.log
```

## Interface

```
[log output]
──────────────────────────────────────────────
/ filter query                       342/10000
```

- The **separator line** is green when the view is following new output, and orange when scrolled up.
- When scrolled up and new data arrives, the separator shows **"↓ New"** to indicate there's unread data below.
- The **counter** (`342/10000`) shows matched lines / line limit. Shows `∞` when the limit is disabled.

## Filtering

Type at any time to filter the visible lines. All terms are matched case-insensitively.

| Syntax           | Meaning                                                      |
| ---------------- | ------------------------------------------------------------ |
| `foo`            | Lines containing `foo`                                       |
| `foo bar`        | Lines containing both `foo` and `bar` (any order)            |
| `-foo` or `!foo` | Exclude lines containing `foo`                               |
| `"foo bar"`      | Lines containing the exact phrase `foo bar` (space included) |
| `foo -bar`       | Lines containing `foo` but not `bar`                         |

### Regex mode

Press `Ctrl+R` to toggle regex mode. The prompt changes from `/ ` to `r/ ` (magenta).

- The full query is treated as a single regular expression
- Matching is case-insensitive by default; use `(?-i)` to make it case-sensitive
- Invalid patterns turn the prompt red and show the compile error in the status bar

## Filter editing

| Key               | Action                         |
| ----------------- | ------------------------------ |
| `←` `→`           | Move cursor                    |
| `Ctrl+←` `Ctrl+→` | Jump to previous/next word     |
| `Backspace`       | Delete character to the left   |
| `Ctrl+W`          | Delete previous word           |
| `Delete`          | Delete character under cursor  |
| `Enter`           | Save query to history          |
| `Esc`             | Clear filter (never exits)     |
| `Ctrl+C`          | Clear filter (if set), or exit |
| `Ctrl+Q`          | Quit immediately               |
| `Ctrl+R`          | Toggle regex mode              |

## Search history

| Key      | Action                                    |
| -------- | ----------------------------------------- |
| `Ctrl+↑` | Step back through previous queries        |
| `Ctrl+↓` | Step forward (back towards current input) |

History is saved when you press `Enter`, `Esc`, or `Ctrl+C`. Duplicates and empty queries are not saved. Up to 100 entries are kept.

Query history is persisted to `~/.ftailhst` between sessions, so previous searches are available the next time you run ftail.

## Scrolling

| Key                     | Action                                  |
| ----------------------- | --------------------------------------- |
| `↑` / `↓`               | Scroll one line                         |
| `Page Up` / `Page Down` | Scroll one page                         |
| `Home`                  | Jump to oldest entry (top of buffer)    |
| `End`                   | Jump to latest entry (resume following) |
| `Shift+←` / `Shift+→`   | Scroll horizontally to view long lines  |

When scrolled up, new lines continue to be tailed but the view stays static on the same content. Scrolling back to the bottom resumes following.

Long lines are truncated to fit the terminal width. Use `Shift+←` and `Shift+→` to scroll horizontally and view content that extends beyond the screen. The horizontal scroll position is automatically reset when moving vertically or changing the filter.

## Line prefix

Press `Ctrl+N` to toggle the filename prefix on each line at any time, regardless of the `-n` flag used at startup.

Press `Ctrl+T` to toggle the timestamp prefix on each line at any time, regardless of the `-t` flag used at startup.

## Saving

Press `Ctrl+S` to open the save prompt. Type a filename and press `Enter` to write all currently filtered lines to that file. Press `Esc` or `Ctrl+C` to cancel.

The save prompt supports the same cursor movement and editing keys as the filter bar.

## Help

Press `Ctrl+H` at any time to display a help modal with all keyboard shortcuts. Use `↑`/`↓` or `Page Up`/`Page Down` to scroll the help if needed. Press `q`, `Esc` or `Ctrl+C` to close the help.
