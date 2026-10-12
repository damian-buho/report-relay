// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"errors"
	"net/url"
	"strings"
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
		report.Body[key] = clipUTF8(text, limit)
	}
	selection, _ := report.Body[fieldSelection].(string)
	if strings.TrimSpace(selection) == "" {
		return errors.New("body is missing \"selection\"")
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

// fragmentTerm percent-encodes one text directive term, including the `-` the directive syntax reserves.
func fragmentTerm(val any) string {
	text, _ := val.(string)
	escaped := url.QueryEscape(strings.TrimSpace(text))
	return strings.NewReplacer("+", "%20", "-", "%2D").Replace(escaped)
}
