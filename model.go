package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

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
	offset            int // rows scrolled up from the bottom; 0 = follow latest
	saving            bool
	savePath          string
	saveCursor        int
	saveMsg           string // status shown after a save attempt
	saveMsgWidth      int    // visible (unstyled) rune width of saveMsg
	regexMode         bool
	compiledRe        *regexp.Regexp
	lastCompiledQuery string // query string used to produce compiledRe
	reErr             error
	history           []string
	historyIdx        int    // -1 = not browsing; >= 0 = index into history
	tempQuery         string // query saved before history browsing began
	tempCursor        int
}

const maxHistory = 100

// addHistory appends query to history, deduplicating and capping the size.
func (m *model) addHistory() {
	if m.query == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == m.query {
		return
	}
	m.history = append(m.history, m.query)
	if len(m.history) > maxHistory {
		m.history = m.history[len(m.history)-maxHistory:]
	}
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
	if m.offset > 0 {
		m.offset = max(m.offset+newMatches-trimMatches, 0)
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
		switch msg.Type {
		case tea.KeyEsc:
			m.clearQuery()
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
				m.historyIdx = len(m.history) - 1
				if m.history[m.historyIdx] == m.tempQuery && m.historyIdx > 0 {
					m.historyIdx--
				}
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.query = m.history[m.historyIdx]
			m.offset = 0
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
				m.historyIdx = -1
			} else {
				m.query = m.history[m.historyIdx]
			}
			m.offset = 0
			m.recompile(false)
			if m.historyIdx != -1 {
				m.cursor = len(m.queryRunes)
			}
		case tea.KeyCtrlR:
			m.regexMode = !m.regexMode
			m.recompile(false)
		case tea.KeyEnter:
			m.addHistory()
			m.historyIdx = -1
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
		case tea.KeyCtrlLeft:
			m.cursor = prevWordStart(m.query, m.cursor)
		case tea.KeyRight:
			m.cursor = min(m.cursor+1, len(m.queryRunes))
		case tea.KeyCtrlRight:
			m.cursor = nextWordStart(m.query, m.cursor)
		case tea.KeyHome:
			m.offset = maxOffset
		case tea.KeyEnd:
			m.offset = 0
		case tea.KeyBackspace:
			m.historyIdx = -1
			m.query, m.cursor = deleteRune(m.query, m.cursor)
			m.offset = 0
			m.recompile(false)
		case tea.KeyDelete:
			m.historyIdx = -1
			m.query = deleteRuneForward(m.query, m.cursor)
			m.offset = 0
			m.recompile(false)
		case tea.KeySpace:
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, []rune{' '})
			m.offset = 0
			m.recompile(true)
		case tea.KeyRunes:
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, msg.Runes)
			m.offset = 0
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

func (m model) View() string {
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

		// Truncate the log text to the space remaining after the prefix.
		text := e.text
		if m.width > 0 {
			avail := m.width - prefixWidth
			if utf8.RuneCountInString(text) > avail {
				text = string([]rune(text)[:avail-1]) + "…"
			}
		}

		if m.showTimestamp {
			sb.WriteString(fileStyle.Render(e.received.Format("15:04:05") + " "))
		}
		if m.showNames {
			style := fileStyle
			if s, ok := m.fileColours[e.file]; ok {
				style = s
			}
			sb.WriteString(style.Render(e.file + ": "))
		}
		sb.WriteString(m.highlightLine(text))
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
		promptWidth = m.saveMsgWidth
	} else if m.regexMode {
		pStyle := reStyle
		if m.reErr != nil {
			pStyle = reErrStyle
		}
		prompt = pStyle.Render("r/ ") + string(m.queryRunes[:m.cursor]) + "█" + string(m.queryRunes[m.cursor:])
		promptWidth = 3 + len(m.queryRunes) + 1
	} else {
		prompt = searchBarStyle.Render("/ ") + string(m.queryRunes[:m.cursor]) + "█" + string(m.queryRunes[m.cursor:])
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
