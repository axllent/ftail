// Package main is the application
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/axllent/ghru/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	flag "github.com/spf13/pflag"
)

var (
	// Version is the current version of ftail, set at build time
	Version = "dev"
)

func main() {
	var showNames bool
	var showTimestamp bool
	var limit int
	var filter string
	var update bool
	var showVersion bool
	flag.BoolVar(&showNames, "filename", false, "prefix each line with the source filename")
	flag.BoolVarP(&showTimestamp, "timestamp", "t", false, "prefix each line with the received timestamp")
	flag.IntVarP(&limit, "limit", "l", 200000, "maximum number of lines to process (0 = unlimited)")
	flag.StringVarP(&filter, "filter", "f", "", "preset filter query")
	flag.BoolVarP(&update, "update", "u", false, "check for updates and self-update if available")
	flag.BoolVarP(&showVersion, "version", "v", false, "display version and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: ftail [-f filter] [-l limit] <file> [file ...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Follow one or more files, printing new lines as they are written.")
		fmt.Fprintln(os.Stderr, "Type to filter lines; press Ctrl+C to exit.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if limit < 0 {
		fmt.Fprintln(os.Stderr, "ftail: -l must be >= 0")
		os.Exit(1)
	}

	ghruConf := &ghru.Config{
		Repo:           "axllent/ftail",
		BinaryName:     "ftail",
		ArchiveName:    "ftail-{{.OS}}-{{.Arch}}",
		CurrentVersion: Version,
	}

	// Handle version flag
	if showVersion {
		fmt.Printf("Version: %s\n", Version)

		release, err := ghruConf.Latest()
		if err != nil {
			fmt.Printf("Error checking for latest release: %s\n", err)
			os.Exit(1)
		}

		// The latest version is the same version
		if release.Tag == Version {
			os.Exit(0)
		}

		// A newer release is available
		fmt.Printf(
			"Update available: %s\nRun `%s --update` to update (requires read/write access to install directory).\n",
			release.Tag,
			os.Args[0],
		)
		os.Exit(0)
	}

	// Handle update flag
	if update {
		release, err := ghruConf.SelfUpdate()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
			os.Exit(1)
		}
		if release.Tag == Version {
			fmt.Printf("Already running the latest version: %s\n", release.Tag)
		} else {
			fmt.Printf("Updated to version %s\n", release.Tag)
		}
		os.Exit(0)
	}

	// Detect piped stdin.
	var stdinCh chan entry
	stdinStat, _ := os.Stdin.Stat()
	stdinPiped := (stdinStat.Mode() & os.ModeCharDevice) == 0

	paths := flag.Args()
	if len(paths) == 0 && !stdinPiped {
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
		t, initial, err := newTailer(path, limit)
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

	opts := []tea.ProgramOption{tea.WithAltScreen()}

	if stdinPiped {
		stdinCh = make(chan entry, 256)
		// Read keyboard events from the terminal directly.
		tty, err := os.Open("/dev/tty")
		if err != nil {
			fmt.Fprintln(os.Stderr, "ftail: cannot open /dev/tty for keyboard input:", err)
			os.Exit(1)
		}
		opts = append(opts, tea.WithInput(tty))
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				stdinCh <- entry{file: "stdin", text: scanner.Text(), received: time.Now()}
			}
			close(stdinCh)
		}()
	}

	var historyFile string
	if home, err := os.UserHomeDir(); err == nil {
		historyFile = filepath.Join(home, ".ftailhst")
	}

	m := model{
		tailers:       tailers,
		stdinCh:       stdinCh,
		showNames:     showNames,
		showTimestamp: showTimestamp,
		entries:       initialEntries,
		maxEntries:    limit,
		fileColours:   fileColours,
		query:         filter,
		cursor:        len([]rune(filter)),
		historyIdx:    -1,
		history:       loadHistoryFile(historyFile),
		historyFile:   historyFile,
	}
	m.recompile(false) // initialise tokens, queryRunes, and filtered from initialEntries

	p := tea.NewProgram(m, opts...)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
