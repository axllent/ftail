package main

import (
	"regexp"
	"strings"
)

type token struct {
	exclude bool
	term    string
}

// parseTokens splits a query into tokens. Whitespace separates tokens.
// A leading - or ! marks a token as an exclusion.
// Quoted strings (using ") are treated as a single term, preserving spaces.
// An unclosed quote is treated as if closed at end of input.
func parseTokens(pattern string) []token {
	var tokens []token
	runes := []rune(pattern)
	i := 0
	for i < len(runes) {
		for i < len(runes) && runes[i] == ' ' {
			i++
		}
		if i >= len(runes) {
			break
		}
		exclude := false
		if runes[i] == '-' || runes[i] == '!' {
			exclude = true
			i++
		}
		if i >= len(runes) {
			break
		}
		var term string
		if runes[i] == '"' {
			i++ // skip opening quote
			start := i
			for i < len(runes) && runes[i] != '"' {
				i++
			}
			term = string(runes[start:i])
			if i < len(runes) {
				i++ // skip closing quote
			}
		} else {
			start := i
			for i < len(runes) && runes[i] != ' ' {
				i++
			}
			term = string(runes[start:i])
		}
		if term != "" {
			tokens = append(tokens, token{exclude, term})
		}
	}
	return tokens
}

// match reports whether s satisfies the pattern:
// - inclusion terms must appear as substrings (case-insensitive)
// - exclusion terms (- or ! prefix) must NOT appear
func match(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	s = strings.ToLower(s)
	for _, t := range parseTokens(pattern) {
		contains := strings.Contains(s, strings.ToLower(t.term))
		if t.exclude && contains {
			return false
		}
		if !t.exclude && !contains {
			return false
		}
	}
	return true
}

// highlight returns line with each occurrence of every inclusion term in
// pattern rendered in the match colour; unmatched text is left unstyled.
func highlight(pattern, line string) string {
	if pattern == "" {
		return line
	}
	lineRunes := []rune(line)
	lineLower := []rune(strings.ToLower(line))
	marked := make([]bool, len(lineRunes))

	for _, tok := range parseTokens(pattern) {
		if tok.exclude {
			continue
		}
		w := []rune(strings.ToLower(tok.term))
		for i := range len(lineLower) - len(w) + 1 {
			ok := true
			for j, r := range w {
				if lineLower[i+j] != r {
					ok = false
					break
				}
			}
			if ok {
				for j := range w {
					marked[i+j] = true
				}
			}
		}
	}

	var sb strings.Builder
	inMatch := false
	segStart := 0
	for i := range lineRunes {
		if marked[i] != inMatch {
			seg := string(lineRunes[segStart:i])
			if inMatch {
				sb.WriteString(matchStyle.Render(seg))
			} else {
				sb.WriteString(seg)
			}
			inMatch = marked[i]
			segStart = i
		}
	}
	seg := string(lineRunes[segStart:])
	if inMatch {
		sb.WriteString(matchStyle.Render(seg))
	} else {
		sb.WriteString(seg)
	}
	return sb.String()
}

// highlightRegex highlights all substrings of line matched by re.
func highlightRegex(re *regexp.Regexp, line string) string {
	indices := re.FindAllStringIndex(line, -1)
	if len(indices) == 0 {
		return line
	}
	var sb strings.Builder
	prev := 0
	for _, idx := range indices {
		sb.WriteString(line[prev:idx[0]])
		sb.WriteString(matchStyle.Render(line[idx[0]:idx[1]]))
		prev = idx[1]
	}
	sb.WriteString(line[prev:])
	return sb.String()
}
