// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package guard

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadBodyRejectsOversizeBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 2048)))
	_, err := ReadBody(req, 1024)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestReadBodyAcceptsBodyAtTheCap(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 1024)))
	body, err := ReadBody(req, 1024)
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	if len(body) != 1024 {
		t.Errorf("len = %d, want 1024", len(body))
	}
}

func TestReadBodyBoundsTheDecompressedSize(t *testing.T) {
	// A body that compresses to a few hundred bytes and expands past the cap must
	// be rejected: the cap applies to what is parsed, not to what is transferred.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(strings.Repeat("a", 1<<20))); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if buf.Len() > 2048 {
		t.Fatalf("the fixture did not compress: %d bytes", buf.Len())
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	if _, err := ReadBody(req, 1024); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge for a gzip bomb", err)
	}
}

func TestReadBodyRejectsBrokenGzip(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not gzip at all"))
	req.Header.Set("Content-Encoding", "gzip")
	if _, err := ReadBody(req, 1024); err == nil {
		t.Fatal("a broken gzip body was accepted")
	}
}

func TestDecodeRejectsDeepNesting(t *testing.T) {
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 16}
	body := []byte(strings.Repeat(`{"a":`, 20) + `1` + strings.Repeat(`}`, 20))
	var dst map[string]any
	if err := Decode(body, &dst, limits); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("err = %v, want ErrTooDeep", err)
	}
}

func TestDecodeAcceptsShallowNesting(t *testing.T) {
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 16}
	var dst map[string]any
	if err := Decode([]byte(`{"a":{"b":{"c":1}}}`), &dst, limits); err != nil {
		t.Fatalf("Decode: %v", err)
	}
}

func TestDecodeRejectsBadJSON(t *testing.T) {
	var dst map[string]any
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 16}
	if err := Decode([]byte(`{"a":`), &dst, limits); err == nil {
		t.Fatal("truncated JSON was accepted")
	}
}

func TestCountItemsEnforcesTheArrayCap(t *testing.T) {
	limits := Limits{MaxBodyBytes: 1 << 20, MaxJSONDepth: 8, MaxArrayItems: 3}
	if _, err := CountItems([]byte(`[1,2,3,4,5]`), limits); !errors.Is(err, ErrArrayTooLong) {
		t.Fatalf("err = %v, want ErrArrayTooLong", err)
	}
	count, err := CountItems([]byte(`[1,2,3]`), limits)
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestLimiterTripsAfterTheBurst(t *testing.T) {
	limiter := NewLimiter(1, 3)
	for i := range 3 {
		if !limiter.Allow("203.0.113.1") {
			t.Fatalf("request %d of the burst was refused", i+1)
		}
	}
	if limiter.Allow("203.0.113.1") {
		t.Fatal("the fourth request in the same instant was allowed")
	}
	if !limiter.Allow("203.0.113.2") {
		t.Fatal("a different client was refused: the bucket is per address")
	}
}

func TestLimiterEvictsIdleClients(t *testing.T) {
	limiter := NewLimiter(1000, 2)
	limiter.ttl = time.Millisecond
	for range 10 {
		limiter.Allow("198.51.100.7")
	}
	time.Sleep(5 * time.Millisecond)
	limiter.ops = evictEvery - 1
	if !limiter.Allow("198.51.100.7") {
		t.Fatal("an idle client was refused after its bucket refilled")
	}
	if limiter.Clients() > 1 {
		t.Errorf("clients = %d, want the idle entries evicted", limiter.Clients())
	}
}

func TestClientIPIgnoresForwardedHeaderByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.0.2.10:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(req, false); got != "192.0.2.10" {
		t.Errorf("ClientIP = %q, want the socket address when the proxy is not trusted", got)
	}
	if got := ClientIP(req, true); got != "1.2.3.4" {
		t.Errorf("ClientIP = %q, want the first forwarded hop when the proxy is trusted", got)
	}
}

func TestClientIPUsesTheFirstForwardedHop(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.0.2.10:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 9.10.11.12")
	if got := ClientIP(req, true); got != "1.2.3.4" {
		t.Errorf("ClientIP = %q, want 1.2.3.4", got)
	}
}

func TestLimiterBoundsTheTable(t *testing.T) {
	limiter := NewLimiter(1000, 1000)
	for i := range maxClients + 500 {
		limiter.Allow("10.1." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256))
	}
	if got := limiter.Clients(); got > maxClients {
		t.Errorf("clients = %d, want at most %d", got, maxClients)
	}
}

func TestLimiterClampsNonsense(t *testing.T) {
	limiter := NewLimiter(-5, -2)
	if limiter.burst != 1 {
		t.Errorf("burst = %d, want the clamp to 1", limiter.burst)
	}
	if !limiter.Allow("203.0.113.9") {
		t.Fatal("a clamped limiter refused the burst token")
	}
	if limiter.Allow("203.0.113.9") {
		t.Fatal("a clamped limiter admitted past its burst: negative rates must fail closed")
	}
}

func TestIsGzipMatchesAmongCodings(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Encoding", "gzip, br")
	if !isGzip(req) {
		t.Error("a body coded gzip among others was not gunzipped")
	}
}
