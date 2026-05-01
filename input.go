package main

// insertRunes inserts r into s at rune position pos, returning the new string
// and updated cursor position.
func insertRunes(s string, pos int, r []rune) (string, int) {
	runes := []rune(s)
	runes = append(runes[:pos], append(r, runes[pos:]...)...)
	return string(runes), pos + len(r)
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
