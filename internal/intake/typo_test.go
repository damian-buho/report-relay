// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func decodeTypo(t *testing.T, body string) ([]Report, error) {
	t.Helper()
	return Decode(MediaReportingAPI, []byte(`[{"type":"typo","age":0,"url":"https://beta.example.com/post?token=secret","body":`+body+`}]`), testLimits(), false)
}

func TestTypoHook(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantErr  bool
		wantKeys []string
		fragment string
	}{
		{
			"full", `{"selection":"tpyo","prefix":"in ","suffix":" end","comment":"the","lang":"uk"}`, false,
			[]string{fieldSelection, fieldPrefix, fieldSuffix, fieldComment, fieldLang, fieldTextFragment},
			"https://beta.example.com/post#:~:text=in-,tpyo,-end",
		},
		{
			"selection only", `{"selection":"tpyo"}`, false,
			[]string{fieldSelection, fieldTextFragment},
			"https://beta.example.com/post#:~:text=tpyo",
		},
		{
			"foreign keys dropped", `{"selection":"a-b, c&d","evil":"x","prefix":7}`, false,
			[]string{fieldSelection, fieldTextFragment},
			"https://beta.example.com/post#:~:text=a%2Db%2C%20c%26d",
		},
		{"missing selection", `{"prefix":"x"}`, true, nil, ""},
		{"blank selection", `{"selection":"  "}`, true, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reports, err := decodeTypo(t, tc.body)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidReport) {
					t.Fatalf("err = %v, want ErrInvalidReport", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			got := reports[0]
			if got.Domain != DomainContent || got.Severity() != sevInfo {
				t.Errorf("domain/severity = %s/%s, want content/INFO", got.Domain, got.Severity())
			}
			if len(got.Body) != len(tc.wantKeys) {
				t.Errorf("body = %v, want keys %v", got.Body, tc.wantKeys)
			}
			for _, key := range tc.wantKeys {
				if _, ok := got.Body[key]; !ok {
					t.Errorf("body lacks %q: %v", key, got.Body)
				}
			}
			if got.Body[fieldTextFragment] != tc.fragment {
				t.Errorf("text_fragment = %v, want %s", got.Body[fieldTextFragment], tc.fragment)
			}
		})
	}
}

func TestTypoFieldsAreClipped(t *testing.T) {
	long := strings.Repeat("я", maxTextBytes)
	reports, err := decodeTypo(t, `{"selection":"`+long+`","comment":"`+long+`"}`)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for _, key := range []string{fieldSelection, fieldComment} {
		if got := len(reports[0].Body[key].(string)); got > maxTextBytes {
			t.Errorf("%s = %d bytes, want <= %d", key, got, maxTextBytes)
		}
	}
}

func TestTypoKeepsAnExistingFragment(t *testing.T) {
	if got := textFragment("https://beta.example.com/#top", map[string]any{fieldSelection: "x"}); got != "https://beta.example.com/#top:~:text=x" {
		t.Errorf("textFragment = %s", got)
	}
}

func TestDisabledTypoIsRefusedAsDisabled(t *testing.T) {
	RegisterTypo(false)
	t.Cleanup(func() { RegisterTypo(true) })
	if _, err := decodeTypo(t, `{"selection":"tpyo"}`); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

// TestTypoURLAllowed pins the two checks the intake applies to a typo’s page
// URL: it must be a real http(s) resource, and when the request names an origin
// the URL must belong to it.
func TestTypoURLAllowed(t *testing.T) {
	const pageURL = "https://beta.example.com/post"
	const sameOrigin = "https://beta.example.com"
	cases := []struct {
		name           string
		pageURL        string
		origin         string
		originRequired bool
		want           bool
	}{
		{"https page", pageURL, "", false, true},
		{"bare http page", "http://" + pageURL[len("https://"):], "", false, true},
		{"javascript scheme", "javascript:alert(1)", "", false, false},
		{"data scheme", "data:text/html,x", "", false, false},
		{"relative url", "/post", "", false, false},
		{"scheme without a host", "https:///post", "", false, false},
		{"origin absent, not required", pageURL, "", false, true},
		{"origin absent, required", pageURL, "", true, false},
		{"same origin", pageURL, sameOrigin, false, true},
		{"same origin, case and trailing dot", "https://beta.example.com./post", "https://Beta.example.com", false, true},
		{"cross-origin", "https://evil.example/post", sameOrigin, false, false},
		{"unparsable origin", pageURL, "https://[::1", false, false},
		{"origin without a host", pageURL, "not a url", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TypoURLAllowed(tc.pageURL, tc.origin, tc.originRequired); got != tc.want {
				t.Errorf("TypoURLAllowed(%q, %q, %v) = %v, want %v", tc.pageURL, tc.origin, tc.originRequired, got, tc.want)
			}
		})
	}
}

// TestNewTypoDedupAliasesTheGuardCache pins the alias a caller outside the
// guard package uses: same bounded, TTL-based set, same clock seam.
func TestNewTypoDedupAliasesTheGuardCache(t *testing.T) {
	if d := NewTypoDedup(4, time.Minute); d == nil || !d.Allow("k") || d.Allow("k") {
		t.Error("NewTypoDedup did not admit a key exactly once")
	}
	fixed := func() time.Time { return time.Unix(0, 0) }
	withClock := NewTypoDedupWithClock(4, time.Minute, fixed)
	if !withClock.Allow("k") {
		t.Error("NewTypoDedupWithClock refused a new key")
	}
	if withClock.Allow("k") {
		t.Error("a clock that never moves must keep the key inside its TTL")
	}
}

// TestSanitizeTypoTextDropsControlAndBidiRunes pins what a human-written field
// may carry: a control byte or a direction override would corrupt the inbox row.
func TestSanitizeTypoTextDropsControlAndBidiRunes(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"plain text", "tpyo brown fox", "tpyo brown fox"},
		{"bidi override", "tpyo\u202equik", "tpyoquik"},
		{"bidi embedding marks", "\u202a\u202b\u202c\u202d\u202e x", " x"},
		{"directional isolates", "\u2066\u2067\u2068\u2069 y", " y"},
		{"ascii control bytes", "a\u0000b\u001fc", "abc"},
		{"newlines and tabs", "x\ny\tz", "xyz"},
		{"DEL and the C1 range", "a\u007fb\u0080c\u009fd", "abcd"},
		{"multibyte text is untouched", "éè 日本語", "éè 日本語"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeTypoText(tc.text); got != tc.want {
				t.Errorf("sanitizeTypoText(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// TestKeepQueryForNamesTheTypesThatKeepTheirQuery pins the exported form of
// the per-type allow list, including its case folding and space trimming.
func TestKeepQueryForNamesTheTypesThatKeepTheirQuery(t *testing.T) {
	cases := []struct {
		name       string
		reportType string
		global     bool
		types      []string
		want       bool
	}{
		{"global switch wins", typeTypo, true, nil, true},
		{"global switch wins over an empty list", "anything", true, []string{}, true},
		{"listed type", typeTypo, false, []string{typeTypo}, true},
		{"listed type folds case and space", typeTypo, false, []string{" TYPO "}, true},
		{"another listed type", typeTypo, false, []string{typeCSPViolation, typeTypo}, true},
		{"unlisted type", typeTypo, false, []string{typeCSPViolation}, false},
		{"empty list", typeTypo, false, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeepQueryFor(tc.reportType, tc.global, tc.types); got != tc.want {
				t.Errorf("KeepQueryFor(%q, %v, %v) = %v, want %v", tc.reportType, tc.global, tc.types, got, tc.want)
			}
		})
	}
}

// TestTypoHookRefusesAPageURLThatIsNotAResource pins the decoder-side check:
// the hook owns the URL before the queue ever sees the report.
func TestTypoHookRefusesAPageURLThatIsNotAResource(t *testing.T) {
	body := `[{"type":"typo","url":"%s","body":{"selection":"tpyo"}}]`
	for _, pageURL := range []string{"javascript:alert(1)", "data:text/html,x", "/relative", "https://"} {
		raw := strings.Replace(body, "%s", pageURL, 1)
		if _, err := Decode(MediaReportingAPI, []byte(raw), testLimits(), false); !errors.Is(err, ErrBadURL) {
			t.Fatalf("url %q: err = %v, want ErrBadURL", pageURL, err)
		}
	}
}
