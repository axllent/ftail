package main

import (
	"bufio"
	"io"
	"os"
	"time"
)

const pollInterval = 200 * time.Millisecond

// tailer tracks the read position within a file.
type tailer struct {
	path   string
	file   *os.File
	offset int64
}

// entry holds a single tailed line and its source file.
type entry struct {
	file     string
	text     string
	received time.Time
	matched  bool // true when this entry is present in model.filtered
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

	var initial []string
	var offset int64
	if n == 0 {
		initial, offset, err = readLinesFrom(f, 0)
	} else {
		initial, offset, err = lastNLines(f, n)
	}
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

	// Estimate capacity: assume ~80 bytes per log line on average.
	newBytes := info.Size() - t.offset
	lines := make([]string, 0, max(int(newBytes/80), 4))
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
