package main

import "testing"

func TestPrevWordStart(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		pos      int
		expected int
	}{
		{
			name:     "start of string",
			text:     "hello world",
			pos:      0,
			expected: 0,
		},
		{
			name:     "middle of first word",
			text:     "hello world",
			pos:      3,
			expected: 0,
		},
		{
			name:     "start of second word",
			text:     "hello world",
			pos:      6,
			expected: 0,
		},
		{
			name:     "middle of second word",
			text:     "hello world",
			pos:      9,
			expected: 6,
		},
		{
			name:     "end of string",
			text:     "hello world",
			pos:      11,
			expected: 6,
		},
		{
			name:     "multiple spaces",
			text:     "hello   world",
			pos:      11,
			expected: 8,
		},
		{
			name:     "in spaces between words",
			text:     "hello   world",
			pos:      7,
			expected: 0,
		},
		{
			name:     "three words",
			text:     "error not found",
			pos:      15,
			expected: 10,
		},
		{
			name:     "empty string",
			text:     "",
			pos:      0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prevWordStart(tt.text, tt.pos)
			if result != tt.expected {
				t.Errorf("prevWordStart(%q, %d) = %d, want %d", tt.text, tt.pos, result, tt.expected)
			}
		})
	}
}

func TestNextWordStart(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		pos      int
		expected int
	}{
		{
			name:     "start of first word",
			text:     "hello world",
			pos:      0,
			expected: 6,
		},
		{
			name:     "middle of first word",
			text:     "hello world",
			pos:      3,
			expected: 6,
		},
		{
			name:     "start of second word",
			text:     "hello world",
			pos:      6,
			expected: 11,
		},
		{
			name:     "middle of second word",
			text:     "hello world",
			pos:      9,
			expected: 11,
		},
		{
			name:     "end of string",
			text:     "hello world",
			pos:      11,
			expected: 11,
		},
		{
			name:     "multiple spaces",
			text:     "hello   world",
			pos:      0,
			expected: 8,
		},
		{
			name:     "in spaces between words",
			text:     "hello   world",
			pos:      5,
			expected: 8,
		},
		{
			name:     "three words",
			text:     "error not found",
			pos:      0,
			expected: 6,
		},
		{
			name:     "three words from middle",
			text:     "error not found",
			pos:      6,
			expected: 10,
		},
		{
			name:     "empty string",
			text:     "",
			pos:      0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextWordStart(tt.text, tt.pos)
			if result != tt.expected {
				t.Errorf("nextWordStart(%q, %d) = %d, want %d", tt.text, tt.pos, result, tt.expected)
			}
		})
	}
}
