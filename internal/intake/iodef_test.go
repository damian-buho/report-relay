// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"errors"
	"strings"
	"testing"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

const iodefHost = "beta.dbuho.me"

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
