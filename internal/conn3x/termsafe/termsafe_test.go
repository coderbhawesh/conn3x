// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package termsafe

import "testing"

func TestEscape(t *testing.T) {
	for in, want := range map[string]string{
		"plain text, naïve ✓\n\tindented\n": "plain text, naïve ✓\n\tindented\n",
		"dos line\r\nnext":                  "dos line\r\nnext",
		"over\rwrite":                       `over\rwrite`,
		"\x1b[2J\x1b[Hhidden":               `\x1b[2J\x1b[Hhidden`,
		"bell\x07 del\x7f nul\x00":          `bell\x07 del\x7f nul\x00`,
		"c1 \u009b31m":                      `c1 \x9b31m`,
		"rtl \u202egnp.exe \u2066x\u2069":   `rtl \u202egnp.exe \u2066x\u2069`,
		"bad \xff byte":                     `bad \xff byte`,
		"":                                  "",
	} {
		if got := Escape(in); got != want {
			t.Errorf("Escape(%q) = %q, want %q", in, got, want)
		}
	}
}
