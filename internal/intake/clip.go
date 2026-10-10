// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import "unicode/utf8"

// maxTextBytes bounds a free-text field a report carries, such as an IODEF description or a node name.
const maxTextBytes = 1024

// maxTypeBytes bounds the raw report type kept in the body: four times the label cap, because the raw value is what did not fit the label.
const maxTypeBytes = 4 * maxTypeLen

// clipUTF8 shortens s to limit bytes on a rune boundary: a byte-count cut can land mid-rune, and OTLP strings must be valid UTF-8, so one stray byte can cost the whole export.
func clipUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for len(s) > 0 {
		// A lone lead byte and a bare continuation byte both decode to one invalid rune, so this trims either half and keeps whole runes.
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size != 1 {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}
