// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

const iodefHost = "beta.example.com"

// iodefAltHost is the reported host of the fixtures that build their own node,
// domain or reference rather than reusing the shared one.
const iodefAltHost = "mx1.example.com"

const iodefFixture = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<IODEF-Document version="2.00" lang="en" xmlns="urn:ietf:params:xml:ns:iodef-2.0">` +
	`<Incident purpose="reporting" status="new">` +
	`<IncidentID name="ca1.example.net">caa-violation-001</IncidentID>` +
	`<ReportTime>2026-09-26T00:00:00+00:00</ReportTime>` +
	`<DetectTime>2026-09-25T23:00:00+00:00</DetectTime>` +
	`<Description>Certificate requested against CAA policy</Description>` +
	`<Contact type="organization" role="creator"><Email><EmailTo>abuse@ca1.example.net</EmailTo></Email></Contact>` +
	`<EventData><Flow><System category="target"><Node><NodeName>` + iodefHost + `</NodeName>` +
	`<Address category="ipv4-addr">93.184.216.34</Address></Node>` +
	`<Service><Port>443</Port></Service></System></Flow></EventData>` +
	`</Incident></IODEF-Document>`

// TestIODEFDescriptionIsCutOnARuneBoundary pins the same clip the type paths
// use: 1024 bytes leaves one byte inside a three-byte rune, and a description
// is the longest free text an IODEF report carries.
func TestIODEFDescriptionIsCutOnARuneBoundary(t *testing.T) {
	body := `<IODEF-Document><Incident purpose="reporting"><IncidentID name="ca1.example.net">caa-1</IncidentID>` +
		`<Description>` + strings.Repeat("€", 400) + `</Description>` +
		`<EventData><Flow><System category="target"><Node><NodeName>` + iodefHost + `</NodeName></Node></System></Flow></EventData>` +
		`</Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	kept, ok := reports[0].Body["description"].(string)
	if !ok {
		t.Fatalf("description = %v, want the text in the body", reports[0].Body["description"])
	}
	if len(kept) > maxTextBytes {
		t.Errorf("description is %d bytes, want at most %d", len(kept), maxTextBytes)
	}
	if !utf8.ValidString(kept) {
		t.Error("description is not valid UTF-8, so a strict collector may refuse the export")
	}
	if got := utf8.RuneCountInString(kept); got != 341 {
		t.Errorf("description keeps %d runes, want the 341 whole ones", got)
	}
}

func TestIODEFMinimalReportIsAccepted(t *testing.T) {
	reports, err := Decode(MediaIODEF, []byte(iodefFixture), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]
	if report.Type != typeIodef {
		t.Errorf("type = %q, want iodef", report.Type)
	}
	if report.Domain != DomainCert {
		t.Errorf("domain = %q, want %q", report.Domain, DomainCert)
	}
	if report.Source != SourceIODEF {
		t.Errorf("source = %q, want %q", report.Source, SourceIODEF)
	}
	if report.URL != iodefHost {
		t.Errorf("url = %q, want the reported node", report.URL)
	}
	if report.Body["incident-id"] != "caa-violation-001" {
		t.Errorf("incident-id = %v, want the sender's identifier", report.Body["incident-id"])
	}
	if report.Severity() != sevWarn {
		t.Errorf("severity = %q, want WARN for a certificate incident", report.Severity())
	}
}

func TestIODEFMediaTypesShareTheDecoder(t *testing.T) {
	for _, media := range []string{MediaIODEF, MediaXML, MediaTextXML} {
		reports, err := Decode(media, []byte(iodefFixture), testLimits(), false)
		if err != nil {
			t.Errorf("Decode(%s): %v", media, err)
			continue
		}
		if len(reports) != 1 || reports[0].Type != typeIodef {
			t.Errorf("Decode(%s) = %+v, want one iodef report", media, reports)
		}
	}
}

func TestIODEFRIDEnvelopeIsAccepted(t *testing.T) {
	body := `<RID-Message xmlns="urn:ietf:params:xml:ns:rid-2.0">` +
		`<Incident purpose="reporting"><IncidentID name="ca1.example.net">rid-7</IncidentID>` +
		`<Node><NodeName>` + iodefHost + `</NodeName></Node></Incident></RID-Message>`
	reports, err := Decode(MediaTextXML, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 1 || reports[0].Body["incident-id"] != "rid-7" {
		t.Fatalf("reports = %+v, want the wrapped incident", reports)
	}
}

func TestIODEFRejectsIncidentWithoutID(t *testing.T) {
	body := `<IODEF-Document><Incident purpose="reporting"><Description>no id</Description></Incident></IODEF-Document>`
	if _, err := Decode(MediaIODEF, []byte(body), testLimits(), false); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport", err)
	}
}

func TestIODEFRejectsDocumentWithoutIncident(t *testing.T) {
	body := `<IODEF-Document version="2.00"></IODEF-Document>`
	if _, err := Decode(MediaIODEF, []byte(body), testLimits(), false); !errors.Is(err, ErrNoReports) {
		t.Fatalf("err = %v, want ErrNoReports", err)
	}
}

func TestIODEFRejectsMalformedXML(t *testing.T) {
	if _, err := Decode(MediaIODEF, []byte(`<IODEF-Document><Incident>`), testLimits(), false); err == nil {
		t.Fatal("truncated XML was accepted")
	}
}

func TestIODEFRejectsDTDDeclarations(t *testing.T) {
	body := `<!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>` +
		`<IODEF-Document><Incident><IncidentID>n</IncidentID></Incident></IODEF-Document>`
	if _, err := Decode(MediaIODEF, []byte(body), testLimits(), false); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport for a DTD body", err)
	}
}

func TestIODEFDomainDataNameBecomesTheURL(t *testing.T) {
	body := `<IODEF-Document><Incident><IncidentID name="ca">d1</IncidentID>` +
		`<EventData><Flow><System category="target"><Node><DomainData><Name>` + iodefHost + `</Name>` +
		`</DomainData></Node></System></Flow></EventData></Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].URL != iodefHost {
		t.Errorf("url = %q, want the DomainData name", reports[0].URL)
	}
	if reports[0].Body["domains"] != iodefHost {
		t.Errorf("domains = %v, want the domain carried through", reports[0].Body["domains"])
	}
}

func TestIODEFTwoIncidentsBecomeTwoReports(t *testing.T) {
	body := `<IODEF-Document>` +
		`<Incident><IncidentID name="ca">i-1</IncidentID><Node><NodeName>a.example</NodeName></Node></Incident>` +
		`<Incident><IncidentID name="ca">i-2</IncidentID><Node><NodeName>b.example</NodeName></Node></Incident>` +
		`</IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want one per incident", len(reports))
	}
	if reports[1].Body["incident-id"] != "i-2" {
		t.Errorf("second incident-id = %v, want i-2", reports[1].Body["incident-id"])
	}
}

func TestIODEFIncidentsOverTheCapAreRejected(t *testing.T) {
	limits := guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 32, MaxArrayItems: 1}
	body := `<IODEF-Document>` +
		`<Incident><IncidentID name="ca">i-1</IncidentID></Incident>` +
		`<Incident><IncidentID name="ca">i-2</IncidentID></Incident></IODEF-Document>`
	if _, err := Decode(MediaIODEF, []byte(body), limits, false); !errors.Is(err, guard.ErrArrayTooLong) {
		t.Fatalf("err = %v, want ErrArrayTooLong", err)
	}
}

// TestIODEFIncidentAttributesBecomeBodyKeys pins the two Incident attributes
// and the IncidentID name attribute, which a sender fills and a query selects on.
func TestIODEFIncidentAttributesBecomeBodyKeys(t *testing.T) {
	reports, err := Decode(MediaIODEF, []byte(iodefFixture), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	body := reports[0].Body
	if body["incident-id-name"] != "ca1.example.net" {
		t.Errorf("incident-id-name = %v, want the IncidentID name attribute", body["incident-id-name"])
	}
	if body["purpose"] != "reporting" {
		t.Errorf("purpose = %v, want the Incident purpose attribute", body["purpose"])
	}
	if body["status"] != "new" {
		t.Errorf("status = %v, want the Incident status attribute", body["status"])
	}
}

// TestIODEFSiblingAtTheDepthCapIsAccepted pins the depth counter on the way back
// up: a second incident that starts where the first one ended sits at the same
// depth, so a RID wrapper carrying several incidents stays inside the cap.
func TestIODEFSiblingAtTheDepthCapIsAccepted(t *testing.T) {
	body := []byte(`<RID-Message>` +
		`<Incident purpose="reporting"><IncidentID name="ca">i-1</IncidentID></Incident>` +
		`<Incident purpose="reporting"><IncidentID name="ca">i-2</IncidentID></Incident>` +
		`</RID-Message>`)
	atCap := guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 3, MaxArrayItems: 512, MaxBodyKeys: 512}
	reports, err := Decode(MediaTextXML, body, atCap, false)
	if err != nil {
		t.Fatalf("a sibling element at the depth cap was refused: %v", err)
	}
	if len(reports) != 2 {
		t.Errorf("got %d reports, want both incidents", len(reports))
	}
}

// TestIODEFBodyKeysAreOmittedWhenTheSenderLeftThemOut pins the other side of the
// same mapping: an absent field is not carried as an empty attribute.
func TestIODEFBodyKeysAreOmittedWhenTheSenderLeftThemOut(t *testing.T) {
	body := `<IODEF-Document><Incident><IncidentID>i-1</IncidentID>` +
		`<Node><NodeName>` + iodefAltHost + `</NodeName></Node></Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for _, key := range []string{
		"incident-id-name", "purpose", "status", "description", "contact-email",
		"report-time", "detect-time", "start-time", "end-time",
	} {
		if _, ok := reports[0].Body[key]; ok {
			t.Errorf("body carries %q with no value in the incident: %v", key, reports[0].Body)
		}
	}
}

// TestIODEFTimesBecomeBodyKeys is the populated side of the times: a sender that
// reports them gets queryable attributes.
func TestIODEFTimesBecomeBodyKeys(t *testing.T) {
	reports, err := Decode(MediaIODEF, []byte(iodefFixture), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	body := reports[0].Body
	if body["report-time"] != "2026-09-26T00:00:00+00:00" {
		t.Errorf("report-time = %v, want the incident's report time", body["report-time"])
	}
	if body["detect-time"] != "2026-09-25T23:00:00+00:00" {
		t.Errorf("detect-time = %v, want the incident's detect time", body["detect-time"])
	}
	if _, ok := body["start-time"]; ok {
		t.Errorf("start-time = %v, want no key invented for a time the sender omitted", body["start-time"])
	}
	if _, ok := body["end-time"]; ok {
		t.Errorf("end-time = %v, want no key invented for a time the sender omitted", body["end-time"])
	}
}

// TestIODEFNestingAtTheDepthCapIsAccepted pins the XML depth cap at the value it
// admits, and one level past it: the document is three elements deep.
func TestIODEFNestingAtTheDepthCapIsAccepted(t *testing.T) {
	body := []byte(`<IODEF-Document><Incident purpose="reporting"><IncidentID>i-1</IncidentID></Incident></IODEF-Document>`)
	atCap := guard.Limits{MaxBodyBytes: 65536, MaxJSONDepth: 3, MaxArrayItems: 512, MaxBodyKeys: 512}
	if _, err := Decode(MediaIODEF, body, atCap, false); err != nil {
		t.Fatalf("a document exactly at the depth cap was refused: %v", err)
	}
	pastCap := atCap
	pastCap.MaxJSONDepth = 2
	if _, err := Decode(MediaIODEF, body, pastCap, false); !errors.Is(err, guard.ErrTooDeep) {
		t.Fatalf("err = %v, want ErrTooDeep one level past the cap", err)
	}
}

// TestIODEFListHoldsAtItsCap pins the per-list bound: a ninth description is
// dropped, not appended.
func TestIODEFListHoldsAtItsCap(t *testing.T) {
	var descriptions strings.Builder
	for i := range iodefMaxList + 1 {
		if i > 0 {
			descriptions.WriteString(`</Description><Description>`)
		}
		descriptions.WriteString("d" + strconv.Itoa(i))
	}
	body := `<IODEF-Document><Incident purpose="reporting"><IncidentID>i-1</IncidentID><Description>` +
		descriptions.String() + `</Description>` +
		`<Node><NodeName>` + iodefAltHost + `</NodeName></Node></Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	kept, _ := reports[0].Body["description"].(string)
	if parts := strings.Split(kept, " | "); len(parts) != iodefMaxList {
		t.Errorf("description = %v, want %d entries", kept, iodefMaxList)
	}
}

// TestIODEFNameOutsideDomainDataIsNotADomain pins the only parent Name has: a
// Name elsewhere in the incident is not a reported domain.
func TestIODEFNameOutsideDomainDataIsNotADomain(t *testing.T) {
	body := `<IODEF-Document><Incident purpose="reporting"><IncidentID name="ca">i-1</IncidentID>` +
		`<Contact type="organization"><Name>` + iodefAltHost + `</Name></Contact>` +
		`<Node><NodeName>` + iodefAltHost + `</NodeName></Node></Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := reports[0].Body["domains"]; ok {
		t.Errorf("domains = %v, want a Name outside DomainData ignored", reports[0].Body["domains"])
	}
	if reports[0].URL != iodefAltHost {
		t.Errorf("url = %q, want the reported node", reports[0].URL)
	}
}

// TestIODEFIncidentIDOutsideAnIncidentIsIgnored pins the guard: an IncidentID
// before any Incident names no incident, so it annotates nothing.
func TestIODEFIncidentIDOutsideAnIncidentIsIgnored(t *testing.T) {
	body := `<IODEF-Document><IncidentID name="wrapped">outer</IncidentID>` +
		`<Incident purpose="reporting"><IncidentID name="ca1.example.net">inner</IncidentID>` +
		`<Node><NodeName>` + iodefAltHost + `</NodeName></Node></Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Body["incident-id-name"] != "ca1.example.net" {
		t.Errorf("incident-id-name = %v, want the incident's own IncidentID", reports[0].Body["incident-id-name"])
	}
	if reports[0].Body["incident-id"] != "inner" {
		t.Errorf("incident-id = %v, want inner", reports[0].Body["incident-id"])
	}
}

// TestIODEFReferenceBecomesTheURLWhenNoNodeIsReported pins the fallback: with no
// node, domain or address, the first reference is the record's subject.
func TestIODEFReferenceBecomesTheURLWhenNoNodeIsReported(t *testing.T) {
	body := `<IODEF-Document><Incident purpose="reporting"><IncidentID name="ca">i-1</IncidentID>` +
		`<Reference><URL>https://mx1.example.com/mail?token=secret</URL></Reference>` +
		`</Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].URL != "https://mx1.example.com/mail" {
		t.Errorf("url = %q, want the reported reference with its query dropped", reports[0].URL)
	}
}

// TestIODEFSpareIncidentCarriesNoURL pins the empty case of the same fallback:
// neither a node nor a reference leaves the record without a subject.
func TestIODEFSpareIncidentCarriesNoURL(t *testing.T) {
	body := `<IODEF-Document><Incident purpose="reporting"><IncidentID name="ca">i-1</IncidentID>` +
		`<Description>nothing addressable</Description></Incident></IODEF-Document>`
	reports, err := Decode(MediaIODEF, []byte(body), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].URL != "" {
		t.Errorf("url = %q, want empty", reports[0].URL)
	}
}

func TestIODEFContactEmailIsKept(t *testing.T) {
	reports, err := Decode(MediaIODEF, []byte(iodefFixture), testLimits(), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if reports[0].Body["contact-email"] != "abuse@ca1.example.net" {
		t.Errorf("contact-email = %v, want the notifying address", reports[0].Body["contact-email"])
	}
	if !strings.Contains(reports[0].Body["description"].(string), "CAA policy") {
		t.Errorf("description = %v, want the sender's text", reports[0].Body["description"])
	}
}
