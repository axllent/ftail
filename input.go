package main

// insertRunes inserts r into s at rune position pos, returning the new string
// and updated cursor position.
func insertRunes(s string, pos int, r []rune) (string, int) {
	runes := []rune(s)
	result := make([]rune, len(runes)+len(r))
	copy(result, runes[:pos])
	copy(result[pos:], r)
	copy(result[pos+len(r):], runes[pos:])
	return string(result), pos + len(r)
}

// deleteRune removes the rune to the left of pos (backspace behaviour),
// returning the new string and updated cursor position.
func deleteRune(s string, pos int) (string, int) {
	if pos == 0 {
		return s, 0
	}
	runes := []rune(s)
	return string(append(runes[:pos-1], runes[pos:]...)), pos - 1
}

// deleteRuneForward removes the rune at pos (delete-key behaviour).
func deleteRuneForward(s string, pos int) string {
	runes := []rune(s)
	if pos >= len(runes) {
		return s
	}
	return string(append(runes[:pos], runes[pos+1:]...))
}

// prevWordStart moves the cursor to the start of the previous word.
// Words are separated by spaces.
func prevWordStart(s string, pos int) int {
	if pos == 0 {
		return 0
	}
	runes := []rune(s)
	pos = min(pos, len(runes))
	
	// Skip any spaces to the left
	for pos > 0 && runes[pos-1] == ' ' {
		pos--
	}
	
	// Skip to the start of the current/previous word
	for pos > 0 && runes[pos-1] != ' ' {
		pos--
	}
	
	return pos
}

// nextWordStart moves the cursor to the start of the next word.
// Words are separated by spaces.
func nextWordStart(s string, pos int) int {
	runes := []rune(s)
	if pos >= len(runes) {
		return len(runes)
	}
	
	// Skip to the end of the current word
	for pos < len(runes) && runes[pos] != ' ' {
		pos++
	}
	
	// Skip any spaces
	for pos < len(runes) && runes[pos] == ' ' {
		pos++
	}
	
	return pos
}
