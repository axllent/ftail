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

type model struct {
	tailers     []*tailer
	showNames   bool
	entries     []entry
	maxEntries  int
	fileColours map[string]lipgloss.Style
	query       string
	cursor      int // rune index within query
	width       int
	height      int
	offset      int // rows scrolled up from the bottom; 0 = follow latest
	saving      bool
	savePath    string
	saveCursor  int
	saveMsg     string // status shown after a save attempt
	regexMode   bool
	compiledRe  *regexp.Regexp
	reErr       error
	history     []string
	historyIdx  int    // -1 = not browsing; >= 0 = index into history
	tempQuery   string // query saved before history browsing began
	tempCursor  int
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

// recompile updates compiledRe/reErr from the current query when in regex mode.
func (m *model) recompile() {
	if m.regexMode && m.query != "" {
		m.compiledRe, m.reErr = regexp.Compile("(?i)" + m.query)
	} else {
		m.compiledRe = nil
		m.reErr = nil
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
	return match(m.query, s)
}

// highlightLine highlights matched portions of line for the current mode.
func (m model) highlightLine(line string) string {
	if m.regexMode {
		if m.compiledRe == nil {
			return line
		}
		return highlightRegex(m.compiledRe, line)
	}
	return highlight(m.query, line)
}

func (m model) saveFiltered() error {
	f, err := os.Create(m.savePath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range m.entries {
		if !m.matches(e.text) {
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
			if m.matches(e.text) {
				filteredCount++
			}
		}
		maxOffset := max(filteredCount-avail, 0)
		switch msg.Type {
		case tea.KeyEsc:
			m.addHistory()
			m.query = ""
			m.cursor = 0
			m.offset = 0
			m.historyIdx = -1
			m.recompile()
		case tea.KeyCtrlC:
			if m.query != "" {
				m.addHistory()
				m.query = ""
				m.cursor = 0
				m.offset = 0
				m.historyIdx = -1
				m.recompile()
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
			m.cursor = len([]rune(m.query))
			m.offset = 0
			m.recompile()
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
				m.cursor = len([]rune(m.query))
			}
			m.offset = 0
			m.recompile()
		case tea.KeyCtrlR:
			m.regexMode = !m.regexMode
			m.recompile()
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
		case tea.KeyRight:
			m.cursor = min(m.cursor+1, len([]rune(m.query)))
		case tea.KeyHome:
			m.offset = maxOffset
		case tea.KeyEnd:
			m.offset = 0
		case tea.KeyBackspace:
			m.historyIdx = -1
			m.query, m.cursor = deleteRune(m.query, m.cursor)
			m.offset = 0
			m.recompile()
		case tea.KeyDelete:
			m.historyIdx = -1
			m.query = deleteRuneForward(m.query, m.cursor)
			m.offset = 0
			m.recompile()
		case tea.KeySpace:
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, []rune{' '})
			m.offset = 0
			m.recompile()
		case tea.KeyRunes:
			m.historyIdx = -1
			m.query, m.cursor = insertRunes(m.query, m.cursor, msg.Runes)
			m.offset = 0
			m.recompile()
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
				if m.offset > 0 && m.matches(e.text) {
					newMatches++
				}
			}
		}
		var trimMatches int
		if m.maxEntries > 0 && len(m.entries) > m.maxEntries {
			excess := len(m.entries) - m.maxEntries
			if m.offset > 0 {
				for _, e := range m.entries[:excess] {
					if m.matches(e.text) {
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
		if m.matches(e.text) {
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
			style := fileStyle
			if s, ok := m.fileColours[e.file]; ok {
				style = s
			}
			sb.WriteString(style.Render(prefix))
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
		promptWidth = len([]rune(m.saveMsg)) // approximate; ANSI codes ignored for padding
	} else if m.regexMode {
		qRunes := []rune(m.query)
		before := string(qRunes[:m.cursor])
		after := string(qRunes[m.cursor:])
		pStyle := reStyle
		if m.reErr != nil {
			pStyle = reErrStyle
		}
		prompt = pStyle.Render("r/ ") + before + "█" + after
		promptWidth = 3 + len(qRunes) + 1
	} else {
		qRunes := []rune(m.query)
		before := string(qRunes[:m.cursor])
		after := string(qRunes[m.cursor:])
		prompt = searchBarStyle.Render("/ ") + before + "█" + after
		promptWidth = 2 + len(qRunes) + 1
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
