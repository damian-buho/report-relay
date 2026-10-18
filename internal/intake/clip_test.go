// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"strings"
	"testing"
)

// TestClipUTF8KeepsAStringAtTheLimit pins the byte budget: a value of exactly
// the limit is whole, so it rides through untouched.
func TestClipUTF8KeepsAStringAtTheLimit(t *testing.T) {
	if got := clipUTF8("abc", 3); got != "abc" {
		t.Errorf("clipUTF8 = %q, want the string at its limit unchanged", got)
	}
}

// TestClipUTF8KeepsAnInvalidByteAtTheLimit is the same edge with a body that is
// not valid UTF-8: at the limit nothing is cut, so nothing is trimmed either.
func TestClipUTF8KeepsAnInvalidByteAtTheLimit(t *testing.T) {
	if got := clipUTF8("\xff\xfe", 2); got != "\xff\xfe" {
		t.Errorf("clipUTF8 = %q, want a body at its limit untouched", got)
	}
}

// TestClipUTF8TrimsALoneInvalidByte pins the trim loop's last turn: the cut
// leaves a single invalid byte, which is dropped rather than exported.
func TestClipUTF8TrimsALoneInvalidByte(t *testing.T) {
	if got := clipUTF8("\xffx", 1); got != "" {
		t.Errorf("clipUTF8 = %q, want the lone invalid byte dropped", got)
	}
}

// TestSanitizeTypeAcceptsTheCharsetEdges pins the label charset at every
// character class it admits: the last letter of the range, the first and last
// digit, and the dash, each in one type of exactly the label cap.
func TestSanitizeTypeAcceptsTheCharsetEdges(t *testing.T) {
	raw := strings.Repeat("z", maxTypeLen-3) + "0-9"
	if len(raw) != maxTypeLen {
		t.Fatalf("the fixture is %d bytes, want exactly %d", len(raw), maxTypeLen)
	}
	if got := SanitizeType(raw); got != raw {
		t.Errorf("SanitizeType = %q, want the %d-character type kept", got, maxTypeLen)
	}
	if got := SanitizeType(strings.ToUpper(raw)); got != raw {
		t.Errorf("SanitizeType = %q, want the folded form kept", got)
	}
}
