// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

func testLimits() guard.Limits {
	return guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 32, MaxArrayItems: 512}
}

// testSiteURL and testBlockedURL are the sites every fixture reports on, and
// sevInfo/sevWarn the severities assertions compare against. Constants, not
// literals, because the linter counts repetitions even in tests.
const (
	testSiteURL    = "https://beta.dbuho.me/"
	testBlockedURL = "https://evil.example/x.js"
	sevInfo        = "INFO"
	sevWarn        = "WARN"
)

// legacyCSPBody is a real report-uri body: kebab-case keys inside "csp-report".
const legacyCSPBody = `{"csp-report":{"document-uri":"https://beta.dbuho.me/?token=secret#frag",` +
	`"violated-directive":"script-src","blocked-uri":"https://evil.example/x.js?a=b",` +
	`"effective-directive":"script-src","original-policy":"default-src 'self'"}}`

func TestLegacyCSPReportNormalisesToReportingAPI(t *testing.T) {
	reports, err := Decode(MediaCSPReport, []byte(legacyCSPBody), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]
	if report.Type != typeCSPViolation {
		t.Errorf("type = %q, want csp-violation", report.Type)
	}
	if report.Domain != DomainBrowser {
		t.Errorf("domain = %q, want %q", report.Domain, DomainBrowser)
	}
	if report.Source != SourceCSP {
		t.Errorf("source = %q, want %q", report.Source, SourceCSP)
	}
	if report.URL != testSiteURL {
		t.Errorf("url = %q, want the query and fragment dropped", report.URL)
	}
	if report.Body[fieldEffectiveDirective] != "script-src" {
		t.Errorf("effectiveDirective = %v, want the camelCase key the Reporting API uses", report.Body["effectiveDirective"])
	}
	if _, legacy := report.Body["document-uri"]; legacy {
		t.Error("the kebab-case key survived normalisation")
	}
	if report.Body[fieldBlockedURL] != testBlockedURL {
		t.Errorf("blockedURL = %v, want the query dropped", report.Body["blockedURL"])
	}
}

func TestReportingAPIBatchOfThreeBecomesThreeReports(t *testing.T) {
	body := []byte(`[
	  {"type":"csp-violation","age":5,"url":"https://beta.dbuho.me/?t=1",
	   "body":{"documentURL":"https://beta.dbuho.me/","effectiveDirective":"script-src","blockedURL":"https://evil.example/x.js"}},
	  {"type":"deprecation","age":60,"url":"https://beta.dbuho.me/legacy",
	   "body":{"id":"websql","message":"WebSQL is deprecated","sourceFile":"https://beta.dbuho.me/legacy"}},
	  {"type":"network-error","age":120,"url":"https://beta.dbuho.me/api",
	   "body":{"phase":"dns","type":"dns.address_changed","method":"GET","protocol":"http/1.1","referrer":"https://beta.dbuho.me/","sampling-fraction":1.0,"server-ip":"93.184.216.34","status-code":0,"elapsed-time":12}}
	]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("got %d reports, want 3", len(reports))
	}
	if reports[0].Age != 5 || reports[1].Age != 60 || reports[2].Age != 120 {
		t.Errorf("ages = %d/%d/%d, want 5/60/120", reports[0].Age, reports[1].Age, reports[2].Age)
	}
	if reports[1].Severity() != sevInfo {
		t.Errorf("deprecation severity = %q, want INFO", reports[1].Severity())
	}
	if reports[2].Severity() != sevWarn {
		t.Errorf("network-error severity = %q, want WARN", reports[2].Severity())
	}
	if reports[0].URL != testSiteURL {
		t.Errorf("url = %q, want the query dropped", reports[0].URL)
	}
}

func TestNELReportIsAcceptedAsNetworkError(t *testing.T) {
	body := []byte(`[{"type":"network-error","age":10,"url":"https://beta.dbuho.me/img.png",
	  "body":{"phase":"connection","type":"tcp.timed_out","method":"GET","protocol":"h2","referrer":"https://beta.dbuho.me/","sampling-fraction":1.0,"server-ip":"93.184.216.34","status-code":0,"elapsed-time":210}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeNetworkError {
		t.Errorf("type = %q, want network-error", reports[0].Type)
	}
}

func TestUnknownReportTypeSurvives(t *testing.T) {
	// A CT report, or any type added to the Reporting API after this build: the
	// envelope carries it and the body reaches the record untouched.
	body := []byte(`[{"type":"certificate-transparency","age":7,"url":"https://beta.dbuho.me/",
	  "body":{"ct-policy":"scts","host":"beta.dbuho.me","future-field":{"nested":1}}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != "certificate-transparency" {
		t.Errorf("type = %q, want the sender's own type preserved", reports[0].Type)
	}
	if reports[0].Body["ct-policy"] != "scts" {
		t.Errorf("body = %v, want the unknown type's body carried through", reports[0].Body)
	}
	raw, ok := reports[0].Body["future-field"].(map[string]any)
	if !ok {
		t.Fatalf("nested body = %T, want a map", reports[0].Body["future-field"])
	}
	if raw["nested"].(json.Number).String() != "1" {
		t.Errorf("nested value = %v, want 1", raw["nested"])
	}
}

func TestNormalizeTypeFoldsSpellings(t *testing.T) {
	cases := map[string]string{
		typeCSPViolation:          typeCSPViolation,
		"CSPViolationReportBody":  typeCSPViolation,
		typeCOEP:                  typeCOEP,
		"COEPViolationReportBody": typeCOEP,
		"nel":                     typeNetworkError,
		typeDeprecation:           typeDeprecation,
	}
	for input, want := range cases {
		if got := normalizeType(input); got != want {
			t.Errorf("normalizeType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTLSRPTIndividualReportYieldsOneRecordPerFailure(t *testing.T) {
	body := []byte(`{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me",
	  "report-id":"2026-09-25T00:00:00Z","result-type":"individual",
	  "failure-details":[
	    {"result-type":"expired","resulting-cipher-suite":"TLS_AES_128_GCM_SHA256","server-name":"mx1.dbuho.me"},
	    {"result-type":"protocol","resulting-cipher-suite":"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256","server-name":"mx2.dbuho.me"}
	  ]}`)
	reports, err := Decode(MediaTLSRPTJSON, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want one per failure detail", len(reports))
	}
	if reports[0].Type != "expired" || reports[1].Type != "protocol" {
		t.Errorf("types = %q/%q, want expired/protocol", reports[0].Type, reports[1].Type)
	}
	if reports[0].Domain != DomainMail {
		t.Errorf("domain = %q, want %q", reports[0].Domain, DomainMail)
	}
	if reports[0].URL != "dbuho.me" {
		t.Errorf("url = %q, want the reporting domain", reports[0].URL)
	}
	if reports[1].Body["server-name"] != "mx2.dbuho.me" {
		t.Errorf("server-name = %v, want the failing host", reports[1].Body["server-name"])
	}
}

func TestTLSRPTAggregateReportCarriesSessionCounts(t *testing.T) {
	body := []byte(`{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me",
	  "report-id":"2026-09-25T00:00:00Z","result-type":"aggregate",
	  "failure-info":{"result-type":"aggregate","failing-sessions":{"expired":4,"protocol":1}}}`)
	reports, err := Decode(MediaTLSRPTJSON, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want one per result type", len(reports))
	}
	sessions, ok := reports[0].Body["failing-sessions"].(map[string]int64)
	if !ok {
		t.Fatalf("failing-sessions = %T, want map[string]int64", reports[0].Body["failing-sessions"])
	}
	if sessions["expired"] != 4 || sessions["protocol"] != 1 {
		t.Errorf("failing-sessions = %v, want expired=4 protocol=1", sessions)
	}
}

func TestGzippedTLSRPTIsDecoded(t *testing.T) {
	raw := `{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me",
	  "report-id":"2026-09-25T00:00:00Z","result-type":"aggregate",
	  "failure-info":{"result-type":"aggregate","failing-sessions":{"expired":2}}}`
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(raw)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	// Decompression belongs to the body reader, so the wire path is exercised
	// the way a real MTA request arrives: compressed body, gzip content coding.
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	body, err := guard.ReadBody(req, testLimits().MaxBodyBytes)
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	reports, err := Decode(MediaTLSRPTGzip, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 || reports[0].Domain != DomainMail {
		t.Fatalf("reports = %+v, want one mail report", reports)
	}
}

func TestMediaTypeStripsParameters(t *testing.T) {
	cases := map[string]string{
		MediaReportingAPI:                     MediaReportingAPI,
		MediaReportingAPI + "; charset=utf-8": MediaReportingAPI,
		"Application/CSP-Report":              MediaCSPReport,
		"  application/tlsrpt+json  ":         MediaTLSRPTJSON,
	}
	for input, want := range cases {
		if got := MediaType(input); got != want {
			t.Errorf("MediaType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSchemaRejectsIncompleteKnownType(t *testing.T) {
	body := []byte(`[{"type":"csp-violation","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"documentURL":"https://beta.dbuho.me/"}}]`)
	if _, err := Decode(MediaReportingAPI, body, testLimits(), false); err == nil {
		t.Fatal("a csp-violation without effectiveDirective and blockedURL was accepted")
	}
}

func TestSchemaAcceptsUnknownTypeWithoutBody(t *testing.T) {
	body := []byte(`[{"type":"brand-new","age":1,"url":"https://beta.dbuho.me/"}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != "brand-new" {
		t.Errorf("type = %q, want brand-new", reports[0].Type)
	}
}

func TestKeepQueryPreservesTheURL(t *testing.T) {
	body := []byte(`[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/?t=1",
	  "body":{"id":"websql","message":"WebSQL is deprecated","documentURL":"https://beta.dbuho.me/?t=1"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), true)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].URL != "https://beta.dbuho.me/?t=1" {
		t.Errorf("url = %q, want the query kept", reports[0].URL)
	}
	if reports[0].Body["documentURL"] != "https://beta.dbuho.me/?t=1" {
		t.Errorf("documentURL = %v, want the query kept", reports[0].Body["documentURL"])
	}
}

func TestReportingAPIBatchOverTheArrayCapIsRejected(t *testing.T) {
	limits := guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 32, MaxArrayItems: 2}
	body := []byte(`[
	  {"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}},
	  {"type":"deprecation","age":2,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}},
	  {"type":"deprecation","age":3,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}}
	]`)
	if _, err := Decode(MediaReportingAPI, body, limits, false); !errors.Is(err, guard.ErrArrayTooLong) {
		t.Fatalf("err = %v, want ErrArrayTooLong on the request path", err)
	}
}

func TestMaliciousTypeBucketsAsUnknownWithRawKept(t *testing.T) {
	body := []byte(`[{"type":"xss\"><svg onload=alert(1)>","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"documentURL":"https://beta.dbuho.me/"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeUnknown {
		t.Errorf("type = %q, want the label-safe bucket", reports[0].Type)
	}
	if _, ok := reports[0].Body["reported-type"]; !ok {
		t.Error("the sender's raw type is missing from the log body")
	}
}

func TestOverlongTypeBucketsAsUnknown(t *testing.T) {
	raw := strings.Repeat("a", 65)
	body := []byte(`[{"type":"` + raw + `","age":1,"url":"https://beta.dbuho.me/"}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeUnknown {
		t.Errorf("type = %q, want unknown past 64 characters", reports[0].Type)
	}
}

func TestUppercaseUnknownTypeIsFolded(t *testing.T) {
	body := []byte(`[{"type":"Certificate-Transparency","age":1,"url":"https://beta.dbuho.me/"}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != "certificate-transparency" {
		t.Errorf("type = %q, want the folded form", reports[0].Type)
	}
}

func TestTLSRPTFailureDetailsOverTheCapAreRejected(t *testing.T) {
	limits := guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 32, MaxArrayItems: 1}
	body := []byte(`{"organization-name":"dbuho.me","report-id":"r1","result-type":"individual",
	  "failure-details":[
	    {"result-type":"expired","server-name":"mx1.dbuho.me"},
	    {"result-type":"protocol","server-name":"mx2.dbuho.me"}]}`)
	if _, err := Decode(MediaTLSRPTJSON, body, limits, false); !errors.Is(err, guard.ErrArrayTooLong) {
		t.Fatalf("err = %v, want ErrArrayTooLong", err)
	}
}

func TestTLSRPTResultTypeIsSanitized(t *testing.T) {
	body := []byte(`{"organization-name":"dbuho.me","report-id":"r1","result-type":"individual",
	  "failure-details":[{"result-type":"EVIL type!!","server-name":"mx1.dbuho.me"}]}`)
	reports, err := Decode(MediaTLSRPTJSON, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeUnknown {
		t.Errorf("type = %q, want unknown", reports[0].Type)
	}
}

func TestRedactURLStripsUserinfo(t *testing.T) {
	if got := redactURL("https://user:secret@beta.dbuho.me/path"); got != "https://beta.dbuho.me/path" {
		t.Errorf("redactURL = %q, want the secret gone", got)
	}
}

func TestLegacySourceFileIsRenamedAndRedacted(t *testing.T) {
	body := []byte(`{"csp-report":{"document-uri":"https://beta.dbuho.me/","violated-directive":"script-src",` +
		`"blocked-uri":"https://evil.example/x.js","effective-directive":"script-src",` +
		`"source-file":"https://beta.dbuho.me/app.js?token=secret"}}`)
	reports, err := Decode(MediaCSPReport, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, legacy := reports[0].Body["source-file"]; legacy {
		t.Error("the kebab-case source-file survived normalisation")
	}
	if reports[0].Body[fieldSourceFile] != "https://beta.dbuho.me/app.js" {
		t.Errorf("sourceFile = %v, want the query dropped", reports[0].Body[fieldSourceFile])
	}
}

func TestLegacyCSPWithoutDocumentURIDropsWithACount(t *testing.T) {
	body := []byte(`{"csp-report":{"violated-directive":"script-src",` +
		`"blocked-uri":"https://evil.example/x.js","effective-directive":"script-src",` +
		`"referrer":"https://beta.dbuho.me/page"}}`)
	if _, err := Decode(MediaCSPReport, body, testLimits(), false); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport: a record without a site is dropped, not stored", err)
	}
}

func TestRedactBodyRecursesIntoNesting(t *testing.T) {
	body := map[string]any{
		"nested": map[string]any{"blockedURL": "https://evil.example/x.js?token=secret"},
		"list":   []any{map[string]any{"documentURL": "https://beta.dbuho.me/a?token=secret"}},
	}
	redactBody(body, false)
	nested := body["nested"].(map[string]any)
	if nested["blockedURL"] != testBlockedURL {
		t.Errorf("nested blockedURL = %v, want the query dropped", nested["blockedURL"])
	}
	list := body["list"].([]any)
	first := list[0].(map[string]any)
	if first["documentURL"] != "https://beta.dbuho.me/a" {
		t.Errorf("listed documentURL = %v, want the query dropped", first["documentURL"])
	}
}

func TestCOOPReportIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"coop","age":3,"url":"https://beta.dbuho.me/",
	  "body":{"disposition":"enforce","effectivePolicy":"same-origin","type":"navigation-to-response",
	  "referrer":"https://beta.dbuho.me/?token=secret"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeCOOP {
		t.Errorf("type = %q, want coop", reports[0].Type)
	}
	if reports[0].Severity() != sevWarn {
		t.Errorf("severity = %q, want WARN for an isolation break", reports[0].Severity())
	}
	if reports[0].Body["referrer"] != testSiteURL {
		t.Errorf("referrer = %v, want the query dropped", reports[0].Body["referrer"])
	}
}

func TestCOOPRejectsMissingPolicy(t *testing.T) {
	body := []byte(`[{"type":"coop","age":3,"url":"https://beta.dbuho.me/",
	  "body":{"disposition":"enforce","type":"navigation-to-response"}}]`)
	if _, err := Decode(MediaReportingAPI, body, testLimits(), false); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport", err)
	}
}

func TestCOEPRealBodyHasNoDocumentURL(t *testing.T) {
	body := []byte(`[{"type":"coep","age":8,"url":"https://beta.dbuho.me/",
	  "body":{"type":"corp","blockedURL":"https://evil.example/x.js?a=b","destination":"script","disposition":"enforce"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeCOEP {
		t.Errorf("type = %q, want coep", reports[0].Type)
	}
	if reports[0].Body["blockedURL"] != testBlockedURL {
		t.Errorf("blockedURL = %v, want the query dropped", reports[0].Body["blockedURL"])
	}
}

func TestDeprecationRealBodyIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"id":"websql","message":"WebSQL is deprecated","sourceFile":"https://beta.dbuho.me/a.js?token=secret"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Severity() != sevInfo {
		t.Errorf("severity = %q, want INFO", reports[0].Severity())
	}
	if reports[0].Body[fieldSourceFile] != "https://beta.dbuho.me/a.js" {
		t.Errorf("sourceFile = %v, want the query dropped", reports[0].Body[fieldSourceFile])
	}
}

func TestInterventionReportIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"intervention","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"id":"audio-no-gesture","message":"A play() request was interrupted"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Severity() != sevInfo {
		t.Errorf("severity = %q, want INFO", reports[0].Severity())
	}
}

func TestCrashReportPassesThrough(t *testing.T) {
	body := []byte(`[{"type":"crash","age":1,"url":"https://beta.dbuho.me/","body":{"reason":"oom"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeCrash || reports[0].Severity() != sevWarn {
		t.Errorf("type/severity = %q/%q, want crash/WARN", reports[0].Type, reports[0].Severity())
	}
	empty := []byte(`[{"type":"crash","age":1,"url":"https://beta.dbuho.me/","body":{}}]`)
	if _, err := Decode(MediaReportingAPI, empty, testLimits(), false); err != nil {
		t.Fatalf("a crash without a reason was rejected: %v", err)
	}
}

func TestIntegrityViolationIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"integrity-violation","age":2,"url":"https://beta.dbuho.me/",
	  "body":{"documentURL":"https://beta.dbuho.me/","blockedURL":"https://cdn.example/lib.js","destination":"script","reportOnly":false}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Severity() != sevWarn {
		t.Errorf("severity = %q, want WARN", reports[0].Severity())
	}
}

func TestPermissionsPolicyRealBodyIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"permissions-policy-violation","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"policyId":"geolocation","disposition":"enforce","message":"Geolocation access denied",
	  "sourceFile":"https://beta.dbuho.me/a.js","lineNumber":7}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Severity() != sevWarn {
		t.Errorf("severity = %q, want WARN", reports[0].Severity())
	}
}

func TestFeaturePolicyLegacyBodyIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"feature-policy-violation","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"featureId":"geolocation","disposition":"enforce"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Type != typeFeaturePolicy {
		t.Errorf("type = %q, want the Firefox spelling kept", reports[0].Type)
	}
}

func TestDocumentPolicyViolationIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"document-policy-violation","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"policyId":"document-write","disposition":"enforce","message":"document.write blocked"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Severity() != sevWarn {
		t.Errorf("severity = %q, want WARN", reports[0].Severity())
	}
}

func TestPotentialPermissionsPolicyViolationIsAccepted(t *testing.T) {
	body := []byte(`[{"type":"potential-permissions-policy-violation","age":1,"url":"https://beta.dbuho.me/",
	  "body":{"policyId":"fullscreen","disposition":"report","message":"Would block",
	  "allowAttribute":"fullscreen","srcAttribute":"https://frames.example/?token=secret"}}]`)
	reports, err := Decode(MediaReportingAPI, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Body["srcAttribute"] != "https://frames.example/" {
		t.Errorf("srcAttribute = %v, want the query dropped", reports[0].Body["srcAttribute"])
	}
}

func TestExpectCTReportIsAccepted(t *testing.T) {
	body := []byte(`{"expect-ct-report":{"date-time":"2026-09-26T00:00:00Z","hostname":"beta.dbuho.me",
	  "port":443,"effective-expiration-date":"2026-10-26T00:00:00Z",
	  "served-certificate-chain":["PEM1"],"validated-certificate-chain":["PEM1"]}}`)
	reports, err := Decode(MediaExpectCT, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]
	if report.Type != typeExpectCT || report.Source != SourceExpectCT {
		t.Errorf("type/source = %q/%q, want expect-ct/expect-ct", report.Type, report.Source)
	}
	if report.URL != "beta.dbuho.me" || report.Severity() != sevWarn {
		t.Errorf("url/severity = %q/%q, want the hostname and WARN", report.URL, report.Severity())
	}
}

func TestExpectCTRejectsMissingHostname(t *testing.T) {
	body := []byte(`{"expect-ct-report":{"date-time":"2026-09-26T00:00:00Z",
	  "served-certificate-chain":["PEM1"]}}`)
	if _, err := Decode(MediaExpectCT, body, testLimits(), false); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport", err)
	}
}

func TestHPKPReportIsAccepted(t *testing.T) {
	body := []byte(`{"date-time":"2026-09-26T00:00:00Z","hostname":"beta.dbuho.me","port":443,
	  "effective-expiration-date":"2026-10-26T00:00:00Z","include-subdomains":false,
	  "noted-hostname":"beta.dbuho.me","served-certificate-chain":["PEM1"],
	  "validated-certificate-chain":["PEM1"],"known-pins":["pin-sha256=\"abcd\""]}`)
	reports, err := Decode(MediaHPKP, body, testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].Type != typeHPKP || reports[0].URL != "beta.dbuho.me" {
		t.Errorf("type/url = %q/%q, want hpkp and the noted host", reports[0].Type, reports[0].URL)
	}
}

func TestHPKPRejectsForeignJSON(t *testing.T) {
	if _, err := Decode(MediaHPKP, []byte(`{"hello":"world"}`), testLimits(), false); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport for non-pin JSON", err)
	}
}
