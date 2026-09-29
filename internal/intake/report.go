// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Package intake decodes the report wire formats browsers and mail servers
// send into one Report shape, so the telemetry layer never sees a wire format.
package intake

import (
	"net/url"
	"strings"
)

// Report is one report, whatever wire format carried it. A new report type
// (a CT report, a future Reporting API type) is a new decoder that fills this
// same struct; nothing downstream changes.
type Report struct {
	// Type is the report type, folded onto one vocabulary and bounded for
	// label use. A sender spelling the decoder recognises maps to the known
	// name; anything else arrives lowercased, or as unknown with the raw
	// value kept in the body.
	Type string
	// Domain is the reporting surface: browser or mail.
	Domain string
	// Source is the intake format that carried it: reporting-api, csp or tlsrpt.
	Source string
	// URL is the document or endpoint the report is about.
	URL string
	// Age is the Reporting API age field in milliseconds, 0 when absent.
	Age int64
	// Body holds the report fields, already redacted.
	Body map[string]any
}

// severity maps a report type to a log severity. An unknown type is INFO:
// receiving a report type this build does not know is not an error.
func severity(reportType string) string {
	switch reportType {
	case typeCSPViolation, typeCOEP, typeCOEPViolation, typeNetworkError,
		typeCOOP, typeCrash, typeIntegrityViolation, typeDocumentPolicy,
		typePermissions, typeFeaturePolicy, typeExpectCT, typeHPKP,
		typeIodef, "attribution-reporting":
		return "WARN"
	default:
		return "INFO"
	}
}

// Severity returns the log severity for this report.
func (r Report) Severity() string {
	if r.Domain == DomainMail || r.Domain == DomainCert {
		return "WARN"
	}
	return severity(r.Type)
}

// The intake source names, as they appear in the report.source attribute.
const (
	SourceReportingAPI = "reporting-api"
	SourceCSP          = "csp"
	SourceTLSRPT       = "tlsrpt"
	SourceExpectCT     = "expect-ct"
	SourceHPKP         = "hpkp"
	SourceIODEF        = "iodef"
)

// The reporting domains, as they appear in the event.domain attribute.
const (
	DomainBrowser = "browser"
	DomainMail    = "mail"
	DomainCert    = "cert"
)

// The report type names and body field names the decoders and the schema hooks
// share. A literal repeated across the intake is a typo waiting to happen, and
// the linter is right to say so.
const (
	typeCSPViolation         = "csp-violation"
	typeCOEP                 = "coep"
	typeCOEPViolation        = "coep-violation"
	typeCOOP                 = "coop"
	typeNetworkError         = "network-error"
	typeDeprecation          = "deprecation"
	typeIntervention         = "intervention"
	typeCrash                = "crash"
	typeIntegrityViolation   = "integrity-violation"
	typePermissions          = "permissions-policy-violation"
	typeFeaturePolicy        = "feature-policy-violation"
	typePotentialPermissions = "potential-permissions-policy-violation"
	typeDocumentPolicy       = "document-policy-violation"
	typeExpectCT             = "expect-ct"
	typeHPKP                 = "hpkp"
	typeIodef                = "iodef"

	fieldDocumentURL        = "documentURL"
	fieldBlockedURL         = "blockedURL"
	fieldEffectiveDirective = "effectiveDirective"
	fieldSourceFile         = "sourceFile"
	fieldReferrer           = "referrer"
	fieldDisposition        = "disposition"
	fieldPolicyID           = "policyId"
	fieldType               = "type"
)

// urlFields are the report body keys whose value is a URL. They are redacted
// unless the operator opted out, because a query string routinely carries a token.
var urlFields = map[string]bool{
	"documentURI":     true,
	fieldBlockedURL:   true,
	fieldReferrer:     true,
	"document-uri":    true,
	"blocked-uri":     true,
	"source-file":     true,
	fieldSourceFile:   true,
	"srcAttribute":    true,
	"allowAttribute":  true,
	"initialPopupURL": true,
	"openeeURL":       true,
	"effectiveURI":    true,
	"sourceURL":       true,
	"sample":          true,
	fieldDocumentURL:  true,
}

// redactURL drops the query string, the fragment and any userinfo of a URL,
// keeping the part that identifies the resource.
func redactURL(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		if idx := strings.IndexAny(value, "?#"); idx >= 0 {
			return value[:idx]
		}
		return value
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// redactBody walks a decoded report body and redacts every URL-shaped field.
// The walk recurses into nested objects and arrays, bounded by maxRedactDepth,
// because a token can hide below the top level.
func redactBody(body map[string]any, keepQuery bool) {
	redactValue(body, keepQuery, 0)
}

// maxRedactDepth bounds the redaction walk. Bodies already passed the JSON
// depth guard, so this is strictly smaller and never the limiting factor.
const maxRedactDepth = 16

// redactValue redacts one level of a decoded body, descending while depth allows.
func redactValue(val any, keepQuery bool, depth int) {
	switch node := val.(type) {
	case map[string]any:
		for key, child := range node {
			if s, ok := child.(string); ok && urlFields[key] && !keepQuery {
				node[key] = redactURL(s)
			} else if depth < maxRedactDepth {
				redactValue(child, keepQuery, depth+1)
			}
		}
	case []any:
		if depth >= maxRedactDepth {
			return
		}
		for _, child := range node {
			redactValue(child, keepQuery, depth+1)
		}
	}
}
