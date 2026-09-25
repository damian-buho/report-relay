// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

func testLimits() guard.Limits {
	return guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 32, MaxArrayItems: 512}
}

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
	if report.URL != "https://beta.dbuho.me/" {
		t.Errorf("url = %q, want the query and fragment dropped", report.URL)
	}
	if report.Body[fieldEffectiveDirectve] != "script-src" {
		t.Errorf("effectiveDirective = %v, want the camelCase key the Reporting API uses", report.Body["effectiveDirective"])
	}
	if _, legacy := report.Body["document-uri"]; legacy {
		t.Error("the kebab-case key survived normalisation")
	}
	if report.Body[fieldBlockedURL] != "https://evil.example/x.js" {
		t.Errorf("blockedURL = %v, want the query dropped", report.Body["blockedURL"])
	}
}

func TestReportingAPIBatchOfThreeBecomesThreeReports(t *testing.T) {
	body := []byte(`[
	  {"type":"csp-violation","age":5,"url":"https://beta.dbuho.me/?t=1",
	   "body":{"documentURL":"https://beta.dbuho.me/","effectiveDirective":"script-src","blockedURL":"https://evil.example/x.js"}},
	  {"type":"deprecation","age":60,"url":"https://beta.dbuho.me/legacy",
	   "body":{"documentURL":"https://beta.dbuho.me/legacy"}},
	  {"type":"network-error","age":120,"url":"https://beta.dbuho.me/api",
	   "body":{"documentURL":"https://beta.dbuho.me/api","phase":"dns","type":"dns_error"}}
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
	if reports[1].Severity() != "INFO" {
		t.Errorf("deprecation severity = %q, want INFO", reports[1].Severity())
	}
	if reports[2].Severity() != "WARN" {
		t.Errorf("network-error severity = %q, want WARN", reports[2].Severity())
	}
	if reports[0].URL != "https://beta.dbuho.me/" {
		t.Errorf("url = %q, want the query dropped", reports[0].URL)
	}
}

func TestNELReportIsAcceptedAsNetworkError(t *testing.T) {
	body := []byte(`[{"type":"network-error","age":10,"url":"https://beta.dbuho.me/img.png",
	  "body":{"documentURL":"https://beta.dbuho.me/img.png","phase":"connection"}}]`)
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
	  "body":{"documentURL":"https://beta.dbuho.me/?t=1"}}]`)
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
