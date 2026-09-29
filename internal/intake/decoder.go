// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"fmt"
	"strings"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

// Decoder turns one request body into the reports it carries.
type Decoder func(body []byte, limits guard.Limits, keepQuery bool) ([]Report, error)

// decoder pairs a media type with the function that reads it.
type decoder struct {
	mediaType string
	decode    Decoder
}

// decoders is the registry of wire formats, keyed by media type. A new intake
// type is one more entry here, plus an enable flag; nothing else changes.
var decoders = map[string]decoder{
	MediaReportingAPI: {mediaType: MediaReportingAPI, decode: decodeReportingAPI},
	MediaCSPReport:    {mediaType: MediaCSPReport, decode: decodeCSPReport},
	MediaTLSRPTJSON:   {mediaType: MediaTLSRPTJSON, decode: decodeTLSRPT},
	MediaTLSRPTGzip:   {mediaType: MediaTLSRPTGzip, decode: decodeTLSRPT},
	MediaExpectCT:     {mediaType: MediaExpectCT, decode: decodeExpectCT},
	MediaHPKP:         {mediaType: MediaHPKP, decode: decodeHPKP},
	MediaIODEF:        {mediaType: MediaIODEF, decode: decodeIODEF},
	MediaXML:          {mediaType: MediaXML, decode: decodeIODEF},
	MediaTextXML:      {mediaType: MediaTextXML, decode: decodeIODEF},
}

// MediaType returns the bare media type of a Content-Type header value, with
// any parameters stripped and the case folded.
func MediaType(header string) string {
	if idx := indexByte(header, ';'); idx >= 0 {
		header = header[:idx]
	}
	return lowerTrim(header)
}

func indexByte(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// decodeReportingAPI reads a Reporting API batch: a JSON array of reports.
func decodeReportingAPI(body []byte, limits guard.Limits, keepQuery bool) ([]Report, error) {
	var envelopes []reportEnvelope
	if err := guard.Decode(body, &envelopes, limits); err != nil {
		return nil, err
	}
	if len(envelopes) > limits.MaxArrayItems {
		return nil, fmt.Errorf("batch of %d exceeds %d: %w", len(envelopes), limits.MaxArrayItems, guard.ErrArrayTooLong)
	}
	reports := make([]Report, 0, len(envelopes))
	for _, env := range envelopes {
		report, err := reportingAPIReport(env, keepQuery)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// keepRawType stashes the sender's own type string in the body when the label
// had to bucket it as unknown. The raw value stays queryable in the log line,
// but it never becomes a label, so it cannot mint streams or series.
func keepRawType(report *Report, raw string) {
	if report.Type != typeUnknown || strings.EqualFold(raw, typeUnknown) {
		return
	}
	if len(raw) > maxTypeLen*4 {
		raw = raw[:maxTypeLen*4]
	}
	report.Body["reported-type"] = raw
}

func reportingAPIReport(env reportEnvelope, keepQuery bool) (Report, error) {
	report := Report{
		Type:   normalizeType(env.Type),
		Domain: DomainBrowser,
		Source: SourceReportingAPI,
		URL:    env.URL,
		Age:    env.Age,
		Body:   map[string]any{},
	}
	if report.Type == "" {
		return Report{}, fmt.Errorf("%w: report has no type", ErrNoReports)
	}
	if len(env.Body) > 0 {
		if err := guard.DecodeBody(env.Body, &report.Body); err != nil {
			return Report{}, err
		}
	}
	keepRawType(&report, env.Type)
	if hook, ok := bodyHooks[report.Type]; ok {
		if err := hook(report.Body); err != nil {
			return Report{}, fmt.Errorf("%w: type %q: %w", ErrInvalidReport, report.Type, err)
		}
	}
	if !keepQuery {
		report.URL = redactURL(report.URL)
		redactBody(report.Body, keepQuery)
	}
	return report, nil
}

// Decode reads one body in the named wire format. It is the only entry point
// the HTTP layer needs: adding a format means adding a decoders entry, not
// touching a caller.
func Decode(mediaType string, body []byte, limits guard.Limits, keepQuery bool) ([]Report, error) {
	dec, ok := decoders[mediaType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedType, mediaType)
	}
	reports, err := dec.decode(body, limits, keepQuery)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, ErrNoReports
	}
	return reports, nil
}

// lowerTrim folds the case of a header value and drops its spaces.
func lowerTrim(s string) string {
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != ' ' && c != '\t' {
			out = append(out, c)
		}
	}
	return string(out)
}

// cspReportEnvelope is the legacy CSP report-uri body, deprecated by CSP 3 but
// still the only thing an old browser sends.
type cspReportEnvelope struct {
	CSPReport map[string]any `json:"csp-report"`
}

// decodeCSPReport normalises the legacy body into the Reporting API shape, so a
// query never has to care which mechanism the browser used.
func decodeCSPReport(body []byte, limits guard.Limits, keepQuery bool) ([]Report, error) {
	var env cspReportEnvelope
	if err := guard.Decode(body, &env, limits); err != nil {
		return nil, err
	}
	if len(env.CSPReport) == 0 {
		return nil, fmt.Errorf("%w: csp-report is empty", ErrNoReports)
	}
	report := Report{
		Type:   typeCSPViolation,
		Domain: DomainBrowser,
		Source: SourceCSP,
		Body:   camelCaseKeys(env.CSPReport),
	}
	if doc, ok := env.CSPReport["document-uri"].(string); ok {
		report.URL = doc
	}
	if !keepQuery {
		report.URL = redactURL(report.URL)
		redactBody(report.Body, keepQuery)
	}
	if hook, ok := bodyHooks[report.Type]; ok {
		if err := hook(report.Body); err != nil {
			return nil, fmt.Errorf("%w: type %q: %w", ErrInvalidReport, report.Type, err)
		}
	}
	return []Report{report}, nil
}

// legacyCSPKeys maps the kebab-case keys of a legacy CSP report onto the
// camelCase names the Reporting API uses, so both wire formats feed one hook.
var legacyCSPKeys = map[string]string{
	"document-uri":        fieldDocumentURL,
	"referrer":            fieldReferrer,
	"violated-directive":  fieldEffectiveDirective,
	"effective-directive": fieldEffectiveDirective,
	"blocked-uri":         fieldBlockedURL,
	"source-file":         fieldSourceFile,
	"original-policy":     "originalPolicy",
	"disposition":         fieldDisposition,
	"status-code":         "statusCode",
	"script-sample":       "scriptSample",
}

// camelCaseKeys renames the legacy keys in place where they are known, and
// leaves every other key untouched so a future field is not dropped.
func camelCaseKeys(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for key, val := range body {
		if renamed, ok := legacyCSPKeys[key]; ok {
			if _, exists := out[renamed]; !exists {
				out[renamed] = val
				continue
			}
		}
		out[key] = val
	}
	return out
}

// tlsRPTEnvelope is the RFC 8460 report body. The two shapes differ in the
// result: an individual report names each failure, an aggregate report counts
// them per result type.
type tlsRPTEnvelope struct {
	OrganizationName string          `json:"organization-name"`
	ContactInfo      string          `json:"contact-info"`
	ReportID         string          `json:"report-id"`
	ResultType       string          `json:"result-type"`
	FailureInfo      *failureInfo    `json:"failure-info"`
	FailureDetails   []failureDetail `json:"failure-details"`
}

type failureInfo struct {
	ResultType      string           `json:"result-type"`
	FailingSessions map[string]int64 `json:"failing-sessions"`
}

type failureDetail struct {
	ResultType           string `json:"result-type"`
	ResultingCipherSuite string `json:"resulting-cipher-suite"`
	ServerName           string `json:"server-name"`
}

// decodeTLSRPT turns one RFC 8460 report into one record per failure: one per
// failure-details entry for an individual report, one per result type for an
// aggregate report, whose session counts ride along as attributes.
func decodeTLSRPT(body []byte, limits guard.Limits, _ bool) ([]Report, error) {
	var env tlsRPTEnvelope
	if err := guard.Decode(body, &env, limits); err != nil {
		return nil, err
	}
	if env.OrganizationName == "" {
		return nil, fmt.Errorf("%w: organization-name is empty", ErrNoReports)
	}
	base := func(resultType string) Report {
		sanitized := SanitizeType(resultType)
		report := Report{
			Type:   sanitized,
			Domain: DomainMail,
			Source: SourceTLSRPT,
			URL:    env.OrganizationName,
			Body: map[string]any{
				"organization-name": env.OrganizationName,
				"contact-info":      env.ContactInfo,
				"report-id":         env.ReportID,
			},
		}
		if sanitized == typeUnknown && !strings.EqualFold(resultType, typeUnknown) {
			raw := resultType
			if len(raw) > maxTypeLen*4 {
				raw = raw[:maxTypeLen*4]
			}
			report.Body["reported-type"] = raw
		}
		return report
	}
	if len(env.FailureDetails) > 0 {
		if len(env.FailureDetails) > limits.MaxArrayItems {
			return nil, fmt.Errorf("failure-details of %d exceeds %d: %w", len(env.FailureDetails), limits.MaxArrayItems, guard.ErrArrayTooLong)
		}
		reports := make([]Report, 0, len(env.FailureDetails))
		for _, detail := range env.FailureDetails {
			report := base(detail.ResultType)
			report.Body["resulting-cipher-suite"] = detail.ResultingCipherSuite
			report.Body["server-name"] = detail.ServerName
			reports = append(reports, report)
		}
		return reports, nil
	}
	if env.FailureInfo == nil {
		return nil, fmt.Errorf("%w: neither failure-info nor failure-details", ErrNoReports)
	}
	report := base(env.FailureInfo.ResultType)
	sessions := make(map[string]int64, len(env.FailureInfo.FailingSessions))
	for result, count := range env.FailureInfo.FailingSessions {
		sessions[result] = count
	}
	report.Body["failing-sessions"] = sessions
	return []Report{report}, nil
}

// expectCTEnvelope is the RFC 9163 body: one expect-ct-report object carrying
// the hostname that failed the CT compliance check and both chains.
type expectCTEnvelope struct {
	ExpectCT map[string]any `json:"expect-ct-report"`
}

// decodeExpectCT reads one RFC 9163 violation report. The hostname is the
// record's subject: a report without one names nothing to select on.
func decodeExpectCT(body []byte, limits guard.Limits, keepQuery bool) ([]Report, error) {
	var env expectCTEnvelope
	if err := guard.Decode(body, &env, limits); err != nil {
		return nil, err
	}
	if len(env.ExpectCT) == 0 {
		return nil, fmt.Errorf("%w: expect-ct-report is empty", ErrNoReports)
	}
	host, _ := env.ExpectCT["hostname"].(string)
	if host == "" {
		return nil, fmt.Errorf("%w: expect-ct-report has no hostname", ErrInvalidReport)
	}
	report := Report{
		Type:   typeExpectCT,
		Domain: DomainBrowser,
		Source: SourceExpectCT,
		URL:    host,
		Body:   env.ExpectCT,
	}
	if !keepQuery {
		redactBody(report.Body, keepQuery)
	}
	return []Report{report}, nil
}

// hpkpKeys is the full RFC 7469 report shape. application/json carries
// anything, so the shape is the discriminator: only a body with every key is
// a pin validation failure, the rest is not ours to read.
var hpkpKeys = []string{
	"date-time", "hostname", "port", "effective-expiration-date",
	"include-subdomains", "noted-hostname", "served-certificate-chain",
	"validated-certificate-chain", "known-pins",
}

// decodeHPKP reads one RFC 7469 pin validation failure. HPKP registered no
// media type of its own, so this runs on plain application/json and admits
// only the exact report shape.
func decodeHPKP(body []byte, limits guard.Limits, keepQuery bool) ([]Report, error) {
	var reportBody map[string]any
	if err := guard.Decode(body, &reportBody, limits); err != nil {
		return nil, err
	}
	for _, key := range hpkpKeys {
		if _, ok := reportBody[key]; !ok {
			return nil, fmt.Errorf("%w: json is not a pin validation failure", ErrInvalidReport)
		}
	}
	host, _ := reportBody["hostname"].(string)
	report := Report{
		Type:   typeHPKP,
		Domain: DomainBrowser,
		Source: SourceHPKP,
		URL:    host,
		Body:   reportBody,
	}
	if !keepQuery {
		redactBody(report.Body, keepQuery)
	}
	return []Report{report}, nil
}
