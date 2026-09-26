// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Media types the intake accepts. A type is matched on its media type alone,
// with any parameters (a charset, a boundary) ignored.
const (
	MediaReportingAPI = "application/reports+json"
	MediaCSPReport    = "application/csp-report"
	MediaTLSRPTJSON   = "application/tlsrpt+json"
	MediaTLSRPTGzip   = "application/tlsrpt+gzip"
)

// ErrUnsupportedType is returned for a content type no enabled intake claims.
var ErrUnsupportedType = errors.New("unsupported content type")

// ErrDisabled is returned for a content type whose intake is switched off.
var ErrDisabled = errors.New("intake disabled")

// ErrNoReports is returned when a body decoded to nothing usable.
var ErrNoReports = errors.New("body carried no report")

// ErrInvalidReport is returned when a report decoded but failed its schema, so
// the sender is at fault rather than the transport.
var ErrInvalidReport = errors.New("report failed validation")

// reportEnvelope is the Reporting API wire shape: an array of reports, each
// with a type, an age in milliseconds, a URL and a free-form body.
//
// The body is deliberately NOT typed. The Reporting API is an open list: a new
// report type (a CT report, a future network-error variant) arrives as the same
// envelope with a different type string, and it must survive a build that has
// never heard of it. Per-type validation therefore lives in the bodyHook
// registry below, and a type with no hook is validated only on the envelope.
type reportEnvelope struct {
	Type string          `json:"type"`
	Age  int64           `json:"age"`
	URL  string          `json:"url"`
	Body json.RawMessage `json:"body"`
}

// bodyHook validates and annotates the body of one report type. It returns an
// error to reject the report, or nil to accept it. Registering a hook for a new
// type is the whole cost of supporting it properly.
type bodyHook func(body map[string]any) error

// bodyHooks holds the per-type validation, keyed by report type. It is an open
// map: a type with no entry is accepted on the envelope alone, because the spec
// requires an endpoint to accept report types it does not recognise.
var bodyHooks = map[string]bodyHook{}

// RegisterBodyHook adds validation for one report type. It is called from a
// package init or from main before the server starts; it is not safe to call
// once traffic is being served.
func RegisterBodyHook(reportType string, hook bodyHook) {
	bodyHooks[reportType] = hook
}

// requiredBody lists the body keys a report type must carry to be accepted.
var requiredBody = map[string][]string{
	typeCSPViolation:  {fieldDocumentURL, fieldEffectiveDirective, fieldBlockedURL},
	typeCOEP:          {fieldDocumentURL},
	typeCOEPViolation: {fieldDocumentURL},
	typeNetworkError:  {fieldDocumentURL, "phase"},
	typeDeprecation:   {fieldDocumentURL},
	typePermissions:   {fieldDocumentURL},
}

func init() {
	for reportType, required := range requiredBody {
		keys := required
		RegisterBodyHook(reportType, func(body map[string]any) error {
			for _, key := range keys {
				if _, ok := body[key]; !ok {
					return fmt.Errorf("body is missing %q", key)
				}
			}
			return nil
		})
	}
}

// typeUnknown is the bucket for a type that fails the label charset. The raw
// value rides in the log body, never in a label, so cardinality stays bounded.
const typeUnknown = "unknown"

// maxTypeLen bounds a type label. Past it the type buckets as unknown.
const maxTypeLen = 64

// normalizeType folds the type strings the Reporting API has accumulated onto
// one vocabulary, so a query never has to know which spelling a browser used.
func normalizeType(reportType string) string {
	switch strings.ToLower(reportType) {
	case typeCSPViolation, "cspviolationreportbody":
		return typeCSPViolation
	case typeCOEP, typeCOEPViolation, "coepviolationreportbody":
		return typeCOEP
	case typeNetworkError, "nel", "networkerror":
		return typeNetworkError
	case typeDeprecation:
		return typeDeprecation
	case typePermissions:
		return typePermissions
	default:
		return SanitizeType(reportType)
	}
}

// SanitizeType bounds an open-vocabulary type for label use. Known spellings
// are folded by normalizeType first; anything else must match a lowercase
// charset after folding, or it buckets as unknown. The intake is an open list,
// so an unseen-but-well-formed type still arrives under its own name.
func SanitizeType(reportType string) string {
	lowered := strings.ToLower(reportType)
	if len(lowered) == 0 || len(lowered) > maxTypeLen || !validTypeChars(lowered) {
		return typeUnknown
	}
	return lowered
}

// validTypeChars reports whether every byte of s is a label-safe character.
func validTypeChars(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return true
}
