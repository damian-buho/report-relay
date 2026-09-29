// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"kiota.ch/damian-buho/report-relay/internal/guard"
)

const (
	iodefMaxText = 1024
	iodefMaxList = 8
)

type iodefIncident struct {
	id           string
	idName       string
	purpose      string
	status       string
	reportTime   string
	detectTime   string
	startTime    string
	endTime      string
	descriptions []string
	emails       []string
	domains      []string
	nodes        []string
	addresses    []string
	references   []string
}

func decodeIODEF(body []byte, limits guard.Limits, keepQuery bool) ([]Report, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("%w: empty iodef body", ErrNoReports)
	}
	probed := bytes.ToLower(body)
	if bytes.Contains(probed, []byte("<!doctype")) || bytes.Contains(probed, []byte("<!entity")) {
		return nil, fmt.Errorf("%w: dtd declarations are not accepted", ErrInvalidReport)
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = true
	var incidents []*iodefIncident
	var current *iodefIncident
	var stack []string
	var text strings.Builder
	depth := 0
	flush := func() string {
		s := strings.TrimSpace(text.String())
		text.Reset()
		if len(s) > iodefMaxText {
			s = s[:iodefMaxText]
		}
		return s
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > limits.MaxJSONDepth {
				return nil, fmt.Errorf("iodef nests deeper than %d: %w", limits.MaxJSONDepth, guard.ErrTooDeep)
			}
			stack = append(stack, tok.Name.Local)
			text.Reset()
			switch tok.Name.Local {
			case "Incident":
				if len(incidents) >= limits.MaxArrayItems {
					return nil, fmt.Errorf("iodef carries more than %d incidents: %w", limits.MaxArrayItems, guard.ErrArrayTooLong)
				}
				current = &iodefIncident{}
				incidents = append(incidents, current)
				for _, attr := range tok.Attr {
					if attr.Name.Local == "purpose" {
						current.purpose = clipIODEF(attr.Value)
					}
					if attr.Name.Local == "status" {
						current.status = clipIODEF(attr.Value)
					}
				}
			case "IncidentID":
				if current == nil {
					break
				}
				for _, attr := range tok.Attr {
					if attr.Name.Local == "name" {
						current.idName = clipIODEF(attr.Value)
					}
				}
			}
		case xml.EndElement:
			name := tok.Name.Local
			if current != nil {
				collectIODEF(current, stack, name, flush())
			} else {
				flush()
			}
			if name == "Incident" {
				current = nil
			}
			if n := len(stack); n > 0 {
				stack = stack[:n-1]
			}
			depth--
		case xml.CharData:
			text.Write(tok)
		}
	}
	if len(incidents) == 0 {
		return nil, fmt.Errorf("%w: no Incident element", ErrNoReports)
	}
	reports := make([]Report, 0, len(incidents))
	for _, inc := range incidents {
		if inc.id == "" {
			return nil, fmt.Errorf("%w: incident without IncidentID", ErrInvalidReport)
		}
		reports = append(reports, iodefReport(inc, keepQuery))
	}
	return reports, nil
}

func collectIODEF(current *iodefIncident, stack []string, name, s string) {
	switch name {
	case "IncidentID":
		current.id = s
	case "Description":
		current.descriptions = appendIODEF(current.descriptions, s)
	case "ReportTime":
		current.reportTime = s
	case "DetectTime":
		current.detectTime = s
	case "StartTime":
		current.startTime = s
	case "EndTime":
		current.endTime = s
	case "NodeName":
		current.nodes = appendIODEF(current.nodes, s)
	case "Address":
		current.addresses = appendIODEF(current.addresses, s)
	case "EmailTo":
		current.emails = appendIODEF(current.emails, s)
	case "EmailFrom":
		current.emails = appendIODEF(current.emails, s)
	case "URL":
		current.references = appendIODEF(current.references, s)
	case "Name":
		if inIODEF(stack, "DomainData") {
			current.domains = appendIODEF(current.domains, s)
		}
	}
}

func inIODEF(stack []string, parent string) bool {
	for _, el := range stack {
		if el == parent {
			return true
		}
	}
	return false
}

func appendIODEF(list []string, s string) []string {
	if s == "" {
		return list
	}
	if len(list) >= iodefMaxList {
		return list
	}
	return append(list, s)
}

func clipIODEF(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > iodefMaxText {
		s = s[:iodefMaxText]
	}
	return s
}

func iodefReport(inc *iodefIncident, keepQuery bool) Report {
	url := firstIODEF(inc.domains, inc.nodes, inc.addresses)
	if url == "" && len(inc.references) > 0 {
		url = redactURL(inc.references[0])
	} else if !keepQuery {
		url = redactURL(url)
	}
	body := map[string]any{"incident-id": inc.id}
	if inc.idName != "" {
		body["incident-id-name"] = inc.idName
	}
	if inc.purpose != "" {
		body["purpose"] = inc.purpose
	}
	if inc.status != "" {
		body["status"] = inc.status
	}
	setIODEF(body, "report-time", inc.reportTime)
	setIODEF(body, "detect-time", inc.detectTime)
	setIODEF(body, "start-time", inc.startTime)
	setIODEF(body, "end-time", inc.endTime)
	if len(inc.descriptions) > 0 {
		body["description"] = strings.Join(inc.descriptions, " | ")
	}
	if len(inc.emails) > 0 {
		body["contact-email"] = strings.Join(inc.emails, ", ")
	}
	joinIODEF(body, "domains", inc.domains)
	joinIODEF(body, "nodes", inc.nodes)
	joinIODEF(body, "addresses", inc.addresses)
	joinIODEF(body, "references", inc.references)
	return Report{Type: typeIodef, Domain: DomainCert, Source: SourceIODEF, URL: url, Body: body}
}

func setIODEF(body map[string]any, key, val string) {
	if val == "" {
		return
	}
	body[key] = val
}

func joinIODEF(body map[string]any, key string, list []string) {
	if len(list) == 0 {
		return
	}
	body[key] = strings.Join(list, ", ")
}

func firstIODEF(lists ...[]string) string {
	for _, list := range lists {
		if len(list) > 0 {
			return list[0]
		}
	}
	return ""
}
