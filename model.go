package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

// historyEntry records a filter query and whether it was in regex mode.
type historyEntry struct {
	query string
	regex bool
}

type stdinLineMsg entry

func waitForStdin(ch <-chan entry) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return stdinLineMsg(e)
	}
}

type model struct {
	tailers           []*tailer
	stdinCh           <-chan entry
	showNames         bool
	showTimestamp     bool
	entries           []entry
	filtered          []int // indices into entries for rows matching the current query
	maxEntries        int
	fileColours       map[string]lipgloss.Style
	query             string
	queryRunes        []rune  // []rune(query), kept in sync with query
	tokens            []token // parsed tokens for plain-text mode
	cursor            int     // rune index within query
	width             int
	height            int
	offset            int  // rows scrolled up from the bottom; 0 = follow latest
	horizontalOffset  int  // columns scrolled to the right; 0 = leftmost
	hasNewData        bool // true when new data arrived while scrolled up
	showingHelp       bool
	helpOffset        int // scroll position in help screen; 0 = top
	showingHistory    bool
	historyModalIdx   int // display index; 0 = most recent entry
	saving            bool
	savePath          string
	saveCursor        int
	saveMsg           string // status shown after a save attempt
	saveMsgWidth      int    // visible (unstyled) rune width of saveMsg
	regexMode         bool
	compiledRe        *regexp.Regexp
	lastCompiledQuery string // query string used to produce compiledRe
	reErr             error
	history           []historyEntry
	historyIdx        int    // -1 = not browsing; >= 0 = index into history
	tempQuery         string // query saved before history browsing began
	tempCursor        int
	tempRegexMode     bool
	historyFile       string // path to persistent history file; empty = disabled
}

const maxHistory = 100

// addHistory appends query to history, deduplicating and capping the size.
// It also persists the entry to the history file if one is configured.
func (m *model) addHistory() {
	if m.query == "" {
		return
	}
	e := historyEntry{query: m.query, regex: m.regexMode}
	if len(m.history) > 0 && m.history[len(m.history)-1] == e {
		return
	}
	m.history = append(m.history, e)
	if len(m.history) > maxHistory {
		m.history = m.history[len(m.history)-maxHistory:]
	}
	appendHistoryFile(m.historyFile, e)
}

// loadHistoryFile reads the history file and returns its entries, deduplicating
// consecutive identical entries and capping at maxHistory. Errors are silently ignored.
// Each line is prefixed with "p " (plain) or "r " (regex); unprefixed lines are treated as plain.
func loadHistoryFile(path string) []historyEntry {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var entries []historyEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var e historyEntry
		switch {
		case strings.HasPrefix(line, "r "):
			e = historyEntry{query: line[2:], regex: true}
		case strings.HasPrefix(line, "p "):
			e = historyEntry{query: line[2:], regex: false}
		default:
			e = historyEntry{query: line, regex: false}
		}
		if e.query == "" {
			continue
		}
		if len(entries) > 0 && entries[len(entries)-1] == e {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) > maxHistory {
		entries = entries[len(entries)-maxHistory:]
	}
	return entries
}

// saveHistoryFile overwrites the history file with all current entries.
// Errors are silently ignored.
func saveHistoryFile(path string, entries []historyEntry) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	for _, e := range entries {
		prefix := "p"
		if e.regex {
			prefix = "r"
		}
		_, _ = fmt.Fprintf(f, "%s %s\n", prefix, e.query)
	}
}

// appendHistoryFile appends a single entry to the history file.
// Errors are silently ignored.
func appendHistoryFile(path string, e historyEntry) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	prefix := "p"
	if e.regex {
		prefix = "r"
	}
	_, _ = fmt.Fprintf(f, "%s %s\n", prefix, e.query)
}

// recompile updates queryRunes, tokens/compiledRe, and rebuilds filtered.
// Call whenever query or regexMode changes. Pass narrow=true when adding
// characters (KeyRunes/KeySpace) so that filtered is narrowed from its current
// state instead of rebuilt from all entries — safe only when the new filter is
// guaranteed to be more restrictive than the previous one.
func (m *model) recompile(narrow bool) {
	m.queryRunes = []rune(m.query)
	oldTokens := m.tokens
	if m.regexMode && m.query != "" {
		if m.query != m.lastCompiledQuery || m.compiledRe == nil {
			m.compiledRe, m.reErr = regexp.Compile("(?i)" + m.query)
			m.lastCompiledQuery = m.query
		}
		m.tokens = nil
		m.rebuildFiltered()
	} else {
		m.compiledRe = nil
		m.lastCompiledQuery = ""
		m.reErr = nil
		m.tokens = parseTokens(m.query)
		if narrow && tokensNarrow(oldTokens, m.tokens) {
			m.narrowFiltered()
		} else {
			m.rebuildFiltered()
		}
	}
}

// narrowFiltered re-filters the current filtered slice in-place.
// Only valid when the current filter is strictly more restrictive than before.
func (m *model) narrowFiltered() {
	n := 0
	for _, idx := range m.filtered {
		if m.matches(m.entries[idx].text) {
			m.filtered[n] = idx
			n++
		} else {
			m.entries[idx].matched = false
		}
	}
	m.filtered = m.filtered[:n]
}

// rebuildFiltered repopulates filtered from entries using the current query.
func (m *model) rebuildFiltered() {
	m.filtered = make([]int, 0, len(m.entries))
	for i := range m.entries {
		matched := m.matches(m.entries[i].text)
		m.entries[i].matched = matched
		if matched {
			m.filtered = append(m.filtered, i)
		}
	}
}

// clearQuery resets the filter and related state.
func (m *model) clearQuery() {
	m.addHistory()
	m.query = ""
	m.cursor = 0
	m.offset = 0
	m.horizontalOffset = 0
	m.historyIdx = -1
	m.recompile(false)
}

// appendEntries adds new entries, maintaining filtered and adjusting the scroll
// offset so the visible window stays pinned to the same content.
func (m *model) appendEntries(entries []entry) {
	var newMatches int
	for _, e := range entries {
		e.matched = m.matches(e.text)
		if e.matched {
			m.filtered = append(m.filtered, len(m.entries))
			newMatches++
		}
		m.entries = append(m.entries, e)
	}
	var trimMatches int
	if m.maxEntries > 0 && len(m.entries) > m.maxEntries {
		excess := len(m.entries) - m.maxEntries
		// filtered is sorted by index, so trimmed entries are a prefix.
		for _, idx := range m.filtered {
			if idx >= excess {
				break
			}
			trimMatches++
		}
		m.entries = m.entries[excess:]
		m.filtered = m.filtered[trimMatches:]
		for i := range m.filtered {
			m.filtered[i] -= excess
		}
	}
	// Adjust offset to keep viewing the same content when new entries are added
	// When scrolled up (offset > 0), increasing offset by newMatches keeps the
	// view pinned to the same position. Trimming from the start shifts indices
	// down, but since offset measures distance from the END, we automatically
	// follow the shifted content without needing to adjust for trimMatches.
	if m.offset > 0 {
		// Set flag when new data arrives while scrolled up
		if newMatches > 0 {
			m.hasNewData = true
		}
		m.offset += newMatches
	}
}

// matches reports whether s satisfies the current query in the current mode.
func (m model) matches(s string) bool {
	if m.regexMode {
		if m.compiledRe == nil {
			return true
		}
		return m.compiledRe.MatchString(s)
	}
	return matchTokens(m.tokens, s)
}

// highlightLine highlights matched portions of line for the current mode.
func (m model) highlightLine(line string) string {
	if m.regexMode {
		if m.compiledRe == nil {
			return line
		}
		return highlightRegex(m.compiledRe, line)
	}
	return highlightTokens(m.tokens, line)
}

func (m model) saveFiltered() error {
	f, err := os.Create(m.savePath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, idx := range m.filtered {
		e := m.entries[idx]
		line := e.text
		if m.showTimestamp {
			line = e.received.Format("15:04:05") + " " + line
		}
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
	cmds := []tea.Cmd{
		tea.Tick(pollInterval, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	}
	if m.stdinCh != nil {
		cmds = append(cmds, waitForStdin(m.stdinCh))
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// --- Help mode ---
		if m.showingHelp {
			helpText := m.getHelpText()
			boxHeight := min(len(helpText)+2, m.height-2)
			availHeight := boxHeight - 2 // subtract border
			maxHelpOffset := max(len(helpText)-availHeight, 0)

			switch msg.Type {
			case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlH:
				m.showingHelp = false
				m.helpOffset = 0
			case tea.KeyRunes:
				// Allow 'q' to close help
				if len(msg.Runes) == 1 && msg.Runes[0] == 'q' {
					m.showingHelp = false
					m.helpOffset = 0
				}
			case tea.KeyUp:
				m.helpOffset = max(m.helpOffset-1, 0)
			case tea.KeyDown:
				m.helpOffset = min(m.helpOffset+1, maxHelpOffset)
			case tea.KeyPgUp:
				m.helpOffset = max(m.helpOffset-availHeight, 0)
			case tea.KeyPgDown:
				m.helpOffset = min(m.helpOffset+availHeight, maxHelpOffset)
			case tea.KeyHome:
				m.helpOffset = 0
			case tea.KeyEnd:
				m.helpOffset = maxHelpOffset
			}
			return m, nil
		}

		// --- History modal mode ---
		if m.showingHistory {
			n := len(m.history)
			switch msg.Type {
			case tea.KeyEsc, tea.KeyCtrlC:
				m.showingHistory = false
			case tea.KeyRunes:
				if len(msg.Runes) != 1 {
					break
				}
				switch msg.Runes[0] {
				case 'q':
					m.showingHistory = false
				case 'd':
					m.history = append(m.history[:m.historyModalIdx], m.history[m.historyModalIdx+1:]...)
					saveHistoryFile(m.historyFile, m.history)
					if len(m.history) == 0 {
						m.showingHistory = false
					} else if m.historyModalIdx >= len(m.history) {
						m.historyModalIdx = len(m.history) - 1
					}
				}
			case tea.KeyUp:
				if m.historyModalIdx > 0 {
					m.historyModalIdx--
				}
			case tea.KeyDown:
				if m.historyModalIdx < n-1 {
					m.historyModalIdx++
				}
			case tea.KeyEnter:
				if n > 0 {
					e := m.history[m.historyModalIdx]
					m.query = e.query
					m.regexMode = e.regex
					m.cursor = len([]rune(m.query))
					m.historyIdx = -1
					m.offset = 0
					m.horizontalOffset = 0
					m.recompile(false)
					m.addHistory()
				}
				m.showingHistory = false
			}
			return m, nil
		}

		// --- Save-prompt mode ---
		if m.saving {
			// Ctrl+W deletes the previous word
			keyStr := msg.String()
			if keyStr == "ctrl+w" || msg.Type == tea.KeyCtrlW {
				m.savePath, m.saveCursor = deletePrevWord(m.savePath, m.saveCursor)
				return m, nil
			}

			switch msg.Type {
			case tea.KeyCtrlC, tea.KeyEsc:
				m.saving = false
				m.savePath = ""
				m.saveCursor = 0
			case tea.KeyEnter:
				m.saving = false
				if err := m.saveFiltered(); err != nil {
					text := "error: " + err.Error()
					m.saveMsg = saveMsgErrStyle.Render(text)
					m.saveMsgWidth = len([]rune(text))
				} else {
					text := "saved: " + m.savePath
					m.saveMsg = saveMsgOkStyle.Render(text)
					m.saveMsgWidth = len([]rune(text))
				}
				m.savePath = ""
				m.saveCursor = 0
			case tea.KeyLeft:
				m.saveCursor = max(m.saveCursor-1, 0)
			case tea.KeyCtrlLeft:
				m.saveCursor = prevWordStart(m.savePath, m.saveCursor)
			case tea.KeyRight:
				m.saveCursor = min(m.saveCursor+1, len([]rune(m.savePath)))
			case tea.KeyCtrlRight:
				m.saveCursor = nextWordStart(m.savePath, m.saveCursor)
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
		m.saveMsg = ""
		m.saveMsgWidth = 0
		avail := max(m.height-2, 0)
		maxOffset := max(len(m.filtered)-avail, 0)

		// Ctrl+H - Show help
		if msg.Type == tea.KeyCtrlH {
			m.showingHelp = true
			m.helpOffset = 0 // Reset scroll position
			return m, nil
		}

		// Ctrl+R - open history modal
		if msg.Type == tea.KeyCtrlR && len(m.history) > 0 {
			m.showingHistory = true
			m.historyModalIdx = len(m.history) - 1
			return m, nil
		}

		// Ctrl+/ (sent as Ctrl+_ by terminals) toggles regex mode
		if msg.Type == tea.KeyCtrlUnderscore {
			m.regexMode = !m.regexMode
			m.horizontalOffset = 0
			m.recompile(false)
			return m, nil
		}

		// Ctrl+W deletes the previous word
		if msg.Type == tea.KeyCtrlW {
			m.historyIdx = -1
			m.query, m.cursor = deletePrevWord(m.query, m.cursor)
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(false)
			return m, nil
		}

		switch msg.Type {
		case tea.KeyEsc:
			m.clearQuery()
		case tea.KeyCtrlQ:
			return m, tea.Quit
		case tea.KeyCtrlC:
			if m.query != "" {
				m.clearQuery()
			} else {
				return m, tea.Quit
			}
		case tea.KeyCtrlUp:
			if len(m.history) == 0 {
				break
			}
			if m.historyIdx == -1 {
				m.tempQuery = m.query
				m.tempCursor = m.cursor
				m.tempRegexMode = m.regexMode
				m.historyIdx = len(m.history) - 1
				cur := m.history[m.historyIdx]
				if cur.query == m.tempQuery && cur.regex == m.tempRegexMode && m.historyIdx > 0 {
					m.historyIdx--
				}
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.query = m.history[m.historyIdx].query
			m.regexMode = m.history[m.historyIdx].regex
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(false)
			m.cursor = len(m.queryRunes)
		case tea.KeyCtrlDown:
			if m.historyIdx == -1 {
				break
			}
			m.historyIdx++
			if m.historyIdx >= len(m.history) {
				m.query = m.tempQuery
				m.cursor = m.tempCursor
				m.regexMode = m.tempRegexMode
				m.historyIdx = -1
			} else {
				m.query = m.history[m.historyIdx].query
				m.regexMode = m.history[m.historyIdx].regex
			}
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(false)
			if m.historyIdx != -1 {
				m.cursor = len(m.queryRunes)
			}
		case tea.KeyEnter:
			m.addHistory()
			m.historyIdx = -1
			m.horizontalOffset = 0
		case tea.KeyCtrlS:
			m.saving = true
			m.savePath = ""
			m.saveCursor = 0
		case tea.KeyCtrlN:
			m.showNames = !m.showNames
		case tea.KeyCtrlT:
			m.showTimestamp = !m.showTimestamp
		case tea.KeyUp:
			m.offset = min(m.offset+1, maxOffset)
		case tea.KeyDown:
			m.offset = max(m.offset-1, 0)
			if m.offset == 0 {
				m.hasNewData = false // Clear flag when returning to bottom
			}
		case tea.KeyPgUp:
			m.offset = min(m.offset+avail, maxOffset)
		case tea.KeyPgDown:
			m.offset = max(m.offset-avail, 0)
			if m.offset == 0 {
				m.hasNewData = false // Clear flag when returning to bottom
			}
		case tea.KeyShiftLeft:
			m.horizontalOffset = max(m.horizontalOffset-10, 0)
		case tea.KeyShiftRight:
			m.horizontalOffset += 10
		case tea.KeyLeft:
			m.cursor = max(m.cursor-1, 0)
		case tea.KeyCtrlLeft:
			m.cursor = prevWordStart(m.query, m.cursor)
		case tea.KeyRight:
			m.cursor = min(m.cursor+1, len(m.queryRunes))
		case tea.KeyCtrlRight:
			m.cursor = nextWordStart(m.query, m.cursor)
		case tea.KeyHome:
			m.offset = maxOffset
			m.horizontalOffset = 0 // Reset horizontal scroll
		case tea.KeyEnd:
			m.offset = 0
			m.horizontalOffset = 0 // Reset horizontal scroll
			m.hasNewData = false   // Clear flag when jumping to bottom
		case tea.KeyBackspace:
			m.historyIdx = -1
			m.query, m.cursor = deleteRune(m.query, m.cursor)
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(false)
		case tea.KeyDelete:
			m.historyIdx = -1
			m.query = deleteRuneForward(m.query, m.cursor)
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(false)
		case tea.KeySpace:
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, []rune{' '})
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(true)
		case tea.KeyRunes:
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, msg.Runes)
			m.offset = 0
			m.horizontalOffset = 0
			m.recompile(true)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case stdinLineMsg:
		m.appendEntries([]entry{entry(msg)})
		return m, waitForStdin(m.stdinCh)

	case tickMsg:
		var lines []entry
		for _, t := range m.tailers {
			newLines, _ := t.readNew()
			now := time.Now()
			for _, l := range newLines {
				lines = append(lines, entry{file: t.path, text: l, received: now})
			}
		}
		if len(lines) > 0 {
			m.appendEntries(lines)
		}
		return m, tea.Tick(pollInterval, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return m, nil
}

func (m model) getHelpText() []string {
	return []string{
		"",
		"  ftail - Keyboard Shortcuts",
		"",
		"  Filter Editing:",
		"    ←/→              Move cursor",
		"    Ctrl+←/Ctrl+→    Jump to previous/next word",
		"    Backspace        Delete character to the left",
		"    Ctrl+W           Delete previous word",
		"    Delete           Delete character under cursor",
		"    Enter            Save query to history",
		"    Esc              Clear filter",
		"    Ctrl+C           Clear filter (if set), or exit",
		"    Ctrl+Q           Quit immediately",
		"    Ctrl+/           Toggle regex mode",
		"",
		"  Search History:",
		"    Ctrl+R           Open history picker (↑/↓ select, Enter apply, d delete)",
		"    Ctrl+↑           Step back through previous queries",
		"    Ctrl+↓           Step forward through queries",
		"",
		"  Scrolling:",
		"    ↑/↓              Scroll one line",
		"    Page Up/Down     Scroll one page",
		"    Home             Jump to oldest entry (top)",
		"    End              Jump to latest entry (resume following)",
		"    Shift+←/Shift+→  Scroll horizontally (long lines)",
		"",
		"  Actions:",
		"    Ctrl+S           Save filtered lines to file",
		"    Ctrl+N           Toggle filename prefix",
		"    Ctrl+T           Toggle timestamp prefix",
		"    Ctrl+H           Show/hide this help",
		"",
		"  Press q, Esc or Ctrl+C to close this help",
		"",
	}
}

func (m model) helpView() string {
	helpText := m.getHelpText()

	var sb strings.Builder

	// Calculate content dimensions
	maxWidth := 0
	for _, line := range helpText {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}
	contentHeight := len(helpText)

	// Center the help box
	boxWidth := min(maxWidth+4, m.width-4)
	boxHeight := min(contentHeight+2, m.height-2)

	topPadding := (m.height - boxHeight) / 2
	leftPadding := (m.width - boxWidth) / 2

	// Add top padding
	for i := 0; i < topPadding; i++ {
		sb.WriteByte('\n')
	}

	// Create the help box with border
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(0, 1).
		Width(boxWidth - 4).
		Height(boxHeight - 2)

	// Build help content with scroll offset
	var content strings.Builder
	availHeight := boxHeight - 2
	startLine := min(m.helpOffset, max(len(helpText)-availHeight, 0))
	endLine := min(startLine+availHeight, len(helpText))

	for i := startLine; i < endLine; i++ {
		if i > startLine {
			content.WriteByte('\n')
		}
		content.WriteString(helpText[i])
	}

	helpBox := helpStyle.Render(content.String())

	// Add left padding and the box
	for _, line := range strings.Split(helpBox, "\n") {
		sb.WriteString(strings.Repeat(" ", leftPadding))
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return sb.String()
}

func (m model) historyView() string {
	n := len(m.history)
	const headerLine = "  Filter History"
	const footerLine = "  ↑/↓ select · Enter apply · Esc/q cancel"

	// Compute minimum content width to fit all items, header, and footer.
	innerWidth := len([]rune(footerLine))
	for _, h := range m.history {
		if w := len([]rune(h.query)) + 7; w > innerWidth { // 7 = "  > r/ " prefix
			innerWidth = w
		}
	}
	boxWidth := min(innerWidth+4, m.width-4) // total width incl. border+padding
	contentWidth := boxWidth - 4              // lipgloss Width arg (inside border+padding)

	// How many list items fit vertically.
	// Box = border(2) + header + blank + items + blank + footer
	maxVisible := m.height - 8
	if maxVisible < 1 {
		maxVisible = 1
	}
	if n < maxVisible {
		maxVisible = n
	}
	boxHeight := maxVisible + 6
	if boxHeight > m.height-2 {
		boxHeight = m.height - 2
		maxVisible = boxHeight - 6
		if maxVisible < 1 {
			maxVisible = 1
		}
	}

	// Scroll window: keep selected item visible.
	start := 0
	if m.historyModalIdx >= maxVisible {
		start = m.historyModalIdx - maxVisible + 1
	}

	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	var content strings.Builder
	content.WriteString(headerLine)
	content.WriteByte('\n')
	content.WriteByte('\n')

	for i := start; i < start+maxVisible; i++ {
		if i > start {
			content.WriteByte('\n')
		}
		e := m.history[i] // oldest first
		maxItemChars := contentWidth - 7 // reserve space for "  > r/ " prefix
		if maxItemChars < 0 {
			maxItemChars = 0
		}
		query := e.query
		if len([]rune(query)) > maxItemChars {
			query = string([]rune(query)[:maxItemChars])
		}
		if i == m.historyModalIdx {
			modePfx := "   "
			if e.regex {
				modePfx = "r/ "
			}
			content.WriteString("  ")
			content.WriteString(selectedStyle.Render("> " + modePfx + query))
		} else {
			if e.regex {
				content.WriteString("    ")
				content.WriteString(reStyle.Render("r/ "))
				content.WriteString(query)
			} else {
				content.WriteString("       ")
				content.WriteString(query)
			}
		}
	}

	content.WriteByte('\n')
	content.WriteByte('\n')
	content.WriteString(fileStyle.Render(footerLine))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(0, 1).
		Width(contentWidth).
		Height(boxHeight - 2)

	box := boxStyle.Render(content.String())

	topPad := (m.height - boxHeight) / 2
	leftPad := (m.width - boxWidth) / 2
	if topPad < 0 {
		topPad = 0
	}
	if leftPad < 0 {
		leftPad = 0
	}

	var sb strings.Builder
	for i := 0; i < topPad; i++ {
		sb.WriteByte('\n')
	}
	for _, line := range strings.Split(box, "\n") {
		sb.WriteString(strings.Repeat(" ", leftPad))
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (m model) View() string {
	// Show help modal if active
	if m.showingHelp {
		return m.helpView()
	}

	// Show history modal if active
	if m.showingHistory {
		return m.historyView()
	}

	filtered := m.filtered

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

	for _, idx := range visible {
		e := m.entries[idx]
		prefixWidth := 0
		if m.showTimestamp {
			prefixWidth += 9 // "15:04:05 "
		}
		if m.showNames {
			prefixWidth += len([]rune(e.file)) + 2
		}

		// Apply horizontal scrolling window to the log text
		text := e.text
		if m.width > 0 {
			avail := m.width - prefixWidth
			textRunes := []rune(text)
			textLen := len(textRunes)

			// Apply horizontal offset
			start := min(m.horizontalOffset, textLen)
			end := min(start+avail, textLen)

			if start >= textLen {
				text = ""
			} else {
				text = string(textRunes[start:end])

				// Add visual indicators for scrollable content
				if m.horizontalOffset > 0 && len(text) > 0 {
					text = "‹" + text[1:] // Left indicator
				}
				if end < textLen && len(text) > 0 {
					text = text[:len(text)-1] + "›" // Right indicator
				}
			}
		}

		if m.showTimestamp {
			sb.WriteString(fileStyle.Render(e.received.Format("15:04:05") + " "))
		}
		if m.showNames {
			style := fileStyle
			sb.WriteString(style.Render(e.file + ": "))
		}
		sb.WriteString(m.highlightLine(text))
		sb.WriteByte('\n')
	}

	// Separator rule — green when following, orange when scrolled.
	// Show indicator if new data arrived while scrolled up.
	ruleStyle := ruleFollowStyle
	var ruleText string
	if m.offset > 0 {
		ruleStyle = ruleScrollStyle
		if m.hasNewData {
			// Add visual indicator for new data
			indicator := " ↓ New "
			indicatorLen := len([]rune(indicator))
			if m.width > indicatorLen+10 {
				// Center the indicator
				leftRules := (m.width - indicatorLen) / 2
				rightRules := m.width - indicatorLen - leftRules
				ruleText = strings.Repeat("─", leftRules) + indicator + strings.Repeat("─", rightRules)
			} else {
				ruleText = strings.Repeat("─", m.width)
			}
		} else {
			ruleText = strings.Repeat("─", m.width)
		}
	} else {
		ruleText = strings.Repeat("─", m.width)
	}
	sb.WriteString(ruleStyle.Render(ruleText))
	sb.WriteByte('\n')

	var counterText string
	if m.query != "" {
		counterText = fmt.Sprintf("%d/%d", len(filtered), len(m.entries))
	} else if m.maxEntries == 0 {
		counterText = fmt.Sprintf("%d/∞", len(m.entries))
	} else {
		counterText = fmt.Sprintf("%d/%d", len(m.entries), m.maxEntries)
	}
	counter := fileStyle.Render(counterText)
	counterWidth := len([]rune(counterText))

	var prompt string
	var promptWidth int
	if m.saving {
		spRunes := []rune(m.savePath)
		spBefore := string(spRunes[:m.saveCursor])
		spAfter := string(spRunes[m.saveCursor:])
		cursorCh := " "
		if m.saveCursor < len(spRunes) {
			cursorCh = string(spRunes[m.saveCursor])
			spAfter = string(spRunes[m.saveCursor+1:])
		}
		prompt = saveStyle.Render("save: ") + spBefore + cursorStyle.Render(cursorCh) + spAfter
		promptWidth = 6 + len(spRunes) + 1
	} else if m.saveMsg != "" {
		prompt = m.saveMsg
		promptWidth = m.saveMsgWidth
	} else if m.regexMode {
		pStyle := reStyle
		if m.reErr != nil {
			pStyle = reErrStyle
		}
		cursorCh := " "
		after := m.queryRunes[m.cursor:]
		if m.cursor < len(m.queryRunes) {
			cursorCh = string(m.queryRunes[m.cursor])
			after = m.queryRunes[m.cursor+1:]
		}
		prompt = pStyle.Render("regex/ ") + string(m.queryRunes[:m.cursor]) + cursorStyle.Render(cursorCh) + string(after)
		promptWidth = 7 + len(m.queryRunes) + 1
	} else {
		cursorCh := " "
		after := m.queryRunes[m.cursor:]
		if m.cursor < len(m.queryRunes) {
			cursorCh = string(m.queryRunes[m.cursor])
			after = m.queryRunes[m.cursor+1:]
		}
		prompt = searchBarStyle.Render("/ ") + string(m.queryRunes[:m.cursor]) + cursorStyle.Render(cursorCh) + string(after)
		promptWidth = 2 + len(m.queryRunes) + 1
	}
	// Replace counter with regex error when pattern is invalid.
	if m.regexMode && m.reErr != nil {
		errText := "  " + m.reErr.Error()
		maxErrWidth := m.width - promptWidth
		if len([]rune(errText)) > maxErrWidth {
			errText = string([]rune(errText)[:maxErrWidth])
		}
		sb.WriteString(prompt + reErrStyle.Render(errText))
		return sb.String()
	}
	pad := m.width - promptWidth - counterWidth
	if pad > 0 {
		prompt += strings.Repeat(" ", pad)
	}
	sb.WriteString(prompt + counter)

	return sb.String()
}
