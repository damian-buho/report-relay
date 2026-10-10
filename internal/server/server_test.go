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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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
		MaxBodyKeys:    512,
		RateLimitRPS:   1000,
		RateLimitBurst: 1000,
		QueueSize:      8,
		BatchTimeout:   5 * time.Millisecond,
		ExportTimeout:  time.Second,
		ReportingAPIOn: true,
		CSPOn:          true,
		TLSRPTOn:       true,
		ExpectCTOn:     true,
		HPKPOn:         true,
		IODEFOn:        true,
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
const oneReport = `[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}}]`

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
	body := `[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}}]`
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
	  {"type":"deprecation","age":60,"url":"https://beta.dbuho.me/legacy","body":{"id":"websql","message":"WebSQL is deprecated"}},
	  {"type":"network-error","age":120,"url":"https://beta.dbuho.me/api","body":{"phase":"dns","type":"dns.address_changed","method":"GET","protocol":"http/1.1","referrer":"https://beta.dbuho.me/","sampling-fraction":1.0,"server-ip":"93.184.216.34","status-code":0,"elapsed-time":12}}
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

// gateExporter blocks every export until release is closed, so the batch
// processor never frees a queue slot mid-test and the gate trips on schedule.
type gateExporter struct{ release chan struct{} }

func (g *gateExporter) Export(ctx context.Context, _ []sdklog.Record) error {
	select {
	case <-g.release:
		return errors.New("collector unreachable")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *gateExporter) Shutdown(context.Context) error { return nil }

func (g *gateExporter) ForceFlush(context.Context) error { return nil }

// gatedRecorder blocks every export until released and then keeps what it was given, so a test can prove which records actually left the queue.
type gatedRecorder struct {
	release chan struct{}
	mu      sync.Mutex
	records []sdklog.Record
}

func (g *gatedRecorder) Export(ctx context.Context, batch []sdklog.Record) error {
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.records = append(g.records, batch...)
	return nil
}

func (g *gatedRecorder) Shutdown(context.Context) error   { return nil }
func (g *gatedRecorder) ForceFlush(context.Context) error { return nil }

func (g *gatedRecorder) Records() []sdklog.Record {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.records)
}

func TestAFullQueueSignalsBackpressure(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 2
	cfg.BatchTimeout = time.Hour
	gate := &gateExporter{release: make(chan struct{})}
	reader := sdkmetric.NewManualReader()
	em, err := telemetry.NewWithExporters(cfg, gate, reader)
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, log, em, func() bool { return true })
	handler := srv.IntakeHandler()
	// The first requests take the two queue slots; everything past them is
	// answered 429, so the sender retries instead of assuming delivery.
	saw429 := false
	for i := range 51 {
		rec := post(t, handler, intake.MediaReportingAPI, oneReport)
		switch rec.Code {
		case http.StatusNoContent:
		case http.StatusTooManyRequests:
			saw429 = true
			if rec.Header().Get("Retry-After") == "" {
				t.Errorf("request %d: a 429 without Retry-After leaves the client guessing", i)
			}
		default:
			t.Fatalf("request %d answered %d, want 204 or 429", i, rec.Code)
		}
	}
	if !saw429 {
		t.Error("no request was answered 429 while the exporter was blocked")
	}
	if got := droppedByReason(t, reader, telemetry.ReasonQueueFull); got == 0 {
		t.Error("no queue-full drop was counted while the exporter was blocked")
	}
	close(gate.release)
}

// fiveReports is a Reporting API batch one report past a two-slot queue, so a refusal covers the whole batch or the sender's retry delivers the prefix twice.
const fiveReports = `[
  {"type":"csp-violation","age":1,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/","effectiveDirective":"script-src","blockedURL":"https://evil.example/x.js"}},
  {"type":"deprecation","age":2,"url":"https://beta.dbuho.me/legacy","body":{"id":"websql","message":"WebSQL is deprecated"}},
  {"type":"network-error","age":3,"url":"https://beta.dbuho.me/api","body":{"phase":"dns","type":"dns.address_changed"}},
  {"type":"coop","age":4,"url":"https://beta.dbuho.me/","body":{"disposition":"enforce","effectivePolicy":"same-origin-allow-popups","type":"navigation-to-response"}},
  {"type":"integrity-violation","age":5,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/","blockedURL":"https://evil.example/x.js"}}
]`

func TestABatchTheQueueCannotTakeIsRefusedWhole(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 2
	cfg.BatchTimeout = time.Hour
	sink := &gatedRecorder{release: make(chan struct{})}
	reader := sdkmetric.NewManualReader()
	em, err := telemetry.NewWithExporters(cfg, sink, reader)
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, log, em, func() bool { return true })
	// Two slots for a five-report batch: the refusal must cover the batch the sender is about to retry.
	rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, fiveReports)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 for a batch past the queue", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 without Retry-After leaves the client guessing")
	}
	if got := droppedByReason(t, reader, telemetry.ReasonQueueFull); got != 5 {
		t.Errorf("queue-full drops = %d, want one per report in the batch", got)
	}
	if got := counterTotal(t, reader, "report.relay.accepted", nil); got != 0 {
		t.Errorf("accepted = %d, want nothing counted as queued", got)
	}
	// Nothing may stay enqueued, and the exporter is the proof: only a queued record ever reaches it.
	close(sink.release)
	if err := em.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := len(sink.Records()); got != 0 {
		t.Errorf("exporter received %d records, want none of the refused batch", got)
	}
}

func TestABatchThatFitsIsAcceptedWhole(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, fiveReports); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a batch the queue takes", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 5 {
		t.Fatalf("records = %d, want 5", len(records))
	}
	if records[0].EventName() != "csp-violation" || records[4].EventName() != "integrity-violation" {
		t.Errorf("first = %q, last = %q, want the batch order kept", records[0].EventName(), records[4].EventName())
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

func TestUnsupportedContentTypeCostsARateToken(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1
	srv, _, _ := testServer(t, cfg)
	handler := srv.IntakeHandler()
	if rec := post(t, handler, "text/plain", "not a report"); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("first status = %d, want 415", rec.Code)
	}
	if rec := post(t, handler, "text/plain", "not a report"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429: garbage must not bypass the limiter", rec.Code)
	}
}

// preflight sends the OPTIONS request a browser sends before a cross-origin POST.
func preflight(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://beta.dbuho.me")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPreflightCostsARateToken(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1
	srv, _, _ := testServer(t, cfg)
	handler := srv.IntakeHandler()
	if rec := preflight(t, handler); rec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec.Code)
	}
	rec := preflight(t, handler)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429: a preflight must not bypass the limiter", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 without Retry-After leaves the client guessing")
	}
}

// panicHandler stands in for a handler that panics on attacker input.
func panicHandler(http.ResponseWriter, *http.Request) {
	panic("boom")
}

// TestRecoverTurnsAPanickingHandlerIntoA500 pins the wrapper both intake routes run through, so a panic is a 500 rather than a dead process.
func TestRecoverTurnsAPanickingHandlerIntoA500(t *testing.T) {
	cfg := testConfig()
	srv, _, _ := testServer(t, cfg)
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /", srv.recover(panicHandler))
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 from the recover wrapper", rec.Code)
	}
}

func TestRateLimitAnswerCarriesRetryAfter(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1
	srv, _, _ := testServer(t, cfg)
	handler := srv.IntakeHandler()
	if rec := post(t, handler, intake.MediaReportingAPI, oneReport); rec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec.Code)
	}
	rec := post(t, handler, intake.MediaReportingAPI, oneReport)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 without Retry-After leaves the client guessing")
	}
}

func TestOversizeKeyCountIsRefusedAsTooLarge(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	var b strings.Builder
	b.WriteString(`[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"gone"`)
	for i := range 511 {
		b.WriteString(`,"k` + strconv.Itoa(i) + `":"v"`)
	}
	b.WriteString(`}}]`)
	body := b.String()
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for a body past the key cap", rec.Code)
	}
}

func TestOversizeBatchIsRefusedAsTooLarge(t *testing.T) {
	cfg := testConfig()
	cfg.MaxArrayItems = 1
	srv, _, _ := testServer(t, cfg)
	body := `[
	  {"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}},
	  {"type":"deprecation","age":2,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}}
	]`
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for a batch past the array cap", rec.Code)
	}
}

func TestCOOPPostBecomesOneRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `[{"type":"coop","age":3,"url":"https://beta.dbuho.me/",
	  "body":{"disposition":"enforce","effectivePolicy":"same-origin-allow-popups","type":"navigation-to-response"}}]`
	if rec := post(t, srv.IntakeHandler(), intake.MediaReportingAPI, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "event.name", "coop")
	assertAttribute(t, records[0], "report.source", "reporting-api")
}

func TestExpectCTPostBecomesOneRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `{"expect-ct-report":{"date-time":"2026-09-26T00:00:00Z","hostname":"beta.dbuho.me",
	  "port":443,"effective-expiration-date":"2026-10-26T00:00:00Z",
	  "served-certificate-chain":["PEM1"],"validated-certificate-chain":["PEM1"]}}`
	if rec := post(t, srv.IntakeHandler(), intake.MediaExpectCT, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "event.name", "expect-ct")
	assertAttribute(t, records[0], "report.url_host", "beta.dbuho.me")
}

func TestDisabledExpectCTAnswersServiceUnavailable(t *testing.T) {
	cfg := testConfig()
	cfg.ExpectCTOn = false
	srv, _, _ := testServer(t, cfg)
	rec := post(t, srv.IntakeHandler(), intake.MediaExpectCT, `{"expect-ct-report":{"hostname":"beta.dbuho.me"}}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHPKPPostBecomesOneRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `{"date-time":"2026-09-26T00:00:00Z","hostname":"beta.dbuho.me","port":443,
	  "effective-expiration-date":"2026-10-26T00:00:00Z","include-subdomains":false,
	  "noted-hostname":"beta.dbuho.me","served-certificate-chain":["PEM1"],
	  "validated-certificate-chain":["PEM1"],"known-pins":["pin-sha256=\"abcd\""]}`
	if rec := post(t, srv.IntakeHandler(), intake.MediaHPKP, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "event.name", "hpkp")
}

func TestForeignJSONIsNotAPinFailure(t *testing.T) {
	srv, _, _ := testServer(t, testConfig())
	rec := post(t, srv.IntakeHandler(), intake.MediaHPKP, `{"hello":"world"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for JSON outside the pin shape", rec.Code)
	}
}

func TestIODEFPostBecomesOneRecord(t *testing.T) {
	srv, sink, sinkEmitter := testServer(t, testConfig())
	body := `<IODEF-Document version="2.00" xmlns="urn:ietf:params:xml:ns:iodef-2.0">` +
		`<Incident purpose="reporting"><IncidentID name="ca1.example.net">caa-1</IncidentID>` +
		`<Node><NodeName>beta.dbuho.me</NodeName></Node></Incident></IODEF-Document>`
	if rec := post(t, srv.IntakeHandler(), intake.MediaIODEF, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	records := sink.Records(t, sinkEmitter)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	assertAttribute(t, records[0], "event.name", "iodef")
	assertAttribute(t, records[0], "event.domain", "cert")
	assertAttribute(t, records[0], "report.source", "iodef")
}

func TestDisabledIODEFAnswersServiceUnavailable(t *testing.T) {
	cfg := testConfig()
	cfg.IODEFOn = false
	srv, _, _ := testServer(t, cfg)
	rec := post(t, srv.IntakeHandler(), intake.MediaIODEF, `<IODEF-Document/>`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestPreflightAdvertisesAllowedHeaders(t *testing.T) {
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
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("preflight without Allow-Headers fails a real browser POST")
	}
}

func TestDisallowedOriginGetsNoCORSHeader(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedOrigins = []string{"https://allowed.example"}
	srv, _, _ := testServer(t, cfg)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oneReport))
	req.Header.Set("Content-Type", intake.MediaReportingAPI)
	req.Header.Set("Origin", "https://denied.example")
	denied := httptest.NewRecorder()
	srv.IntakeHandler().ServeHTTP(denied, req)
	if denied.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: the allow-list gates the header, not the report", denied.Code)
	}
	if got := denied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q for a denied origin, want it absent", got)
	}
}

// counterTotal sums one named counter from a manual reader over the data
// points a predicate keeps, so a test can narrow by attribute.
func counterTotal(t *testing.T, reader *sdkmetric.ManualReader, name string, keep func(attribute.Set) bool) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if keep != nil && !keep(dp.Attributes) {
					continue
				}
				total += dp.Value
			}
		}
	}
	return total
}

// droppedByReason sums the dropped counter for one reason from a manual reader.
func droppedByReason(t *testing.T, reader *sdkmetric.ManualReader, reason string) int64 {
	t.Helper()
	return counterTotal(t, reader, "report.relay.dropped", func(attrs attribute.Set) bool {
		v, ok := attrs.Value(attribute.Key("reason"))
		return ok && v.AsString() == reason
	})
}
