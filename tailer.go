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

// readLinesFrom reads from f starting at offset and returns each newline-
// terminated line (without the terminator). Any bytes after the last newline
// are an incomplete trailing line — they are not emitted, and newOffset stops
// just past the last newline so the next call re-reads the partial line whole
// once the writer finishes it.
func readLinesFrom(f *os.File, offset int64) ([]string, int64, error) {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	r := bufio.NewReader(f)
	var (
		lines    []string
		consumed int64
	)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// io.EOF (or other) before a terminator — discard the partial
			// tail and leave the offset just past the last complete line.
			if err == io.EOF {
				err = nil
			}
			return lines, offset + consumed, err
		}
		consumed += int64(len(line))
		// Trim the trailing \n (and an optional preceding \r), matching the
		// previous bufio.Scanner behaviour.
		n := len(line) - 1
		if n > 0 && line[n-1] == '\r' {
			n--
		}
		lines = append(lines, string(line[:n]))
	}
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
// Incomplete trailing lines (no newline yet) are not emitted; they will be
// read on a subsequent call once the writer terminates them.
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

	lines, newOffset, err := readLinesFrom(t.file, t.offset)
	t.offset = newOffset
	return lines, err
}

func (t *tailer) close() {
	t.file.Close()
}
