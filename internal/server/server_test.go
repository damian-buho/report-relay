// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/intake"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

// recorder is the in-process log exporter a test inspects: it keeps every
// record the batch processor delivered, so an assertion is on the record a
// report became rather than on what the service logged.
type recorder struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (r *recorder) Export(ctx context.Context, batch []sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, batch...)
	return ctx.Err()
}

func (r *recorder) Shutdown(context.Context) error { return nil }

func (r *recorder) ForceFlush(context.Context) error { return nil }

func (r *recorder) Records() []sdklog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.records)
}

func testConfig() config.Config {
	return config.Config{
		ServiceName:    "report-relay",
		Namespace:      "me.dbuho",
		HTTPPort:       "8080",
		AdminPort:      "8081",
		MaxBodyBytes:   65536,
		MaxJSONDepth:   32,
		MaxArrayItems:  512,
		RateLimitRPS:   1000,
		RateLimitBurst: 1000,
		QueueSize:      8,
		BatchTimeout:   5 * time.Millisecond,
		ExportTimeout:  time.Second,
		ReportingAPIOn: true,
		CSPOn:          true,
		TLSRPTOn:       true,
	}
}

// testSink is the in-process pair a test inspects: records for the log
// pipeline, a manual reader for the metric pipeline.
type testSink struct {
	logs    *recorder
	metrics *sdkmetric.ManualReader
}

// Records flushes the batch processor first: the intake answers before the
// export happens, so a test that wants the records has to ask for the flush.
func (s *testSink) Records(t *testing.T, em *telemetry.Emitter) []sdklog.Record {
	t.Helper()
	if err := em.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return s.logs.Records()
}

func newTestEmitter(t *testing.T, cfg config.Config) (*telemetry.Emitter, *testSink) {
	t.Helper()
	sink := &testSink{
		logs:    &recorder{},
		metrics: sdkmetric.NewManualReader(),
	}
	em, err := telemetry.NewWithExporters(cfg, sink.logs, sink.metrics)
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	t.Cleanup(func() {
		if err := em.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return em, sink
}

func testServer(t *testing.T, cfg config.Config) (*Server, *testSink, *telemetry.Emitter) {
	t.Helper()
	emitter, sink := newTestEmitter(t, cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, log, emitter, func() bool { return true }), sink, emitter
}

// oneReport is a Reporting API batch of one, the smallest body a rate-limit or
// queue test needs to get past the decoder.
const oneReport = `[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/"}}]`

func post(t *testing.T, handler http.Handler, mediaType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", mediaType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestEmitSetsEventNameAsAnAttributeNotOnlyAField(t *testing.T) {
	// The collector promotes `event.name` and `event.domain` to Loki labels by
	// reading ATTRIBUTES. The first-class EventName field alone leaves the
	// report type out of the label set, and every query filtering on it returns
	// nothing — so the attribute is load-bearing, not decoration.
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/"}}]`
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].EventName() != "deprecation" {
		t.Errorf("EventName field = %q, want deprecation", records[0].EventName())
	}
	assertAttribute(t, records[0], "event.name", "deprecation")
	assertAttribute(t, records[0], "event.domain", "browser")
}

func TestLegacyCSPPostBecomesOneRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `{"csp-report":{"document-uri":"https://beta.dbuho.me/","violated-directive":"script-src",` +
		`"blocked-uri":"https://evil.example/x.js","effective-directive":"script-src"}}`
	rec := post(t, srv.IntakeHandler(), intake.MediaCSPReport, body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", rec.Code, rec.Body)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want one per report", len(records))
	}
	record := records[0]
	if record.EventName() != "csp-violation" {
		t.Errorf("event.name = %q, want csp-violation", record.EventName())
	}
	assertAttribute(t, record, "event.domain", "browser")
	assertAttribute(t, record, "report.source", "csp")
	assertAttribute(t, record, "report.url_host", "beta.dbuho.me")
	assertAttribute(t, record, "report.effectiveDirective", "script-src")
}

func TestReportingAPIBatchOfThreeBecomesThreeRecords(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `[
	  {"type":"csp-violation","age":5,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/","effectiveDirective":"script-src","blockedURL":"https://evil.example/x.js"}},
	  {"type":"deprecation","age":60,"url":"https://beta.dbuho.me/legacy","body":{"documentURL":"https://beta.dbuho.me/legacy"}},
	  {"type":"network-error","age":120,"url":"https://beta.dbuho.me/api","body":{"documentURL":"https://beta.dbuho.me/api","phase":"dns"}}
	]`
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	if records[2].SeverityText() != "WARN" {
		t.Errorf("network-error severity = %q, want WARN", records[2].SeverityText())
	}
	if records[1].SeverityText() != "INFO" {
		t.Errorf("deprecation severity = %q, want INFO", records[1].SeverityText())
	}
}

func TestUnknownReportTypeReachesTheRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `[{"type":"certificate-transparency","age":7,"url":"https://beta.dbuho.me/",` +
		`"body":{"ct-policy":"scts"}}]`
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "report.type", "certificate-transparency")
	assertAttribute(t, records[0], "report.ct-policy", "scts")
}

func TestTLSRPTPostBecomesOneRecordPerResultType(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me","report-id":"r1",` +
		`"result-type":"aggregate","failure-info":{"result-type":"aggregate","failing-sessions":{"expired":3}}}`
	rec := post(t, srv.IntakeHandler(), intake.MediaTLSRPTJSON, body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "event.domain", "mail")
}

func TestGzippedTLSRPTPostIsAccepted(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	raw := `{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me","report-id":"r1",` +
		`"result-type":"aggregate","failure-info":{"result-type":"aggregate","failing-sessions":{"expired":1}}}`
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(raw)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", intake.MediaTLSRPTGzip)
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.IntakeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := len(sink.Records(t, sinkEmitter)); got != 1 {
		t.Fatalf("records = %d, want 1", got)
	}
}

func TestUnknownContentTypeIsRefused(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	rec := post(t, srv.IntakeHandler(), "text/plain", "not a report")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestDisabledIntakeAnswersServiceUnavailable(t *testing.T) {
	cfg := testConfig()
	cfg.TLSRPTOn = false
	srv, _, _ := testServer(t, cfg)
	rec := post(t, srv.IntakeHandler(), intake.MediaTLSRPTJSON,
		`{"organization-name":"dbuho.me","failure-info":{"result-type":"aggregate"}}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestRateLimitTrips(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1
	srv, _, _ := testServer(t, cfg)
	handler := srv.IntakeHandler()
	if rec := post(t, handler, intake.MediaReportingAPI, oneReport); rec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec.Code)
	}
	if rec := post(t, handler, intake.MediaReportingAPI, oneReport); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec.Code)
	}
}

func TestOversizeBodyIsRefused(t *testing.T) {
	cfg := testConfig()
	cfg.MaxBodyBytes = 128
	srv, _, _ := testServer(t, cfg)
	fat := `[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"documentURL":"` +
		strings.Repeat("a", 512) + `"}}]`
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, fat)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestBadJSONIsRefused(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, `[{"type":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSchemaViolationIsRefused(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI,
		`[{"type":"csp-violation","age":1,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/"}}]`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestCORSPreflightIsAnswered(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://beta.dbuho.me")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()
	srv.IntakeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://beta.dbuho.me" {
		t.Errorf("Allow-Origin = %q, want the requesting origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("Allow-Methods = %q, want POST", got)
	}
}

func TestCORSAllowListNarrowsThePreflight(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedOrigins = []string{"https://allowed.example"}
	srv, _, _ := testServer(t, cfg)
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://denied.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	srv.IntakeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for an origin outside the allow-list", rec.Code)
	}
}

func TestAdminEndpoints(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	for path, want := range map[string]int{"/healthz": http.StatusOK, "/readyz": http.StatusOK} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.AdminHandler().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestReadyzReportsAnUnreachableExporter(t *testing.T) {
	emitter, _ := newTestEmitter(t, testConfig())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(testConfig(), log, emitter, func() bool { return false })
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.AdminHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func assertAttribute(t *testing.T, record sdklog.Record, key, want string) {
	t.Helper()
	var found bool
	record.WalkAttributes(func(attr attribute.KeyValue) bool {
		if string(attr.Key) != key {
			return true
		}
		found = true
		if attr.Value.AsString() != want {
			t.Errorf("%s = %q, want %q", key, attr.Value.AsString(), want)
		}
		return false
	})
	if !found {
		t.Errorf("attribute %q is absent", key)
	}
}

// blockingRecorder never accepts an export, so the batch queue fills and the
// processor has to drop rather than block the intake. The count is atomic
// because the batch processor calls Export on its own goroutine.
type blockingRecorder struct{ calls atomic.Int64 }

func (b *blockingRecorder) Export(context.Context, []sdklog.Record) error {
	b.calls.Add(1)
	return errors.New("collector unreachable")
}

func (b *blockingRecorder) Shutdown(context.Context) error { return nil }

func (b *blockingRecorder) ForceFlush(context.Context) error { return nil }

func TestAFullQueueDropsWithoutBlockingIntake(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 1
	cfg.BatchTimeout = time.Hour
	sink := &blockingRecorder{}
	em, err := telemetry.NewWithExporters(cfg, sink, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, log, em, func() bool { return true })
	handler := srv.IntakeHandler()
	// Every request is answered 204 even though nothing can be exported: the
	// queue is bounded and drops, so a dead collector never slows a browser.
	for i := range 50 {
		if rec := post(t, handler, intake.MediaReportingAPI, oneReport); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d answered %d, want 204 while the queue is full", i, rec.Code)
		}
	}
}

func TestExportFailureNeverBlocksIntake(t *testing.T) {
	cfg := testConfig()
	cfg.BatchTimeout = time.Millisecond
	sink := &blockingRecorder{}
	em, err := telemetry.NewWithExporters(cfg, sink, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, log, em, func() bool { return true })
	for range 5 {
		if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, oneReport); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 with a dead collector", rec.Code)
		}
	}
	// The drain is what makes the failed export observable; the intake above
	// already answered without waiting for it.
	if err := em.Flush(context.Background()); err == nil {
		t.Error("Flush reported success against an exporter that fails every call")
	}
	if sink.calls.Load() == 0 {
		t.Error("the exporter was never called, so the failure path was not exercised")
	}
}
