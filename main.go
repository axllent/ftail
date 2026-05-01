// Package main is the application
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	flag "github.com/spf13/pflag"

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
	tailers    []*tailer
	showNames  bool
	entries    []entry
	maxEntries int
	query      string
	cursor     int // rune index within query
	width      int
	height     int
	offset     int // rows scrolled up from the bottom; 0 = follow latest
	saving     bool
	savePath   string
	saveCursor int
	saveMsg    string // status shown after a save attempt
}

var (
	searchBarStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	matchStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	fileStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ruleFollowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // green — following
	ruleScrollStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange — scrolled
	saveStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	saveMsgOkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	saveMsgErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

type token struct {
	exclude bool
	term    string
}

// parseTokens splits a query into tokens. Whitespace separates tokens.
// A leading - or ! marks a token as an exclusion.
// Quoted strings (using ") are treated as a single term, preserving spaces.
// An unclosed quote is treated as if closed at end of input.
func parseTokens(pattern string) []token {
	var tokens []token
	runes := []rune(pattern)
	i := 0
	for i < len(runes) {
		for i < len(runes) && runes[i] == ' ' {
			i++
		}
		if i >= len(runes) {
			break
		}
		exclude := false
		if runes[i] == '-' || runes[i] == '!' {
			exclude = true
			i++
		}
		if i >= len(runes) {
			break
		}
		var term string
		if runes[i] == '"' {
			i++ // skip opening quote
			start := i
			for i < len(runes) && runes[i] != '"' {
				i++
			}
			term = string(runes[start:i])
			if i < len(runes) {
				i++ // skip closing quote
			}
		} else {
			start := i
			for i < len(runes) && runes[i] != ' ' {
				i++
			}
			term = string(runes[start:i])
		}
		if term != "" {
			tokens = append(tokens, token{exclude, term})
		}
	}
	return tokens
}

// match reports whether s satisfies the pattern:
// - inclusion terms must appear as substrings (case-insensitive)
// - exclusion terms (- or ! prefix) must NOT appear
func match(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	s = strings.ToLower(s)
	for _, t := range parseTokens(pattern) {
		contains := strings.Contains(s, strings.ToLower(t.term))
		if t.exclude && contains {
			return false
		}
		if !t.exclude && !contains {
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

	for _, tok := range parseTokens(pattern) {
		if tok.exclude {
			continue
		}
		w := []rune(strings.ToLower(tok.term))
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

func (m model) saveFiltered() error {
	f, err := os.Create(m.savePath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range m.entries {
		if !match(m.query, e.text) {
			continue
		}
		line := e.text
		if m.showNames {
			line = e.file + ": " + line
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return w.Flush()
}

func (m model) Init() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// insertRunes inserts r into s at rune position pos, returning the new string
// and updated cursor position.
func insertRunes(s string, pos int, r []rune) (string, int) {
	runes := []rune(s)
	runes = append(runes[:pos], append(r, runes[pos:]...)...)
	return string(runes), pos + len(r)
}

// deleteRune removes the rune at pos from s (backspace-style: pos-1).
func deleteRune(s string, pos int) (string, int) {
	if pos == 0 {
		return s, 0
	}
	runes := []rune(s)
	return string(append(runes[:pos-1], runes[pos:]...)), pos - 1
}

// deleteRuneForward removes the rune at pos (delete-key style).
func deleteRuneForward(s string, pos int) string {
	runes := []rune(s)
	if pos >= len(runes) {
		return s
	}
	return string(append(runes[:pos], runes[pos+1:]...))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// --- Save-prompt mode ---
		if m.saving {
			switch msg.Type {
			case tea.KeyCtrlC, tea.KeyEsc:
				m.saving = false
				m.savePath = ""
				m.saveCursor = 0
			case tea.KeyEnter:
				m.saving = false
				if err := m.saveFiltered(); err != nil {
					m.saveMsg = saveMsgErrStyle.Render("error: " + err.Error())
				} else {
					m.saveMsg = saveMsgOkStyle.Render("saved: " + m.savePath)
				}
				m.savePath = ""
				m.saveCursor = 0
			case tea.KeyLeft:
				m.saveCursor = max(m.saveCursor-1, 0)
			case tea.KeyRight:
				m.saveCursor = min(m.saveCursor+1, len([]rune(m.savePath)))
			case tea.KeyHome:
				m.saveCursor = 0
			case tea.KeyEnd:
				m.saveCursor = len([]rune(m.savePath))
			case tea.KeyBackspace:
				m.savePath, m.saveCursor = deleteRune(m.savePath, m.saveCursor)
			case tea.KeyDelete:
				m.savePath = deleteRuneForward(m.savePath, m.saveCursor)
			case tea.KeySpace:
				m.savePath, m.saveCursor = insertRunes(m.savePath, m.saveCursor, []rune{' '})
			case tea.KeyRunes:
				m.savePath, m.saveCursor = insertRunes(m.savePath, m.saveCursor, msg.Runes)
			}
			return m, nil
		}

		// --- Normal mode ---
		m.saveMsg = "" // clear any previous save status on next keypress
		avail := max(m.height-2, 0)
		filteredCount := 0
		for _, e := range m.entries {
			if match(m.query, e.text) {
				filteredCount++
			}
		}
		maxOffset := max(filteredCount-avail, 0)
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.query != "" {
				m.query = ""
				m.cursor = 0
				m.offset = 0
			} else {
				return m, tea.Quit
			}
		case tea.KeyCtrlS:
			m.saving = true
			m.savePath = ""
			m.saveCursor = 0
		case tea.KeyUp:
			m.offset = min(m.offset+1, maxOffset)
		case tea.KeyDown:
			m.offset = max(m.offset-1, 0)
		case tea.KeyPgUp:
			m.offset = min(m.offset+avail, maxOffset)
		case tea.KeyPgDown:
			m.offset = max(m.offset-avail, 0)
		case tea.KeyLeft:
			m.cursor = max(m.cursor-1, 0)
		case tea.KeyRight:
			m.cursor = min(m.cursor+1, len([]rune(m.query)))
		case tea.KeyHome:
			m.cursor = 0
		case tea.KeyEnd:
			m.cursor = len([]rune(m.query))
		case tea.KeyBackspace:
			m.query, m.cursor = deleteRune(m.query, m.cursor)
			m.offset = 0
		case tea.KeyDelete:
			m.query = deleteRuneForward(m.query, m.cursor)
			m.offset = 0
		case tea.KeySpace:
			m.query, m.cursor = insertRunes(m.query, m.cursor, []rune{' '})
			m.offset = 0
		case tea.KeyRunes:
			m.query, m.cursor = insertRunes(m.query, m.cursor, msg.Runes)
			m.offset = 0
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		var newMatches int
		for _, t := range m.tailers {
			lines, _ := t.readNew()
			for _, l := range lines {
				e := entry{file: t.path, text: l}
				m.entries = append(m.entries, e)
				if m.offset > 0 && match(m.query, e.text) {
					newMatches++
				}
			}
		}
		var trimMatches int
		if m.maxEntries > 0 && len(m.entries) > m.maxEntries {
			excess := len(m.entries) - m.maxEntries
			if m.offset > 0 {
				for _, e := range m.entries[:excess] {
					if match(m.query, e.text) {
						trimMatches++
					}
				}
			}
			m.entries = m.entries[excess:]
		}
		// When scrolled up, adjust offset so the visible window stays pinned
		// to the same content despite new lines arriving or old ones being trimmed.
		if m.offset > 0 {
			m.offset = max(m.offset+newMatches-trimMatches, 0)
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

	// Reserve 1 row for the separator and 1 for the search bar.
	avail := max(m.height-2, 0)

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

	// Separator rule — green when following, orange when scrolled.
	ruleStyle := ruleFollowStyle
	if m.offset > 0 {
		ruleStyle = ruleScrollStyle
	}
	sb.WriteString(ruleStyle.Render(strings.Repeat("─", m.width)))
	sb.WriteByte('\n')

	counterText := fmt.Sprintf("%d/%d", len(filtered), m.maxEntries)
	counter := fileStyle.Render(counterText)
	counterWidth := len([]rune(counterText))

	var prompt string
	var promptWidth int
	if m.saving {
		spRunes := []rune(m.savePath)
		spBefore := string(spRunes[:m.saveCursor])
		spAfter := string(spRunes[m.saveCursor:])
		prompt = saveStyle.Render("save: ") + spBefore + "█" + spAfter
		promptWidth = 6 + len(spRunes) + 1
	} else if m.saveMsg != "" {
		prompt = m.saveMsg
		promptWidth = len([]rune(m.saveMsg)) // approximate; ANSI codes ignored for padding
	} else {
		qRunes := []rune(m.query)
		before := string(qRunes[:m.cursor])
		after := string(qRunes[m.cursor:])
		prompt = searchBarStyle.Render("/ ") + before + "█" + after
		promptWidth = 2 + len(qRunes) + 1
	}
	pad := m.width - promptWidth - counterWidth
	if pad > 0 {
		prompt += strings.Repeat(" ", pad)
	}
	sb.WriteString(prompt + counter)

	return sb.String()
}

func main() {
	var showNames bool
	var nLines int
	var maxEntries int
	flag.BoolVarP(&showNames, "filename", "f", false, "prefix each line with the filename")
	flag.IntVarP(&nLines, "lines", "n", 0, "number of existing lines to show on start")
	flag.IntVarP(&maxEntries, "max", "m", 10000, "maximum number of lines to keep in the buffer")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: ftail [-f] [-n lines] [-m max] <file> [file ...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Follow one or more files, printing new lines as they are written.")
		fmt.Fprintln(os.Stderr, "Type to filter lines; press Ctrl+C to exit.")
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
		tailers:    tailers,
		showNames:  showNames,
		entries:    initialEntries,
		maxEntries: maxEntries,
	}, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
