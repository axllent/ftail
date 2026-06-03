package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	filterGen         int    // incremented on each filter query change; used to discard stale results
	appliedTokens    []token // tokens that produced the current filtered set (plain-mode only)
	appliedRegexMode bool    // true when the current filtered set came from a regex-mode run
	filterInFlight   int     // number of dispatched filter goroutines that have not yet returned
}

// filterResultMsg carries the result of an async filter computation.
type filterResultMsg struct {
	gen      int
	snapLen  int
	filtered []int
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
	_ = scanner.Err()
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

// reparse updates queryRunes, tokens/compiledRe, and reErr from the current
// query and regexMode, without touching filtered. Always call this before
// filterCmd so that View() reflects the new query immediately.
func (m *model) reparse() {
	m.queryRunes = []rune(m.query)
	if m.regexMode && m.query != "" {
		if m.query != m.lastCompiledQuery || m.compiledRe == nil {
			m.compiledRe, m.reErr = regexp.Compile("(?i)" + m.query)
			m.lastCompiledQuery = m.query
		}
		m.tokens = nil
	} else {
		m.compiledRe = nil
		m.lastCompiledQuery = ""
		m.reErr = nil
		m.tokens = parseTokens(m.query)
	}
}

// filterCmd increments filterGen and returns a Cmd that computes filtered
// asynchronously. The result is delivered as a filterResultMsg; stale results
// (superseded by a newer keypress or a trim) are silently discarded.
//
// When the new tokens strictly narrow the previously-applied tokens (plain
// mode only), the goroutine filters the previous result instead of every
// entry — typically a large speedup while the user is typing.
func (m *model) filterCmd() tea.Cmd {
	m.filterGen++
	m.filterInFlight++
	gen := m.filterGen
	entries := m.entries // snapshot of slice header; safe — only main loop appends/trims
	snapLen := len(entries)
	re := m.compiledRe // *regexp.Regexp is safe for concurrent MatchString calls
	tokens := m.tokens // immutable once set by reparse
	regexMode := m.regexMode

	// Safe to narrow only when both the previous and current runs are plain
	// mode. Copy m.filtered because appendEntries mutates it in place on trim.
	var prevFiltered []int
	if !regexMode && !m.appliedRegexMode && tokensNarrow(m.appliedTokens, tokens) {
		prevFiltered = make([]int, len(m.filtered))
		copy(prevFiltered, m.filtered)
	}

	return func() tea.Msg {
		var filtered []int
		if prevFiltered != nil {
			filtered = make([]int, 0, len(prevFiltered))
			for _, i := range prevFiltered {
				if i < snapLen && matchTokens(tokens, entries[i].text) {
					filtered = append(filtered, i)
				}
			}
		} else {
			// An empty plain-mode query matches every entry; size exactly to
			// avoid the regrowth chain from a too-small initial cap. Otherwise
			// guess at a quarter — selective queries waste little, and a few
			// regrowths on the way up are cheap.
			initCap := snapLen / 4
			if !regexMode && len(tokens) == 0 {
				initCap = snapLen
			}
			filtered = make([]int, 0, initCap)
			for i, e := range entries {
				var matched bool
				if regexMode {
					matched = re == nil || re.MatchString(e.text)
				} else {
					matched = matchTokens(tokens, e.text)
				}
				if matched {
					filtered = append(filtered, i)
				}
			}
		}
		return filterResultMsg{gen: gen, snapLen: snapLen, filtered: filtered}
	}
}

// initFiltered builds filtered synchronously from all entries. Only called
// once at startup, before the event loop begins.
func (m *model) initFiltered() {
	m.filtered = make([]int, 0, len(m.entries))
	for i, e := range m.entries {
		if m.matches(e.text) {
			m.filtered = append(m.filtered, i)
		}
	}
	m.appliedTokens = m.tokens
	m.appliedRegexMode = m.regexMode
}

// clearQuery resets the filter and related state.
func (m *model) clearQuery() tea.Cmd {
	m.addHistory()
	m.query = ""
	m.cursor = 0
	m.offset = 0
	m.horizontalOffset = 0
	m.historyIdx = -1
	m.reparse()
	return m.filterCmd()
}

// appendEntries adds new entries, maintaining filtered and adjusting the scroll
// offset so the visible window stays pinned to the same content.
func (m *model) appendEntries(entries []entry) tea.Cmd {
	var newMatches int
	for _, e := range entries {
		if m.matches(e.text) {
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
		if m.filterInFlight == 0 {
			// No goroutine is reading the backing array — safe to shift in
			// place. This overwrites the trimmed slots' string references so
			// GC can reclaim the old line bytes immediately, instead of
			// pinning them until append() eventually reallocates.
			copy(m.entries, m.entries[excess:])
			clear(m.entries[m.maxEntries:])
			m.entries = m.entries[:m.maxEntries]
		} else {
			// A filter goroutine has captured the backing array via a slice
			// snapshot; rewriting positions [0, snapLen) would race with its
			// reads. Reslice instead — the dropped strings stay pinned until
			// the array is reallocated, which is acceptable since the
			// snapshot already pins them anyway.
			m.entries = m.entries[excess:]
		}
		m.filtered = m.filtered[trimMatches:]
		for i := range m.filtered {
			m.filtered[i] -= excess
		}
		// Invalidate any in-flight filterResultMsg — its indices are pre-trim.
		// m.filtered has already been adjusted in-line, so no fresh filter run
		// is needed; bumping the gen is enough to discard the stale result.
		m.filterGen++
		return nil
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
	return nil
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
	case tea.KeyPressMsg:
		// --- Help mode ---
		if m.showingHelp {
			helpText := m.getHelpText()
			boxHeight := min(len(helpText)+2, m.height-2)
			availHeight := boxHeight - 2 // subtract border
			maxHelpOffset := max(len(helpText)-availHeight, 0)

			switch msg.String() {
			case "esc", "ctrl+c", "ctrl+h", "q":
				m.showingHelp = false
				m.helpOffset = 0
			case "up":
				m.helpOffset = max(m.helpOffset-1, 0)
			case "down":
				m.helpOffset = min(m.helpOffset+1, maxHelpOffset)
			case "pgup":
				m.helpOffset = max(m.helpOffset-availHeight, 0)
			case "pgdown":
				m.helpOffset = min(m.helpOffset+availHeight, maxHelpOffset)
			case "home":
				m.helpOffset = 0
			case "end":
				m.helpOffset = maxHelpOffset
			}
			return m, nil
		}

		// --- History modal mode ---
		if m.showingHistory {
			n := len(m.history)
			switch msg.String() {
			case "esc", "ctrl+c", "q":
				m.showingHistory = false
			case "d":
				m.history = append(m.history[:m.historyModalIdx], m.history[m.historyModalIdx+1:]...)
				saveHistoryFile(m.historyFile, m.history)
				if len(m.history) == 0 {
					m.showingHistory = false
				} else if m.historyModalIdx >= len(m.history) {
					m.historyModalIdx = len(m.history) - 1
				}
			case "up":
				if m.historyModalIdx > 0 {
					m.historyModalIdx--
				}
			case "down":
				if m.historyModalIdx < n-1 {
					m.historyModalIdx++
				}
			case "enter":
				if n > 0 {
					e := m.history[m.historyModalIdx]
					m.query = e.query
					m.regexMode = e.regex
					m.cursor = len([]rune(m.query))
					m.historyIdx = -1
					m.offset = 0
					m.horizontalOffset = 0
					m.reparse()
					m.addHistory()
				}
				m.showingHistory = false
			}
			return m, m.filterCmd()
		}

		// --- Save-prompt mode ---
		if m.saving {
			switch msg.String() {
			case "ctrl+w":
				m.savePath, m.saveCursor = deletePrevWord(m.savePath, m.saveCursor)
			case "ctrl+c", "esc":
				m.saving = false
				m.savePath = ""
				m.saveCursor = 0
			case "enter":
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
			case "left":
				m.saveCursor = max(m.saveCursor-1, 0)
			case "ctrl+left":
				m.saveCursor = prevWordStart(m.savePath, m.saveCursor)
			case "right":
				m.saveCursor = min(m.saveCursor+1, len([]rune(m.savePath)))
			case "ctrl+right":
				m.saveCursor = nextWordStart(m.savePath, m.saveCursor)
			case "home":
				m.saveCursor = 0
			case "end":
				m.saveCursor = len([]rune(m.savePath))
			case "backspace":
				m.savePath, m.saveCursor = deleteRune(m.savePath, m.saveCursor)
			case "delete":
				m.savePath = deleteRuneForward(m.savePath, m.saveCursor)
			case "space":
				m.savePath, m.saveCursor = insertRunes(m.savePath, m.saveCursor, []rune{' '})
			default:
				if len(msg.Text) > 0 {
					m.savePath, m.saveCursor = insertRunes(m.savePath, m.saveCursor, []rune(msg.Text))
				}
			}
			return m, nil
		}

		// --- Normal mode ---
		m.saveMsg = ""
		m.saveMsgWidth = 0
		avail := max(m.height-2, 0)
		maxOffset := max(len(m.filtered)-avail, 0)

		switch msg.String() {
		case "ctrl+h":
			m.showingHelp = true
			m.helpOffset = 0
			return m, nil
		case "ctrl+r":
			if len(m.history) > 0 {
				m.showingHistory = true
				m.historyModalIdx = len(m.history) - 1
				return m, nil
			}
		case "ctrl+_":
			// Ctrl+/ (sent as Ctrl+_ by terminals) toggles regex mode
			m.regexMode = !m.regexMode
			m.horizontalOffset = 0
			m.reparse()
			return m, m.filterCmd()
		case "ctrl+w":
			m.historyIdx = -1
			m.query, m.cursor = deletePrevWord(m.query, m.cursor)
			m.offset = 0
			m.horizontalOffset = 0
			m.reparse()
			return m, m.filterCmd()
		case "esc":
			return m, m.clearQuery()
		case "ctrl+q":
			return m, tea.Quit
		case "ctrl+c":
			if m.query != "" {
				return m, m.clearQuery()
			} else {
				return m, tea.Quit
			}
		case "ctrl+up":
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
			m.reparse()
			m.cursor = len(m.queryRunes)
			return m, m.filterCmd()
		case "ctrl+down":
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
			m.reparse()
			if m.historyIdx != -1 {
				m.cursor = len(m.queryRunes)
			}
			return m, m.filterCmd()
		case "enter":
			m.addHistory()
			m.historyIdx = -1
			m.horizontalOffset = 0
		case "ctrl+s":
			m.saving = true
			m.savePath = ""
			m.saveCursor = 0
		case "ctrl+n":
			m.showNames = !m.showNames
		case "ctrl+t":
			m.showTimestamp = !m.showTimestamp
		case "up":
			m.offset = min(m.offset+1, maxOffset)
		case "down":
			m.offset = max(m.offset-1, 0)
			if m.offset == 0 {
				m.hasNewData = false // Clear flag when returning to bottom
			}
		case "pgup":
			m.offset = min(m.offset+avail, maxOffset)
		case "pgdown":
			m.offset = max(m.offset-avail, 0)
			if m.offset == 0 {
				m.hasNewData = false // Clear flag when returning to bottom
			}
		case "shift+left":
			m.horizontalOffset = max(m.horizontalOffset-10, 0)
		case "shift+right":
			m.horizontalOffset += 10
		case "left":
			m.cursor = max(m.cursor-1, 0)
		case "ctrl+left":
			m.cursor = prevWordStart(m.query, m.cursor)
		case "right":
			m.cursor = min(m.cursor+1, len(m.queryRunes))
		case "ctrl+right":
			m.cursor = nextWordStart(m.query, m.cursor)
		case "home":
			m.offset = maxOffset
			m.horizontalOffset = 0 // Reset horizontal scroll
		case "end":
			m.offset = 0
			m.horizontalOffset = 0 // Reset horizontal scroll
			m.hasNewData = false   // Clear flag when jumping to bottom
		case "backspace":
			m.historyIdx = -1
			m.query, m.cursor = deleteRune(m.query, m.cursor)
			m.offset = 0
			m.horizontalOffset = 0
			m.reparse()
			return m, m.filterCmd()
		case "delete":
			m.historyIdx = -1
			m.query = deleteRuneForward(m.query, m.cursor)
			m.offset = 0
			m.horizontalOffset = 0
			m.reparse()
			return m, m.filterCmd()
		case "space":
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, []rune{' '})
			m.offset = 0
			m.horizontalOffset = 0
			m.reparse()
			return m, m.filterCmd()
		default:
			if len(msg.Text) > 0 {
				m.historyIdx = -1
				m.query, m.cursor = insertRunes(m.query, m.cursor, []rune(msg.Text))
				m.offset = 0
				m.horizontalOffset = 0
				m.reparse()
				return m, m.filterCmd()
			}
		}

	case tea.PasteMsg:
		if m.showingHelp || m.showingHistory {
			return m, nil
		}
		text := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg.Content)
		if text == "" {
			return m, nil
		}
		if m.saving {
			m.savePath, m.saveCursor = insertRunes(m.savePath, m.saveCursor, []rune(text))
			return m, nil
		}
		m.saveMsg = ""
		m.saveMsgWidth = 0
		m.historyIdx = -1
		m.query, m.cursor = insertRunes(m.query, m.cursor, []rune(text))
		m.offset = 0
		m.horizontalOffset = 0
		m.reparse()
		return m, m.filterCmd()

	case filterResultMsg:
		m.filterInFlight-- // every dispatched goroutine sends exactly one msg, even stale ones
		if msg.gen != m.filterGen {
			break // superseded by a newer query or trim
		}
		m.filtered = msg.filtered
		// Gen match guarantees m.tokens/m.regexMode are the same values the
		// goroutine used, so they describe the basis of m.filtered.
		m.appliedTokens = m.tokens
		m.appliedRegexMode = m.regexMode
		// Append any entries that arrived after the snapshot was taken.
		for i := msg.snapLen; i < len(m.entries); i++ {
			if m.matches(m.entries[i].text) {
				m.filtered = append(m.filtered, i)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case stdinLineMsg:
		cmd := m.appendEntries([]entry{entry(msg)})
		return m, tea.Batch(waitForStdin(m.stdinCh), cmd)

	case tickMsg:
		var lines []entry
		for _, t := range m.tailers {
			newLines, _ := t.readNew()
			now := time.Now()
			for _, l := range newLines {
				lines = append(lines, entry{file: t.path, text: l, received: now})
			}
		}
		trimCmd := tea.Cmd(nil)
		if len(lines) > 0 {
			trimCmd = m.appendEntries(lines)
		}
		return m, tea.Batch(tea.Tick(pollInterval, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}), trimCmd)
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
		"    Ctrl+Shift+V     Paste from clipboard (middle-click also works)",
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
	const footerLine = "  ↑/↓ select · Enter apply · d delete · Esc/q cancel"

	// Compute minimum content width to fit all items, header, and footer.
	innerWidth := len([]rune(footerLine))
	for _, h := range m.history {
		if w := len([]rune(h.query)) + 7; w > innerWidth { // 7 = "  > r/ " prefix
			innerWidth = w
		}
	}
	boxWidth := min(innerWidth+4, m.width-4) // total width incl. border+padding
	contentWidth := boxWidth - 4             // lipgloss Width arg (inside border+padding)

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

	// Scroll window: keep selected item centred.
	start := max(m.historyModalIdx-maxVisible/2, 0)
	if start+maxVisible > n {
		start = max(n-maxVisible, 0)
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
		e := m.history[i]                // oldest first
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

func (m model) View() tea.View {
	// Show help modal if active
	if m.showingHelp {
		v := tea.NewView(m.helpView())
		v.AltScreen = true
		return v
	}

	// Show history modal if active
	if m.showingHistory {
		v := tea.NewView(m.historyView())
		v.AltScreen = true
		return v
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
		_, _ = sb.WriteString(prompt + reErrStyle.Render(errText))
		v := tea.NewView(sb.String())
		v.AltScreen = true
		return v
	}
	pad := m.width - promptWidth - counterWidth
	if pad > 0 {
		prompt += strings.Repeat(" ", pad)
	}
	_, _ = sb.WriteString(prompt + counter)

	v := tea.NewView(sb.String())
	v.AltScreen = true
	return v
}
