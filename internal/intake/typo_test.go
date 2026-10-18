// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package intake

import (
	"errors"
	"strings"
	"testing"
)

func decodeTypo(t *testing.T, body string) ([]Report, error) {
	t.Helper()
	return Decode(MediaReportingAPI, []byte(`[{"type":"typo","age":0,"url":"https://beta.example.com/post?token=secret","body":`+body+`}]`), testLimits(), false)
}

func TestTypoHook(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantErr  bool
		wantKeys []string
		fragment string
	}{
		{
			"full", `{"selection":"tpyo","prefix":"in ","suffix":" end","comment":"the","lang":"uk"}`, false,
			[]string{fieldSelection, fieldPrefix, fieldSuffix, fieldComment, fieldLang, fieldTextFragment},
			"https://beta.example.com/post#:~:text=in-,tpyo,-end",
		},
		{
			"selection only", `{"selection":"tpyo"}`, false,
			[]string{fieldSelection, fieldTextFragment},
			"https://beta.example.com/post#:~:text=tpyo",
		},
		{
			"foreign keys dropped", `{"selection":"a-b, c&d","evil":"x","prefix":7}`, false,
			[]string{fieldSelection, fieldTextFragment},
			"https://beta.example.com/post#:~:text=a%2Db%2C%20c%26d",
		},
		{"missing selection", `{"prefix":"x"}`, true, nil, ""},
		{"blank selection", `{"selection":"  "}`, true, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reports, err := decodeTypo(t, tc.body)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidReport) {
					t.Fatalf("err = %v, want ErrInvalidReport", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			got := reports[0]
			if got.Domain != DomainContent || got.Severity() != sevInfo {
				t.Errorf("domain/severity = %s/%s, want content/INFO", got.Domain, got.Severity())
			}
			if len(got.Body) != len(tc.wantKeys) {
				t.Errorf("body = %v, want keys %v", got.Body, tc.wantKeys)
			}
			for _, key := range tc.wantKeys {
				if _, ok := got.Body[key]; !ok {
					t.Errorf("body lacks %q: %v", key, got.Body)
				}
			}
			if got.Body[fieldTextFragment] != tc.fragment {
				t.Errorf("text_fragment = %v, want %s", got.Body[fieldTextFragment], tc.fragment)
			}
		})
	}
}

func TestTypoFieldsAreClipped(t *testing.T) {
	long := strings.Repeat("я", maxTextBytes)
	reports, err := decodeTypo(t, `{"selection":"`+long+`","comment":"`+long+`"}`)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for _, key := range []string{fieldSelection, fieldComment} {
		if got := len(reports[0].Body[key].(string)); got > maxTextBytes {
			t.Errorf("%s = %d bytes, want <= %d", key, got, maxTextBytes)
		}
	}
}

func TestTypoKeepsAnExistingFragment(t *testing.T) {
	if got := textFragment("https://beta.example.com/#top", map[string]any{fieldSelection: "x"}); got != "https://beta.example.com/#top:~:text=x" {
		t.Errorf("textFragment = %s", got)
	}
}

func TestDisabledTypoIsRefusedAsDisabled(t *testing.T) {
	RegisterTypo(false)
	t.Cleanup(func() { RegisterTypo(true) })
	if _, err := decodeTypo(t, `{"selection":"tpyo"}`); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}
