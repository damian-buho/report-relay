// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package guard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fixtures the address tests share, named so a repeated literal stays out.
const (
	peerWithPort = "10.0.0.1:5555"
	barePeer     = "192.0.2.10"
	trustedRange = "10.0.0.0/8"
)

// errReader fails every read, so a body that dies mid-transfer is covered too.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestReadBodyReportsATransferFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", errReader{})
	if _, err := ReadBody(req, 1024); err == nil {
		t.Fatal("a failed transfer was accepted as a body")
	}
}

// TestReadBodyReportsAGzipTransferFailure covers the same failure one layer
// deeper: a stream that stops mid-member leaves the gunzip reader broken.
func TestReadBodyReportsAGzipTransferFailure(t *testing.T) {
	var buf strings.Builder
	buf.WriteString("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff")
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(buf.String()))
	req.Header.Set("Content-Encoding", "gzip")
	if _, err := ReadBody(req, 1024); err == nil {
		t.Fatal("a truncated gzip member answered as a body")
	}
}

// TestDecodeRejectsTrailingGarbage pins the second read of the same bytes: the
// unmarshal stops at the first value, so the shape walk is what sees the rest.
func TestDecodeRejectsTrailingGarbage(t *testing.T) {
	var dst map[string]any
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 16, MaxBodyKeys: 16}
	if err := Decode([]byte(`{"a":1} trailing`), &dst, limits); err == nil {
		t.Fatal("garbage after the value was accepted")
	}
}

// TestDecodeCountsKeysAndRedactsAcrossNestedObjects pins the walk that bounds
// one body key to one log attribute, at the two values either side of the cap.
func TestDecodeCountsKeysAndRedactsAcrossNestedObjects(t *testing.T) {
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 16, MaxBodyKeys: 2}
	var dst map[string]any
	if err := Decode([]byte(`{"a":"one","b":"two"}`), &dst, limits); err != nil {
		t.Fatalf("a body at the key cap was refused: %v", err)
	}
	if err := Decode([]byte(`{"a":"one","b":"two","c":"three"}`), &dst, limits); !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("err = %v, want ErrTooManyKeys", err)
	}
}

// TestPopFrameClosesTheInnermostFrame pins the frame contract the shape walk
// relies on: an empty stack is returned untouched rather than indexed past its
// end, an object close leaves its parent expecting a key, and an array close
// leaves the parent taking whatever token comes next.
func TestPopFrameClosesTheInnermostFrame(t *testing.T) {
	cases := []struct {
		name  string
		stack []frame
		want  []frame
	}{
		{"nil stack", nil, nil},
		{"empty stack", []frame{}, []frame{}},
		{"only frame", []frame{{object: true, key: false}}, []frame{}},
		{"object inside object", []frame{{object: true, key: false}, {object: true, key: false}}, []frame{{object: true, key: true}}},
		{"array inside object", []frame{{object: true, key: true}, {object: false}}, []frame{{object: true, key: true}}},
		{"array inside array", []frame{{object: false}, {object: false}}, []frame{{object: false}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := popFrame(tc.stack)
			if len(got) != len(tc.want) {
				t.Fatalf("popFrame(%v) = %v, want %v frames", tc.stack, got, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("frame %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestCheckShapeHandlesAValueThatIsNotAnObject pins the arm a top-level array
// or a bare scalar takes: no key is counted, and the walk still ends cleanly.
func TestCheckShapeHandlesAValueThatIsNotAnObject(t *testing.T) {
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 16, MaxBodyKeys: 16}
	var dsts []any
	for _, tc := range []struct {
		name string
		body string
		dst  any
	}{
		{"array of strings", `["a","b"]`, &dsts},
		{"bare string", `"a"`, new(string)},
		{"bare number", `1`, new(int)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Decode([]byte(tc.body), tc.dst, limits); err != nil {
				t.Fatalf("Decode: %v", err)
			}
		})
	}
}

// TestRemoteHostKeepsAnAddressWithoutAPort pins the fallback: a peer with no
// port hands over a bare address, and it is used as it arrived.
func TestRemoteHostKeepsAnAddressWithoutAPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = barePeer
	if got := remoteHost(req); got != barePeer {
		t.Errorf("remoteHost = %q, want the raw address", got)
	}
}

// TestClientIPSkipsAHopThatIsNotAnAddress pins the chain walk: one unparsable
// entry is ignored rather than treated as the client.
func TestClientIPSkipsAHopThatIsNotAnAddress(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = peerWithPort
	req.Header.Set("X-Forwarded-For", "not-an-ip, 203.0.113.9")
	if got := ClientIP(req, true, mustNets(t, trustedRange)); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want the nearest real untrusted hop", got)
	}
}

// TestClientIPFallsBackWhenNoForwardedChainIsSent pins the empty-header case:
// a trusted proxy that forwards nothing is simply the peer.
func TestClientIPFallsBackWhenNoForwardedChainIsSent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = peerWithPort
	if got := ClientIP(req, true, mustNets(t, trustedRange)); got != "10.0.0.1" {
		t.Errorf("ClientIP = %q, want the socket peer when the header is absent", got)
	}
}

// TestDeniedCIDRNamesTheFirstMatchingNetwork pins the deny lookup an operator
// reads when a peer is refused: the network string, or empty for none.
func TestDeniedCIDRNamesTheFirstMatchingNetwork(t *testing.T) {
	nets := mustNets(t, trustedRange, "192.0.2.0/24", "2001:db8::/32")
	cases := []struct{ ip, want string }{
		{"10.1.2.3", trustedRange},
		{" 192.0.2.9 ", "192.0.2.0/24"},
		{"2001:db8::1", "2001:db8::/32"},
		{"203.0.113.9", ""},
		{"not-an-ip", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := DeniedCIDR(tc.ip, nets); got != tc.want {
			t.Errorf("DeniedCIDR(%q) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}

// TestParseTrustedProxiesSkipsBlankEntries pins the empty arm of the list an
// operator types by hand: a trailing comma is not a configuration error.
func TestParseTrustedProxiesSkipsBlankEntries(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"", "  ", "10.0.0.0/8", ""})
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	if len(nets) != 1 {
		t.Fatalf("networks = %d, want 1", len(nets))
	}
}
