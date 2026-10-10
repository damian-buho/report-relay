// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/intake"
)

// stubExporter accepts every batch, so the emitter's own accounting is what a
// test observes rather than the SDK's.
type stubExporter struct{}

func (stubExporter) Export(context.Context, []sdklog.Record) error { return nil }
func (stubExporter) Shutdown(context.Context) error                { return nil }
func (stubExporter) ForceFlush(context.Context) error              { return nil }

func testConfig() config.Config {
	return config.Config{
		ServiceName: "report-relay",
		Namespace:   "me.dbuho",
		QueueSize:   8,
	}
}

func TestHostOfParsesWhatMatters(t *testing.T) {
	cases := map[string]string{
		"https://beta.dbuho.me/path?q=1":      "beta.dbuho.me",
		"https://user:secret@Example.COM:443": "example.com",
		"http://[2001:db8::1]:8080/x":         "2001:db8::1",
		"http://[2001:db8::1]/x":              "2001:db8::1",
		"dbuho.me":                            "dbuho.me",
		"Example.COM.":                        "example.com",
		"":                                    "",
	}
	for input, want := range cases {
		if got := hostOf(input); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExportHealthTripsAfterConsecutiveFailures(t *testing.T) {
	em, err := NewWithExporters(testConfig(), stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	if !em.ExportHealthy() {
		t.Fatal("a fresh emitter reads unhealthy")
	}
	em.CountExportFailure(context.Background(), errors.New("collector unreachable"))
	em.CountExportFailure(context.Background(), errors.New("collector unreachable"))
	if !em.ExportHealthy() {
		t.Fatal("two failures read unhealthy before the threshold")
	}
	em.CountExportFailure(context.Background(), context.DeadlineExceeded)
	if em.ExportHealthy() {
		t.Fatal("three consecutive failures still read healthy")
	}
}

func TestCountingExporterReleasesSlotsAndCounts(t *testing.T) {
	em, err := NewWithExporters(testConfig(), stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	wrapped := &countingExporter{inner: &failingExporter{}, em: em}
	em.queued.Add(2)
	if err := wrapped.Export(context.Background(), make([]sdklog.Record, 2)); err == nil {
		t.Fatal("a failing inner exporter reported success")
	}
	if got := em.queued.Load(); got != 0 {
		t.Errorf("queued = %d, want the failed batch released", got)
	}
	if !em.ExportHealthy() {
		t.Error("one failure tripped readiness before the threshold")
	}
	em.CountExportFailure(context.Background(), errors.New("collector unreachable"))
	em.CountExportFailure(context.Background(), errors.New("collector unreachable"))
	if em.ExportHealthy() {
		t.Error("three consecutive failures still read healthy")
	}
	if err := wrapped.Export(context.Background(), nil); err != nil {
		t.Fatalf("the recovery export failed: %v", err)
	}
	if !em.ExportHealthy() {
		t.Error("a successful export did not clear the failure run")
	}
}

// failingExporter fails the first call and succeeds after, so the counting
// wrapper's failure and recovery paths are both exercised.
type failingExporter struct{ calls int }

func (f *failingExporter) Export(context.Context, []sdklog.Record) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("collector unreachable")
	}
	return nil
}
func (f *failingExporter) Shutdown(context.Context) error   { return nil }
func (f *failingExporter) ForceFlush(context.Context) error { return nil }

func TestOTLPConfiguredReadsTheEndpointVariables(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	if OTLPConfigured() {
		t.Fatal("no endpoint named, yet the OTLP path was selected")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://o9s-alloy:4318")
	if !OTLPConfigured() {
		t.Fatal("a named endpoint did not select the OTLP path")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://o9s-alloy:4318")
	if !OTLPConfigured() {
		t.Fatal("a named logs endpoint did not select the OTLP path")
	}
}

func TestNewWithoutEndpointEmitsToWriter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	var buf bytes.Buffer
	em, err := NewWithExporters(testConfig(), &stdoutExporter{w: &buf}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	report := intake.Report{
		Type:   "csp-violation",
		Domain: intake.DomainBrowser,
		Source: intake.SourceReportingAPI,
		URL:    "https://beta.dbuho.me/marker",
		Body:   map[string]any{"effectiveDirective": "script-src"},
	}
	if !em.Emit(context.Background(), report) {
		t.Fatal("the stdout emitter refused a report with room in the queue")
	}
	if err := em.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	line := strings.TrimSpace(buf.String())
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("stdout line is not JSON: %v", err)
	}
	if decoded["event_name"] != "csp-violation" {
		t.Errorf("event_name = %v, want the report type", decoded["event_name"])
	}
	if decoded["event.domain"] != intake.DomainBrowser {
		t.Errorf("event.domain = %v, want the report domain", decoded["event.domain"])
	}
	if decoded["resource.service.name"] != "report-relay" {
		t.Errorf("resource.service.name = %v, want the configured service", decoded["resource.service.name"])
	}
}

// reportsOf builds bare reports of the given types, which is all the queue gate looks at.
func reportsOf(types ...string) []intake.Report {
	out := make([]intake.Report, 0, len(types))
	for _, reportType := range types {
		out = append(out, intake.Report{
			Type:   reportType,
			Domain: intake.DomainBrowser,
			Source: intake.SourceReportingAPI,
		})
	}
	return out
}

func TestEmitBatchTakesTheWholeBatchOrNothing(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 2
	cfg.BatchTimeout = time.Hour
	reader := sdkmetric.NewManualReader()
	em, err := NewWithExporters(cfg, stubExporter{}, reader)
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	if em.EmitBatch(context.Background(), reportsOf("deprecation", "deprecation", "deprecation")) {
		t.Error("a batch past the queue was accepted")
	}
	if got := em.queued.Load(); got != 0 {
		t.Errorf("queued = %d after a refused batch, want nothing claimed", got)
	}
	if !em.EmitBatch(context.Background(), reportsOf("deprecation", "deprecation")) {
		t.Fatal("a batch the queue takes whole was refused")
	}
	if got := em.queued.Load(); got != 2 {
		t.Errorf("queued = %d, want the two claimed slots", got)
	}
	if em.EmitBatch(context.Background(), reportsOf("coop")) {
		t.Error("a single report was accepted past the claimed slots")
	}
	if err := em.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := em.queued.Load(); got != 0 {
		t.Errorf("queued = %d after the flush, want the slots released", got)
	}
}

func TestNewFallsBackToStdoutWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	em, err := New(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("New without an endpoint: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	if !em.ExportHealthy() {
		t.Fatal("a fresh stdout emitter reads unhealthy")
	}
}

// gateCollector answers 502 to every log upload until its gate opens, which is how a collector that is down for a while and then recovers behaves.
type gateCollector struct {
	open     time.Time
	mu       sync.Mutex
	attempts int
	bodies   []string
}

func (g *gateCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/logs" {
		w.WriteHeader(http.StatusOK)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attempts++
	if time.Now().Before(g.open) {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	body, _ := io.ReadAll(r.Body)
	g.bodies = append(g.bodies, string(body))
	w.WriteHeader(http.StatusOK)
}

func (g *gateCollector) received(marker string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, body := range g.bodies {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// TestAnExportOutlivingTheAttemptDeadlineDeliversTheBatch pins the precedence between the two knobs: the retry budget is longer, so the processor ceiling must be the budget rather than the attempt deadline.
func TestAnExportOutlivingTheAttemptDeadlineDeliversTheBatch(t *testing.T) {
	collector := &gateCollector{open: time.Now().Add(2500 * time.Millisecond)}
	endpoint := httptest.NewServer(collector)
	defer endpoint.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")

	// The shipped relation between the knobs — a retry budget well past the per-attempt deadline — compressed so the suite stays in seconds.
	cfg := testConfig()
	cfg.BatchTimeout = 50 * time.Millisecond
	cfg.ExportTimeout = time.Second
	cfg.ExportInitialBackoff = 50 * time.Millisecond
	cfg.ExportMaxBackoff = 500 * time.Millisecond
	cfg.ExportMaxElapsed = 5 * time.Second
	em, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	report := intake.Report{
		Type:   "deprecation",
		Domain: intake.DomainBrowser,
		Source: intake.SourceReportingAPI,
		URL:    "https://beta.dbuho.me/marker",
		Body:   map[string]any{"effectiveDirective": "script-src"},
	}
	if !em.Emit(context.Background(), report) {
		t.Fatal("Emit refused a report with room in the queue")
	}
	if err := em.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !em.ExportHealthy() {
		t.Error("a collector that recovered inside the budget reads as failed")
	}
	if !collector.received("deprecation") {
		t.Errorf("attempts = %d, no accepted upload carried the report", collector.attempts)
	}
}
