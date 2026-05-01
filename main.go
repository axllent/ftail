// Package main is the application
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const pollInterval = 200 * time.Millisecond

type tailer struct {
	path   string
	file   *os.File
	offset int64
}

func newTailer(path string) (*tailer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &tailer{path: path, file: f, offset: offset}, nil
}

func (t *tailer) readNew(prefix string) error {
	info, err := os.Stat(t.path)
	if err != nil {
		return err
	}

	// File was truncated or rotated
	if info.Size() < t.offset {
		t.file.Close()
		f, err := os.Open(t.path)
		if err != nil {
			return err
		}
		t.file = f
		t.offset = 0
	}

	if info.Size() == t.offset {
		return nil
	}

	if _, err := t.file.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}

	scanner := bufio.NewScanner(t.file)
	for scanner.Scan() {
		fmt.Printf("%s%s\n", prefix, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	t.offset, err = t.file.Seek(0, io.SeekCurrent)
	return err
}

func (t *tailer) close() {
	t.file.Close()
}

func main() {
	var showNames bool
	flag.BoolVar(&showNames, "n", false, "prefix each line with the filename")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ftail [-n] <file> [file ...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Follow one or more files, printing new lines as they are written.")
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

	prefix := showNames || len(paths) > 1
	tailers := make([]*tailer, 0, len(paths))

	for _, path := range paths {
		t, err := newTailer(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ftail: %s: %v\n", path, err)
			os.Exit(1)
		}
		defer t.close()
		tailers = append(tailers, t)
	}

	for {
		for _, t := range tailers {
			pfx := ""
			if prefix {
				pfx = t.path + ": "
			}
			if err := t.readNew(pfx); err != nil {
				fmt.Fprintf(os.Stderr, "ftail: %s: %v\n", t.path, err)
			}
		}
		time.Sleep(pollInterval)
	}
}
