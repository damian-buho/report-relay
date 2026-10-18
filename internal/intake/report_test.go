// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"strings"
	"testing"
)

// TestRedactURLFallsBackWhenTheValueWillNotParse pins the fallback arm: net/url
// refuses some values a report can carry, and a body query string is stripped
// anyway rather than left whole.
func TestRedactURLFallsBackWhenTheValueWillNotParse(t *testing.T) {
	withQuery := "http://exa\x7fmple.com/path?token=secret#frag"
	if got := redactURL(withQuery); got != "http://exa\x7fmple.com/path" {
		t.Errorf("redactURL = %q, want the query and fragment cut by hand", got)
	}
	bare := "http://[::1"
	if got := redactURL(bare); got != bare {
		t.Errorf("redactURL = %q, want an unparseable value without a query kept whole", got)
	}
}

// The nesting fixtures: a body key that wraps the next level, and the two
// query-bearing URLs the depth caps decide whether the walk ever reaches.
const (
	nestedKey  = "nested"
	secretURL  = "https://evil.example/x.js?token=secret"
	secretPage = "https://beta.example.com/a?token=secret"
)

// nestedBody builds the object chain blockedKey sits at the bottom of, wrapped
// in the levels a depth cap stops the walk at.
func nestedBody(depth int, leafKey, leaf string) map[string]any {
	body := map[string]any{leafKey: leaf}
	for range depth {
		body = map[string]any{nestedKey: body}
	}
	return body
}

// leafAt reads the value the chain ended in, which is redacted or not.
func leafAt(body map[string]any, levels int, leafKey string) string {
	for range levels {
		body = body[nestedKey].(map[string]any)
	}
	return body[leafKey].(string)
}

// TestRedactBodyStopsDescendingAtTheDepthCap pins the cap on nested objects: a
// URL past it keeps its query, because the walk does not reach it, while the
// same body one level of budget deeper is redacted.
func TestRedactBodyStopsDescendingAtTheDepthCap(t *testing.T) {
	atCap := nestedBody(2, fieldBlockedURL, secretURL)
	redactBody(atCap, false, 1)
	if got := leafAt(atCap, 2, fieldBlockedURL); got != secretURL {
		t.Errorf("blockedURL = %v, want it untouched past the depth cap", got)
	}
	withinCap := nestedBody(2, fieldBlockedURL, secretURL)
	redactBody(withinCap, false, 2)
	if got := leafAt(withinCap, 2, fieldBlockedURL); got != testBlockedURL {
		t.Errorf("blockedURL = %v, want the query dropped within the cap", got)
	}
}

// TestRedactBodyStopsDescendingIntoAnArrayAtTheDepthCap is the same cap on the
// array arm: the cap is checked before the first item is walked, and an item
// counts its own depth from the array it sits in.
func TestRedactBodyStopsDescendingIntoAnArrayAtTheDepthCap(t *testing.T) {
	build := func() map[string]any {
		return map[string]any{"list": []any{nestedBody(1, fieldDocumentURL, secretPage)}}
	}
	inner := func(body map[string]any) string {
		return leafAt(body["list"].([]any)[0].(map[string]any), 1, fieldDocumentURL)
	}
	for _, maxDepth := range []int{1, 2} {
		body := build()
		redactBody(body, false, maxDepth)
		if got := inner(body); !strings.Contains(got, "token=secret") {
			t.Errorf("documentURL = %v at depth cap %d, want it untouched past the cap", got, maxDepth)
		}
	}
	within := build()
	redactBody(within, false, 3)
	if got := inner(within); got != "https://beta.example.com/a" {
		t.Errorf("documentURL = %v, want the query dropped within the cap", got)
	}
}
