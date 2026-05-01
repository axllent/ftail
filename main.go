// Package main is the application
package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	flag "github.com/spf13/pflag"
)

func main() {
	var showNames bool
	var showTimestamp bool
	var nLines int
	var maxEntries int
	flag.BoolVarP(&showNames, "filename", "f", false, "prefix each line with the source filename")
	flag.BoolVarP(&showTimestamp, "timestamp", "t", false, "prefix each line with the received timestamp")
	flag.IntVarP(&nLines, "lines", "n", 0, "number of existing lines to show on start")
	flag.IntVarP(&maxEntries, "max", "m", 10000, "maximum number of lines to keep in the buffer")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: ftail [-f] [-n lines] [-m max] <file> [file ...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Follow one or more files, printing new lines as they are written.")
		fmt.Fprintln(os.Stderr, "Type to filter lines; press Ctrl+C or Esc to exit.")
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

	fileColours := make(map[string]lipgloss.Style, len(paths))
	for i, path := range paths {
		fileColours[path] = lipgloss.NewStyle().Foreground(filePalette[i%len(filePalette)])
	}

	startTime := time.Now()
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
			initialEntries = append(initialEntries, entry{file: path, text: l, received: startTime})
		}
	}

	p := tea.NewProgram(model{
		tailers:       tailers,
		showNames:     showNames,
		showTimestamp: showTimestamp,
		entries:       initialEntries,
		maxEntries:    maxEntries,
		fileColours:   fileColours,
		historyIdx:    -1,
	}, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
