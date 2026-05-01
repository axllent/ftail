package main

import (
	"reflect"
	"regexp"
	"testing"
)

func TestParseTokens(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected []token
	}{
		{
			name:     "empty string",
			pattern:  "",
			expected: nil,
		},
		{
			name:     "single term",
			pattern:  "foo",
			expected: []token{{exclude: false, term: "foo"}},
		},
		{
			name:     "multiple terms",
			pattern:  "foo bar baz",
			expected: []token{{false, "foo"}, {false, "bar"}, {false, "baz"}},
		},
		{
			name:     "quoted phrase",
			pattern:  `"foo bar"`,
			expected: []token{{false, "foo bar"}},
		},
		{
			name:     "quoted phrase with other terms",
			pattern:  `"foo bar" baz`,
			expected: []token{{false, "foo bar"}, {false, "baz"}},
		},
		{
			name:     "exclusion with dash",
			pattern:  "-foo",
			expected: []token{{true, "foo"}},
		},
		{
			name:     "exclusion with exclamation",
			pattern:  "!foo",
			expected: []token{{true, "foo"}},
		},
		{
			name:     "mixed inclusion and exclusion",
			pattern:  "foo -bar !baz",
			expected: []token{{false, "foo"}, {true, "bar"}, {true, "baz"}},
		},
		{
			name:     "quoted exclusion",
			pattern:  `-"foo bar"`,
			expected: []token{{true, "foo bar"}},
		},
		{
			name:     "unclosed quote",
			pattern:  `"foo bar`,
			expected: []token{{false, "foo bar"}},
		},
		{
			name:     "empty quotes",
			pattern:  `""`,
			expected: nil,
		},
		{
			name:     "multiple spaces",
			pattern:  "foo    bar",
			expected: []token{{false, "foo"}, {false, "bar"}},
		},
		{
			name:     "leading and trailing spaces",
			pattern:  "  foo bar  ",
			expected: []token{{false, "foo"}, {false, "bar"}},
		},
		{
			name:     "exclusion only dash",
			pattern:  "-",
			expected: nil,
		},
		{
			name:     "complex query",
			pattern:  `error -"not found" warning !debug`,
			expected: []token{{false, "error"}, {true, "not found"}, {false, "warning"}, {true, "debug"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTokens(tt.pattern)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseTokens(%q) = %v, want %v", tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestMatchTokens(t *testing.T) {
	tests := []struct {
		name    string
		tokens  []token
		text    string
		matches bool
	}{
		{
			name:    "empty tokens match all",
			tokens:  nil,
			text:    "anything",
			matches: true,
		},
		{
			name:    "single inclusion match",
			tokens:  []token{{false, "error"}},
			text:    "Error: file not found",
			matches: true,
		},
		{
			name:    "single inclusion no match",
			tokens:  []token{{false, "error"}},
			text:    "Warning: low memory",
			matches: false,
		},
		{
			name:    "case insensitive match",
			tokens:  []token{{false, "ERROR"}},
			text:    "error occurred",
			matches: true,
		},
		{
			name:    "multiple inclusions all match",
			tokens:  []token{{false, "error"}, {false, "file"}},
			text:    "Error: file not found",
			matches: true,
		},
		{
			name:    "multiple inclusions partial match",
			tokens:  []token{{false, "error"}, {false, "network"}},
			text:    "Error: file not found",
			matches: false,
		},
		{
			name:    "exclusion match excludes",
			tokens:  []token{{true, "debug"}},
			text:    "Debug: starting process",
			matches: false,
		},
		{
			name:    "exclusion no match includes",
			tokens:  []token{{true, "debug"}},
			text:    "Info: process started",
			matches: true,
		},
		{
			name:    "mixed inclusion and exclusion",
			tokens:  []token{{false, "error"}, {true, "network"}},
			text:    "Error: file not found",
			matches: true,
		},
		{
			name:    "mixed - inclusion matches but exclusion also matches",
			tokens:  []token{{false, "error"}, {true, "network"}},
			text:    "Error: network timeout",
			matches: false,
		},
		{
			name:    "phrase match",
			tokens:  []token{{false, "not found"}},
			text:    "Error: file not found",
			matches: true,
		},
		{
			name:    "phrase no match - words separated",
			tokens:  []token{{false, "file error"}},
			text:    "Error: file not found",
			matches: false,
		},
		{
			name:    "multiple exclusions",
			tokens:  []token{{true, "debug"}, {true, "trace"}},
			text:    "Info: operation completed",
			matches: true,
		},
		{
			name:    "multiple exclusions - one matches",
			tokens:  []token{{true, "debug"}, {true, "trace"}},
			text:    "Debug: checking status",
			matches: false,
		},
		{
			name:    "substring match",
			tokens:  []token{{false, "err"}},
			text:    "Error occurred",
			matches: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchTokens(tt.tokens, tt.text)
			if result != tt.matches {
				t.Errorf("matchTokens(%v, %q) = %v, want %v", tt.tokens, tt.text, result, tt.matches)
			}
		})
	}
}

func TestTokensNarrow(t *testing.T) {
	tests := []struct {
		name      string
		oldTokens []token
		newTokens []token
		expected  bool
	}{
		{
			name:      "prefix extension",
			oldTokens: []token{{false, "err"}},
			newTokens: []token{{false, "error"}},
			expected:  true,
		},
		{
			name:      "adding more terms",
			oldTokens: []token{{false, "error"}},
			newTokens: []token{{false, "error"}, {false, "file"}},
			expected:  true,
		},
		{
			name:      "prefix extension with multiple terms",
			oldTokens: []token{{false, "err"}, {false, "fil"}},
			newTokens: []token{{false, "error"}, {false, "file"}},
			expected:  true,
		},
		{
			name:      "identical tokens",
			oldTokens: []token{{false, "error"}},
			newTokens: []token{{false, "error"}},
			expected:  false,
		},
		{
			name:      "fewer new tokens",
			oldTokens: []token{{false, "error"}, {false, "file"}},
			newTokens: []token{{false, "error"}},
			expected:  false,
		},
		{
			name:      "different terms",
			oldTokens: []token{{false, "error"}},
			newTokens: []token{{false, "warning"}},
			expected:  false,
		},
		{
			name:      "old has exclusion",
			oldTokens: []token{{true, "debug"}},
			newTokens: []token{{true, "debug"}, {false, "error"}},
			expected:  false,
		},
		{
			name:      "new has exclusion",
			oldTokens: []token{{false, "error"}},
			newTokens: []token{{false, "error"}, {true, "debug"}},
			expected:  false,
		},
		{
			name:      "empty to non-empty",
			oldTokens: nil,
			newTokens: []token{{false, "error"}},
			expected:  true, // going from no filter to a filter does narrow results
		},
		{
			name:      "non-prefix extension",
			oldTokens: []token{{false, "error"}},
			newTokens: []token{{false, "warning"}},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokensNarrow(tt.oldTokens, tt.newTokens)
			if result != tt.expected {
				t.Errorf("tokensNarrow(%v, %v) = %v, want %v", tt.oldTokens, tt.newTokens, result, tt.expected)
			}
		})
	}
}

func TestHighlightRegex(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		line     string
		hasMatch bool
	}{
		{
			name:     "simple match",
			pattern:  `error`,
			line:     "Error: file not found",
			hasMatch: false, // case-sensitive by default
		},
		{
			name:     "case insensitive match",
			pattern:  `(?i)error`,
			line:     "Error: file not found",
			hasMatch: true,
		},
		{
			name:     "multiple matches",
			pattern:  `\d+`,
			line:     "Port 8080 connected to 192.168.1.1",
			hasMatch: true,
		},
		{
			name:     "no matches",
			pattern:  `warning`,
			line:     "Error: file not found",
			hasMatch: false,
		},
		{
			name:     "word boundary",
			pattern:  `\berror\b`,
			line:     "error occurred, no errors found",
			hasMatch: true,
		},
		{
			name:     "anchored pattern",
			pattern:  `^Error`,
			line:     "Error: file not found",
			hasMatch: true,
		},
		{
			name:     "anchored pattern no match",
			pattern:  `^Error`,
			line:     "  Error: file not found",
			hasMatch: false,
		},
		{
			name:     "complex pattern",
			pattern:  `(?i)(error|warning|critical):\s+\w+`,
			line:     "Error: file not found",
			hasMatch: true,
		},
		{
			name:     "empty line",
			pattern:  `.*`,
			line:     "",
			hasMatch: true, // .* matches empty string
		},
		{
			name:     "IP address pattern",
			pattern:  `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`,
			line:     "Connected from 192.168.1.100",
			hasMatch: true,
		},
		{
			name:     "email pattern",
			pattern:  `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
			line:     "Contact us at support@example.com for help",
			hasMatch: true,
		},
		{
			name:     "alternation",
			pattern:  `(GET|POST|PUT|DELETE)`,
			line:     "POST /api/users HTTP/1.1",
			hasMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := regexp.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Failed to compile regex %q: %v", tt.pattern, err)
			}

			// Test that highlightRegex doesn't error and returns a string
			result := highlightRegex(re, tt.line)
			if result == "" && tt.line != "" {
				t.Errorf("highlightRegex(%q, %q) returned empty string", tt.pattern, tt.line)
			}

			// Verify the regex actually matches/doesn't match as expected
			hasActualMatch := re.MatchString(tt.line)
			if hasActualMatch != tt.hasMatch {
				t.Errorf("regex %q match against %q = %v, want %v",
					tt.pattern, tt.line, hasActualMatch, tt.hasMatch)
			}
		})
	}
}

func TestRegexCompilation(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		valid   bool
	}{
		{
			name:    "valid simple pattern",
			pattern: `error`,
			valid:   true,
		},
		{
			name:    "valid case insensitive",
			pattern: `(?i)warning`,
			valid:   true,
		},
		{
			name:    "valid complex pattern",
			pattern: `\d{4}-\d{2}-\d{2}`,
			valid:   true,
		},
		{
			name:    "invalid unclosed bracket",
			pattern: `[abc`,
			valid:   false,
		},
		{
			name:    "invalid unclosed paren",
			pattern: `(abc`,
			valid:   false,
		},
		{
			name:    "invalid repetition",
			pattern: `*abc`,
			valid:   false,
		},
		{
			name:    "invalid escape",
			pattern: `\k`,
			valid:   false,
		},
		{
			name:    "empty pattern",
			pattern: ``,
			valid:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := regexp.Compile(tt.pattern)
			isValid := err == nil
			if isValid != tt.valid {
				t.Errorf("regexp.Compile(%q) valid = %v, want %v (error: %v)",
					tt.pattern, isValid, tt.valid, err)
			}
		})
	}
}
