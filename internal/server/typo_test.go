// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/guard"
	"kiota.ch/damian-buho/report-relay/internal/intake"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

// typoReport is one typo report, the smallest body that reaches every typo guard.
const typoReport = `[{"type":"typo","url":"https://beta.example.com/post","body":{"selection":"tpyo"}}]`

// typoConfig turns the dedup and the stricter typo budget on: defaults in
// config.Load do that, but the zero value of a bare Config does not.
func typoConfig() config.Config {
	cfg := testConfig()
	cfg.TypoRateLimitBurst = 100
	cfg.TypoDedupOn = true
	cfg.TypoDedupSize = 64
	cfg.TypoDedupTTL = time.Minute
	return cfg
}

func TestTypoReportBecomesOneRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, typoConfig())
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, typoReport); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "event.name", "typo")
	assertAttribute(t, records[0], "event.domain", "content")
	assertAttribute(t, records[0], "report.text_fragment", "https://beta.example.com/post#:~:text=tpyo")
}

func TestTypoBatchKeepsTheOtherReportsBesideItsGuards(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoCommentMaxURLs = 2
	srv, sink, sinkEmitter := testServer(t, cfg)
	body := `[
	  {"type":"typo","url":"https://beta.example.com/post","body":{"selection":"tpyo"}},
	  {"type":"deprecation","age":1,"url":"https://beta.example.com/","body":{"id":"websql","message":"WebSQL is deprecated"}}
	]`
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2: the URL cap is a typo guard alone", len(records))
	}
}

// TestTypoCommentFullOfURLsIsRefused pins the link-farm guard: a comment is
// where a human writes, and a URL count past the config is not a report.
func TestTypoCommentFullOfURLsIsRefused(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoCommentMaxURLs = 1
	srv, _, _ := testServer(t, cfg)
	body := `[{"type":"typo","url":"https://beta.example.com/post","body":{"selection":"tpyo",` +
		`"comment":"see https://a.example and https://b.example and https://c.example"}}]`
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a comment past the URL cap", rec.Code)
	}
}

func TestTypoCommentAtTheURLCapIsAccepted(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoCommentMaxURLs = 2
	srv, _, _ := testServer(t, cfg)
	body := `[{"type":"typo","url":"https://beta.example.com/post","body":{"selection":"tpyo",` +
		`"comment":"see https://a.example and https://b.example"}}]`
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 at the URL cap", rec.Code)
	}
}

func TestTypoCommentMaxURLsReadsTheFallbackFields(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoCommentMaxURLs = 3
	cfg.TypoMaxCommentURLs = 5
	cfg.TypoMaxURLs = 9
	if got := typoCommentMaxURLs(cfg); got != 3 {
		t.Errorf("maxURLs = %d, want the newest field to win", got)
	}
	cfg.TypoCommentMaxURLs = 0
	if got := typoCommentMaxURLs(cfg); got != 5 {
		t.Errorf("maxURLs = %d, want the second field when the first is unset", got)
	}
	cfg.TypoMaxCommentURLs = 0
	if got := typoCommentMaxURLs(cfg); got != 9 {
		t.Errorf("maxURLs = %d, want the oldest field when both others are unset", got)
	}
	cfg.TypoMaxURLs = 0
	if got := typoCommentMaxURLs(cfg); got != 0 {
		t.Errorf("maxURLs = %d, want the uncapped value when every field is unset", got)
	}
}

func TestCountURLsCountsBothSchemes(t *testing.T) {
	cases := map[string]int{
		"":                       0,
		"no links here":          0,
		"http://a.example":       1,
		"HTTPS://A.EXAMPLE":      1,
		"a http://x b https://y": 2,
	}
	for comment, want := range cases {
		if got := countURLs(comment); got != want {
			t.Errorf("countURLs(%q) = %d, want %d", comment, got, want)
		}
	}
}

func TestTypoDedupKeySeparatesTheTypoLocation(t *testing.T) {
	base := `[{"type":"typo","url":"%s","body":{"selection":"s","prefix":"p","suffix":"x"}}]`
	keyOf := func(t *testing.T, rawURL string) string {
		t.Helper()
		reports, err := intake.Decode(intake.MediaReportingAPI, []byte(strings.Replace(base, "%s", rawURL, 1)),
			limitsForTests(), false)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return typoDedupKey(reports[0])
	}
	same := keyOf(t, "https://beta.example.com/post?a=1")
	other := keyOf(t, "https://beta.example.com/other?a=1")
	if same == other {
		t.Error("a different path produced the same key, so distinct typos collapse")
	}
	if again := keyOf(t, "https://beta.example.com/post?a=2"); again != same {
		t.Errorf("key changed with the query string: %q vs %q", again, same)
	}
}

// limitsForTests is the guard cap set one typo decoder pass needs.
func limitsForTests() guard.Limits {
	return guard.Limits{
		MaxBodyBytes:  testConfig().MaxBodyBytes,
		MaxJSONDepth:  testConfig().MaxJSONDepth,
		MaxArrayItems: testConfig().MaxArrayItems,
		MaxBodyKeys:   testConfig().MaxBodyKeys,
	}
}

func TestTypoRateLimitTripsOnItsOwnBudget(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoRateLimitBurst = 1
	srv, _, _ := testServer(t, cfg)
	handler := srv.IntakeHandler()
	if rec := post(t, handler, intake.MediaReportingAPI, typoReport); rec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec.Code)
	}
	rec := post(t, handler, intake.MediaReportingAPI, typoReport)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429 from the typo budget", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 without Retry-After leaves the client guessing")
	}
}

// TestTypoLimiterFallsBackToThePerHourRate pins the derived budget: an
// operator who names a per-hour allowance gets a per-second token bucket.
func TestTypoLimiterFallsBackToThePerHourRate(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoRateLimitRPS = 0
	cfg.TypoRateLimitBurst = 1
	cfg.TypoRateLimitPerHour = 3600
	srv := New(cfg, testLoggerFor(t), nil, nil)
	t.Cleanup(srv.Close)
	if srv.typoLimiter == nil {
		t.Fatal("no typo limiter was built, so the per-hour fallback never ran")
	}
}

func testLoggerFor(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTypoDedupIsBuiltOnlyWhenConfigured(t *testing.T) {
	on := New(typoConfig(), testLoggerFor(t), nil, nil)
	t.Cleanup(on.Close)
	if on.typoDedup == nil {
		t.Error("dedup is on in the config, so it must be built")
	}
	off := testConfig()
	off.TypoDedupOn = false
	srv := New(off, testLoggerFor(t), nil, nil)
	t.Cleanup(srv.Close)
	if srv.typoDedup != nil {
		t.Error("a disabled dedup must stay nil, so the intake skips the branch")
	}
	zero := typoConfig()
	zero.TypoDedupSize = 0
	zeroSrv := New(zero, testLoggerFor(t), nil, nil)
	t.Cleanup(zeroSrv.Close)
	if zeroSrv.typoDedup != nil {
		t.Error("a zero size must not build a cache")
	}
	noTTL := typoConfig()
	noTTL.TypoDedupTTL = 0
	ttlSrv := New(noTTL, testLoggerFor(t), nil, nil)
	t.Cleanup(ttlSrv.Close)
	if ttlSrv.typoDedup == nil {
		t.Error("a zero TTL must fall back to the default, not disable the cache")
	}
}

// TestDuplicateTypoReportsCollapseToOneRecord pins the bounded TTL cache: the
// same page and selection twice is one typo, not two inbox rows.
func TestDuplicateTypoReportsCollapseToOneRecord(t *testing.T) {
	cfg := typoConfig()
	cfg.TypoCommentMaxURLs = 0
	srv, sink, sinkEmitter := testServer(t, cfg)
	handler := srv.IntakeHandler()
	for range 3 {
		if rec := post(t, handler, intake.MediaReportingAPI, typoReport); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 for a duplicate too", rec.Code)
		}
	}
	if got := len(sink.Records(t, sinkEmitter)); got != 1 {
		t.Errorf("records = %d, want the duplicates collapsed to one", got)
	}
	if got := droppedByReason(t, sink.metrics, telemetry.ReasonDuplicate); got != 2 {
		t.Errorf("duplicate drops = %d, want one per collapsed report", got)
	}
}

func TestOtherMethodsOnTheIntakeAreRefused(t *testing.T) {
	srv, _, _ := testServer(t, typoConfig())
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/", nil)
		rec := httptest.NewRecorder()
		srv.IntakeHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
	}
}

// TestTypoURLOutsideTheRequestOriginIsRefused pins the anti-flood check: the
// page a typo names must belong to the site that sent it.
func TestTypoURLOutsideTheRequestOriginIsRefused(t *testing.T) {
	cfg := typoConfig()
	cfg.AllowedOrigins = []string{"https://beta.example.com"}
	srv, _, _ := testServer(t, cfg)
	body := `[{"type":"typo","url":"https://evil.example/post","body":{"selection":"tpyo"}}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", intake.MediaReportingAPI)
	req.Header.Set("Origin", "https://beta.example.com")
	rec := httptest.NewRecorder()
	srv.IntakeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a typo about another origin", rec.Code)
	}
}

func TestTypoURLWithoutAHostIsRefusedByTheDecoder(t *testing.T) {
	srv, _, _ := testServer(t, typoConfig())
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI,
		`[{"type":"typo","url":"javascript:alert(1)","body":{"selection":"tpyo"}}]`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a non-http typo url", rec.Code)
	}
}

// TestEmptyReportingAPIBatchIsUnsupported pins the zero-report answer: an
// empty array is a sender bug, and it is answered 422 rather than 204.
func TestEmptyReportingAPIBatchIsUnsupported(t *testing.T) {
	srv, _, _ := testServer(t, typoConfig())
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, `[]`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for an empty batch", rec.Code)
	}
}
