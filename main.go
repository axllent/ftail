// Package main is the application
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const pollInterval = 200 * time.Millisecond

// tailer tracks the read position within a file.
type tailer struct {
	path   string
	file   *os.File
	offset int64
}

// lastNLines returns up to n lines before the end of f, and the file offset
// after the last returned line (i.e. where new content will start).
func lastNLines(f *os.File, n int) ([]string, int64, error) {
	const chunkSize = 4096
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil || size == 0 {
		return nil, size, err
	}

	buf := make([]byte, chunkSize)
	pos := size
	newlines := 0

	for pos > 0 {
		readSize := min(int64(chunkSize), pos)
		pos -= readSize

		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			return nil, 0, err
		}
		if _, err := io.ReadFull(f, buf[:readSize]); err != nil {
			return nil, 0, err
		}

		for i := int(readSize) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				newlines++
				if newlines == n {
					start := pos + int64(i) + 1
					return readLinesFrom(f, start)
				}
			}
		}
	}

	// Fewer lines in file than requested — return everything.
	return readLinesFrom(f, 0)
}

func readLinesFrom(f *os.File, offset int64) ([]string, int64, error) {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err
	}
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	end, _ := f.Seek(0, io.SeekCurrent)
	return lines, end, scanner.Err()
}

func newTailer(path string, n int) (*tailer, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	if n == 0 {
		offset, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		return &tailer{path: path, file: f, offset: offset}, nil, nil
	}

	initial, offset, err := lastNLines(f, n)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return &tailer{path: path, file: f, offset: offset}, initial, nil
}

// readNew returns any lines appended to the file since the last call.
func (t *tailer) readNew() ([]string, error) {
	info, err := os.Stat(t.path)
	if err != nil {
		return nil, err
	}

	// File was truncated or rotated.
	if info.Size() < t.offset {
		t.file.Close()
		f, err := os.Open(t.path)
		if err != nil {
			return nil, err
		}
		t.file = f
		t.offset = 0
	}

	if info.Size() == t.offset {
		return nil, nil
	}

	if _, err := t.file.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}

	var lines []string
	scanner := bufio.NewScanner(t.file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return lines, err
	}

	t.offset, err = t.file.Seek(0, io.SeekCurrent)
	return lines, err
}

func (t *tailer) close() {
	t.file.Close()
}

// entry holds a single tailed line and its source file.
type entry struct {
	file string
	text string
}

type tickMsg time.Time

type model struct {
	tailers   []*tailer
	showNames bool
	entries   []entry
	query     string
	width     int
	height    int
	offset    int // rows scrolled up from the bottom; 0 = follow latest
}

var (
	searchBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12"))
	matchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))
	fileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// match reports whether every whitespace-separated word in pattern appears
// as a substring of s (case-insensitive, any order).
func match(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	s = strings.ToLower(s)
	for word := range strings.FieldsSeq(pattern) {
		if !strings.Contains(s, strings.ToLower(word)) {
			return false
		}
	}
	return true
}

// highlight returns line with each occurrence of every word in pattern
// rendered in the match colour; unmatched text is left unstyled.
func highlight(pattern, line string) string {
	if pattern == "" {
		return line
	}
	lineRunes := []rune(line)
	lineLower := []rune(strings.ToLower(line))
	marked := make([]bool, len(lineRunes))

	for word := range strings.FieldsSeq(pattern) {
		w := []rune(strings.ToLower(word))
		for i := range len(lineLower) - len(w) + 1 {
			ok := true
			for j, r := range w {
				if lineLower[i+j] != r {
					ok = false
					break
				}
			}
			if ok {
				for j := range w {
					marked[i+j] = true
				}
			}
		}
	}

	var sb strings.Builder
	inMatch := false
	segStart := 0
	for i := range lineRunes {
		if marked[i] != inMatch {
			seg := string(lineRunes[segStart:i])
			if inMatch {
				sb.WriteString(matchStyle.Render(seg))
			} else {
				sb.WriteString(seg)
			}
			inMatch = marked[i]
			segStart = i
		}
	}
	seg := string(lineRunes[segStart:])
	if inMatch {
		sb.WriteString(matchStyle.Render(seg))
	} else {
		sb.WriteString(seg)
	}
	return sb.String()
}

func (m model) Init() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		avail := max(m.height-1, 0)
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyUp:
			m.offset = min(m.offset+1, max(len(m.entries)-avail, 0))
		case tea.KeyDown:
			m.offset = max(m.offset-1, 0)
		case tea.KeyPgUp:
			m.offset = min(m.offset+avail, max(len(m.entries)-avail, 0))
		case tea.KeyPgDown:
			m.offset = max(m.offset-avail, 0)
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.query) > 0 {
				runes := []rune(m.query)
				m.query = string(runes[:len(runes)-1])
				m.offset = 0
			}
		case tea.KeySpace:
			m.query += " "
			m.offset = 0
		case tea.KeyRunes:
			m.query += string(msg.Runes)
			m.offset = 0
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		for _, t := range m.tailers {
			lines, _ := t.readNew()
			for _, l := range lines {
				m.entries = append(m.entries, entry{file: t.path, text: l})
			}
		}
		return m, tea.Tick(pollInterval, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return m, nil
}

func (m model) View() string {
	// Filter entries against current query.
	var filtered []entry
	for _, e := range m.entries {
		if match(m.query, e.text) {
			filtered = append(filtered, e)
		}
	}

	// Reserve 1 row for the search bar; each content line is truncated to
	// m.width so it never wraps — 1 entry always equals 1 terminal row.
	avail := max(m.height-1, 0)

	// Select the visible window, honouring scroll offset.
	offset := min(m.offset, max(len(filtered)-avail, 0))
	end := len(filtered) - offset
	start := max(end-avail, 0)
	visible := filtered[start:end]

	var sb strings.Builder

	// Blank lines at the top fill space above the content so the search bar
	// is always anchored at the bottom of the screen.
	for i := len(visible); i < avail; i++ {
		sb.WriteByte('\n')
	}

	for _, e := range visible {
		prefix := ""
		prefixWidth := 0
		if m.showNames {
			prefix = e.file + ": "
			prefixWidth = len([]rune(prefix))
		}

		// Truncate the log text to the space remaining after the prefix.
		text := e.text
		if m.width > 0 {
			avail := m.width - prefixWidth
			runes := []rune(text)
			if len(runes) > avail {
				text = string(runes[:avail-1]) + "…"
			}
		}

		if prefix != "" {
			sb.WriteString(fileStyle.Render(prefix))
		}
		sb.WriteString(highlight(m.query, text))
		sb.WriteByte('\n')
	}

	// Search bar.
	sb.WriteString(searchBarStyle.Render("/ ") + m.query + "█")

	return sb.String()
}

func main() {
	var showNames bool
	var nLines int
	flag.BoolVar(&showNames, "f", false, "prefix each line with the filename")
	flag.IntVar(&nLines, "n", 0, "number of existing lines to show on start")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ftail [-f] [-n lines] <file> [file ...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Follow one or more files, printing new lines as they are written.")
		fmt.Fprintln(os.Stderr, "Type to fuzzy-filter lines; press Ctrl+C to exit.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if len(paths) > 1 {
		showNames = true
	}

	var initialEntries []entry
	tailers := make([]*tailer, 0, len(paths))
	for _, path := range paths {
		t, initial, err := newTailer(path, nLines)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ftail: %s: %v\n", path, err)
			os.Exit(1)
		}
		defer t.close()
		tailers = append(tailers, t)
		for _, l := range initial {
			initialEntries = append(initialEntries, entry{file: path, text: l})
		}
	}

	p := tea.NewProgram(model{
		tailers:   tailers,
		showNames: showNames,
		entries:   initialEntries,
	}, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
