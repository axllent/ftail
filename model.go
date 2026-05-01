package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
		case tea.KeyEsc:
			m.query = ""
			m.cursor = 0
			m.offset = 0
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
