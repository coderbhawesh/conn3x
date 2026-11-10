// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

// Package termsafe makes text from a host, a circuit or a playbook safe to
// print on a terminal. Such text can carry escape sequences that repaint
// the screen or hide what follows, and bidirectional controls that reorder
// what the operator reads.
package termsafe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Escape returns s with every control character written out as a visible
// escape such as \x1b: the C0 and C1 controls, DEL, the Unicode
// bidirectional controls and bytes that aren't UTF-8. Newline and tab stay,
// and so does a carriage return right before a newline. Text with none of
// these comes back unchanged.
func Escape(s string) string {
	clean := true
	for i := 0; i < len(s) && clean; {
		r, size := utf8.DecodeRuneInString(s[i:])
		clean = !unsafe(s, i, r, size)
		i += size
	}
	if clean {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case !unsafe(s, i, r, size):
			b.WriteString(s[i : i+size])
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
		i += size
	}
	return b.String()
}

func unsafe(s string, i int, r rune, size int) bool {
	switch {
	case r == utf8.RuneError && size == 1:
		return true
	case r == '\n' || r == '\t':
		return false
	case r == '\r':
		return !strings.HasPrefix(s[i+size:], "\n")
	case r < 0x20 || (r >= 0x7f && r <= 0x9f):
		return true
	case r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
		return true
	}
	return false
}
