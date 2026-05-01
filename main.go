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
}

var (
	searchBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12"))
	matchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))
)

// fuzzyMatch reports whether all characters of pattern appear in s in order.
// fuzzyMatchWord reports whether all characters of word appear in s in order.
func fuzzyMatchWord(word, s string) bool {
	pi := 0
	for i := 0; i < len(s) && pi < len(word); i++ {
		if s[i] == word[pi] {
			pi++
		}
	}
	return pi == len(word)
}

// fuzzyMatch splits pattern on whitespace and requires every token to
// fuzzy-match s independently.
func fuzzyMatch(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	s = strings.ToLower(s)
	for word := range strings.FieldsSeq(pattern) {
		if !fuzzyMatchWord(strings.ToLower(word), s) {
			return false
		}
	}
	return true
}

func (m model) Init() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.query) > 0 {
				runes := []rune(m.query)
				m.query = string(runes[:len(runes)-1])
			}
		case tea.KeySpace:
			m.query += " "
		case tea.KeyRunes:
			m.query += string(msg.Runes)
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
		if fuzzyMatch(m.query, e.text) {
			filtered = append(filtered, e)
		}
	}

	// Reserve 1 line for the search bar.
	avail := max(m.height-1, 0)

	// Show only the most recent lines that fit.
	if len(filtered) > avail {
		filtered = filtered[len(filtered)-avail:]
	}

	var sb strings.Builder
	for i, e := range filtered {
		if i > 0 {
			sb.WriteByte('\n')
		}
		line := e.text
		if m.showNames {
			line = e.file + ": " + line
		}
		if m.query != "" {
			sb.WriteString(matchStyle.Render(line))
		} else {
			sb.WriteString(line)
		}
	}

	// Pad to push the search bar to the bottom.
	for i := len(filtered); i < avail; i++ {
		sb.WriteByte('\n')
	}

	// Search bar.
	prompt := searchBarStyle.Render("/ ") + m.query + "█"
	sb.WriteString(prompt)

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
