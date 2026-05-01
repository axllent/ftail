# ftail

A terminal UI tool for tailing one or more files with live filtering, regex search, and scrollable history.

While many file tailing tools exist, this project was created to address a specific use case and as an AI experiment. Whilst I am perfectly capable of programming in Go, I wanted to explore how well I could design and implement a user-friendly utility using AI assistance in a relatively short amount of time. With the right prompts and guidance, it was able to generate what I was wanting to build.

## Features

- Tail one or more files simultaneously
- Live filtering as you type - plain word match or regex mode
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

| Flag | Long form     | Default | Description                                    |
| ---- | ------------- | ------- | ---------------------------------------------- |
| `-f` | `--filename`  | off     | Prefix each line with the source filename      |
| `-t` | `--timestamp` | off     | Prefix each line with the received timestamp   |
| `-n` | `--lines`     | 100000  | Number of existing lines to show on start      |
| `-m` | `--max`       | 100000  | Maximum number of lines to keep in the buffer  |
| `-u` | `--update`    | -       | Check for updates and self-update if available |
| `-v` | `--version`   | -       | Display version and exit                       |

```
ftail -n 100 /var/log/syslog
ftail /var/log/syslog /var/log/auth.log -f
ftail --lines 500 --max 50000 /var/log/nginx/access.log
```

## Interface

```
[log output]
──────────────────────────────────────────────
/ filter query█                      342/10000
```

- The **separator line** is green when the view is following new output, and orange when scrolled up.
- The **counter** (`342/10000`) shows matched lines / buffer limit.

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

| Key         | Action                         |
| ----------- | ------------------------------ |
| `←` `→`     | Move cursor                    |
| `Backspace` | Delete character to the left   |
| `Delete`    | Delete character under cursor  |
| `Enter`     | Save query to history          |
| `Esc`       | Clear filter (never exits)     |
| `Ctrl+C`    | Clear filter (if set), or exit |
| `Ctrl+R`    | Toggle regex mode              |

## Search history

| Key      | Action                                    |
| -------- | ----------------------------------------- |
| `Ctrl+↑` | Step back through previous queries        |
| `Ctrl+↓` | Step forward (back towards current input) |

History is saved when you press `Enter`, `Esc`, or `Ctrl+C`. Duplicates and empty queries are not saved. Up to 100 entries are kept.

## Scrolling

| Key                     | Action                                  |
| ----------------------- | --------------------------------------- |
| `↑` / `↓`               | Scroll one line                         |
| `Page Up` / `Page Down` | Scroll one page                         |
| `Home`                  | Jump to oldest entry (top of buffer)    |
| `End`                   | Jump to latest entry (resume following) |

When scrolled up, new lines continue to be tailed but the view stays static on the same content. Scrolling back to the bottom resumes following.

## Saving

Press `Ctrl+S` to open the save prompt. Type a filename and press `Enter` to write all currently filtered lines to that file. Press `Esc` or `Ctrl+C` to cancel.

The save prompt supports the same cursor movement and editing keys as the filter bar.
