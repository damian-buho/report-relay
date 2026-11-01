// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

// The typo body keys: the only ones a typo report keeps.
const (
	fieldSelection    = "selection"
	fieldPrefix       = "prefix"
	fieldSuffix       = "suffix"
	fieldComment      = "comment"
	fieldLang         = "lang"
	fieldTextFragment = "text_fragment"
)

// ErrBadURL is returned when a typo report's page URL is not a usable http(s) URL.
var ErrBadURL = errors.New("report url must be http(s)")

// maxLangBytes bounds the language tag, far above the longest real BCP 47 tag.
const maxLangBytes = 64

// typoFields maps every kept typo key to its byte cap.
var typoFields = map[string]int{
	fieldSelection: maxTextBytes,
	fieldPrefix:    maxTextBytes,
	fieldSuffix:    maxTextBytes,
	fieldComment:   maxTextBytes,
	fieldLang:      maxLangBytes,
}

func init() {
	RegisterTypo(true)
}

// RegisterTypo installs the typo hook, accepting reports when enabled and refusing them with ErrDisabled otherwise.
func RegisterTypo(enabled bool) {
	RegisterBodyHook(typeTypo, func(report *Report) error {
		if !enabled {
			return ErrDisabled
		}
		return typoHook(report)
	})
}

// typoHook closes the body to the typo keys, clips them, and links the typo with a text fragment.
func typoHook(report *Report) error {
	for key, val := range report.Body {
		limit, kept := typoFields[key]
		text, isText := val.(string)
		if !kept || !isText {
			delete(report.Body, key)
			continue
		}
		report.Body[key] = clipUTF8(sanitizeTypoText(text), limit)
	}
	selection, _ := report.Body[fieldSelection].(string)
	if strings.TrimSpace(selection) == "" {
		return errors.New("body is missing \"selection\"")
	}
	// A typo report's page URL becomes a clickable data link in the inbox, so it must be a real http(s) URL; anything else (javascript:, data:, ...) is dropped before it can be stored.
	pageURL, err := url.Parse(report.URL)
	if err != nil || (pageURL.Scheme != "http" && pageURL.Scheme != "https") || pageURL.Host == "" {
		return ErrBadURL
	}
	report.Domain = DomainContent
	report.Body[fieldTextFragment] = textFragment(report.URL, report.Body)
	return nil
}

// textFragment builds `<url>#:~:text=<prefix>-,<selection>,-<suffix>` per the WICG scroll-to-text-fragment draft.
func textFragment(pageURL string, body map[string]any) string {
	directive := fragmentTerm(body[fieldSelection])
	if prefix := fragmentTerm(body[fieldPrefix]); prefix != "" {
		directive = prefix + "-," + directive
	}
	if suffix := fragmentTerm(body[fieldSuffix]); suffix != "" {
		directive += ",-" + suffix
	}
	if strings.Contains(pageURL, "#") {
		return pageURL + ":~:text=" + directive
	}
	return pageURL + "#:~:text=" + directive
}

// TypeTypo is the public name of the typo report type, so callers outside the intake package can recognise it.
const TypeTypo = typeTypo

// Exported aliases of the typo field names, so callers outside the intake package can read them.
const (
	FieldSelection    = fieldSelection
	FieldPrefix       = fieldPrefix
	FieldSuffix       = fieldSuffix
	FieldComment      = fieldComment
	FieldLang         = fieldLang
	FieldTextFragment = fieldTextFragment
)

// TypoURLAllowed reports whether a typo report's page URL is acceptable for the request that carried it: http(s) with a host, and, when an origin is present, a host matching the request Origin; a missing origin is accepted only when the caller does not require one.
func TypoURLAllowed(pageURL, origin string, originRequired bool) bool {
	parsed, err := url.Parse(pageURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if origin == "" {
		return !originRequired
	}
	originURL, err := url.Parse(origin)
	if err != nil || originURL.Host == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), strings.TrimSuffix(originURL.Hostname(), "."))
}

// TypoDedup is an alias for the guard dedup cache, exposed under the intake
// package for callers that look for it there.
type TypoDedup = guard.Dedup

// NewTypoDedup and its clock variant are aliases for the guard constructors.
func NewTypoDedup(size int, ttl time.Duration) *guard.Dedup {
	return guard.NewDedup(size, ttl)
}

func NewTypoDedupWithClock(size int, ttl time.Duration, now func() time.Time) *guard.Dedup {
	return guard.NewDedupWithClock(size, ttl, now)
}

// sanitizeTypoText strips control characters and bidi override code points from a typo field.
func sanitizeTypoText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			continue
		}
		if r >= 0x202a && r <= 0x202e {
			continue
		}
		if r >= 0x2066 && r <= 0x2069 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fragmentTerm percent-encodes one text directive term, including the `-` the directive syntax reserves.
func fragmentTerm(val any) string {
	text, _ := val.(string)
	escaped := url.QueryEscape(strings.TrimSpace(text))
	return strings.NewReplacer("+", "%20", "-", "%2D").Replace(escaped)
}
