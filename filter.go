package main

import (
	"regexp"
	"sort"
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

// tokensNarrow reports whether newTokens is strictly more restrictive than
// oldTokens — i.e., every entry matching newTokens also matches oldTokens.
// Only reliable when all tokens are inclusion tokens; returns false otherwise.
func tokensNarrow(old, new []token) bool {
	for _, t := range old {
		if t.exclude {
			return false
		}
	}
	for _, t := range new {
		if t.exclude {
			return false
		}
	}
	if len(new) < len(old) {
		return false
	}
	extended := len(new) > len(old)
	for i, ot := range old {
		if !strings.HasPrefix(new[i].term, ot.term) {
			return false
		}
		if len(new[i].term) > len(ot.term) {
			extended = true
		}
	}
	return extended
}

// matchTokens reports whether s satisfies all tokens:
// - inclusion terms must appear as substrings (case-insensitive)
// - exclusion terms (- or ! prefix) must NOT appear
func matchTokens(tokens []token, s string) bool {
	if len(tokens) == 0 {
		return true
	}
	s = strings.ToLower(s)
	for _, t := range tokens {
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

// highlightTokens returns line with each occurrence of every inclusion token
// rendered in the match colour; unmatched text is left unstyled.
// Spans are byte-based (matching highlightRegex), which is correct for
// ASCII-dominant log lines; non-ASCII edge cases are handled by strings.Index.
func highlightTokens(tokens []token, line string) string {
	if len(tokens) == 0 {
		return line
	}
	lineLower := strings.ToLower(line)

	type span struct{ start, end int }
	var spans []span
	for _, tok := range tokens {
		if tok.exclude {
			continue
		}
		term := strings.ToLower(tok.term)
		if term == "" {
			continue
		}
		pos := 0
		for {
			idx := strings.Index(lineLower[pos:], term)
			if idx < 0 {
				break
			}
			start := pos + idx
			end := start + len(term)
			spans = append(spans, span{start, end})
			pos = end
		}
	}
	if len(spans) == 0 {
		return line
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	// merge overlapping/adjacent spans
	merged := spans[:1]
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
		} else {
			merged = append(merged, s)
		}
	}

	var sb strings.Builder
	prev := 0
	for _, s := range merged {
		sb.WriteString(line[prev:s.start])
		sb.WriteString(matchStyle.Render(line[s.start:s.end]))
		prev = s.end
	}
	sb.WriteString(line[prev:])
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
