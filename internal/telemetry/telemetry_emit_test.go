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
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"kiota.ch/damian-buho/report-relay/internal/intake"
)

func TestAttrValue(t *testing.T) {
	cases := []struct {
		name string
		val  attribute.Value
		want any
	}{
		{"bool", attribute.BoolValue(true), true},
		{"int64", attribute.Int64Value(42), int64(42)},
		{"float64", attribute.Float64Value(3.14), 3.14},
		{"string", attribute.StringValue("world"), "world"},
		{"bool slice", attribute.BoolSliceValue([]bool{true, false}), []any{true, false}},
		{"int64 slice", attribute.Int64SliceValue([]int64{1, 2}), []any{int64(1), int64(2)}},
		{"float64 slice", attribute.Float64SliceValue([]float64{1.1, 2.2}), []any{1.1, 2.2}},
		{"string slice", attribute.StringSliceValue([]string{"a", "b"}), []any{"a", "b"}},
		{"byte slice", attribute.ByteSliceValue([]byte("hi")), []byte("hi")},
		{"slice", attribute.SliceValue(attribute.StringValue("a")), []any{"a"}},
		{"map", attribute.MapValue(attribute.KeyValue{Key: "k", Value: attribute.StringValue("v")}), map[string]any{"k": "v"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attrValue(tc.val); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("attrValue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStringify(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"string", "hello", "hello"},
		{"json number", json.Number("42"), "42"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, ""},
		{"map", map[string]any{"a": "b"}, `{"a":"b"}`},
		{"slice", []any{"a", "b"}, `["a","b"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringify(tc.val); got != tc.want {
				t.Errorf("stringify = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmitQueuesReportWithBody(t *testing.T) {
	em, err := NewWithExporters(testConfig(), stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	report := intake.Report{
		Type:   "network-error",
		Domain: intake.DomainBrowser,
		Source: intake.SourceReportingAPI,
		URL:    "https://beta.dbuho.me/path?q=secret",
		Age:    1000,
		Body:   map[string]any{"phase": "connection", "blockedURL": "https://evil.com/track?token=abc"},
	}
	if !em.Emit(context.Background(), report) {
		t.Fatal("Emit refused a report with room in the queue")
	}
	if got := em.queued.Load(); got != 1 {
		t.Errorf("queued = %d, want 1", got)
	}
}

// captureExporter keeps every record the batch processor delivers, so an
// assertion is on the attributes a report actually became.
type captureExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (c *captureExporter) Export(_ context.Context, batch []sdklog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, batch...)
	return nil
}

func (c *captureExporter) Shutdown(context.Context) error   { return nil }
func (c *captureExporter) ForceFlush(context.Context) error { return nil }

func (c *captureExporter) Records() []sdklog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.records)
}

// attributesOf flattens a record's attributes into a lookup by key.
func attributesOf(record sdklog.Record) map[string]string {
	out := map[string]string{}
	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value.String()
		return true
	})
	return out
}

// emittingRecords emits one report and returns the attributes of the record it
// became, with the flush that carries it out of the queue.
func emittingRecords(t *testing.T, report intake.Report) map[string]string {
	t.Helper()
	sink := &captureExporter{}
	em, err := NewWithExporters(testConfig(), sink, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	if !em.Emit(context.Background(), report) {
		t.Fatal("Emit refused a report with room in the queue")
	}
	if err := em.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	records := sink.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	return attributesOf(records[0])
}

// spareReport is the one-report fixture the attribute assertions share: a type
// that needs no url, an age and no body shape of its own.
func spareReport() intake.Report {
	report := reportsOf("deprecation")[0]
	report.Body = map[string]any{"id": "websql"}
	return report
}

// TestEmitOmitsTheURLAndAgeTheReportDoesNotCarry pins the three optional
// attributes: no url, no age and no host without a url to take a host from.
func TestEmitOmitsTheURLAndAgeTheReportDoesNotCarry(t *testing.T) {
	attrs := emittingRecords(t, spareReport())
	for _, key := range []string{"report.url", "report.url_host", "report.age_ms"} {
		if _, ok := attrs[key]; ok {
			t.Errorf("%s = %v, want the attribute absent", key, attrs[key])
		}
	}
}

// TestEmitCarriesEveryURLAttributeWhenTheReportHasOne is the populated side of
// the same three, at the smallest age the record admits.
func TestEmitCarriesEveryURLAttributeWhenTheReportHasOne(t *testing.T) {
	report := spareReport()
	report.URL = "https://beta.example.com/legacy"
	report.Age = 1
	attrs := emittingRecords(t, report)
	if attrs["report.url"] != "https://beta.example.com/legacy" {
		t.Errorf("report.url = %q, want the reported url", attrs["report.url"])
	}
	if attrs["report.url_host"] != "beta.example.com" {
		t.Errorf("report.url_host = %q, want the host", attrs["report.url_host"])
	}
	if attrs["report.age_ms"] != "1" {
		t.Errorf("report.age_ms = %q, want 1", attrs["report.age_ms"])
	}
}

// TestEmitOmitsTheHostWhenTheURLWillNotParse pins the host attribute's guard:
// the url still rides along, but no host is invented from a value net/url
// refused.
func TestEmitOmitsTheHostWhenTheURLWillNotParse(t *testing.T) {
	report := spareReport()
	report.URL = "http://[::1"
	attrs := emittingRecords(t, report)
	if attrs["report.url"] != "http://[::1" {
		t.Errorf("report.url = %q, want the reported url kept whole", attrs["report.url"])
	}
	if _, ok := attrs["report.url_host"]; ok {
		t.Errorf("report.url_host = %q, want no host from an unparseable url", attrs["report.url_host"])
	}
}

// TestNewWithExportersClampsAnEmptyQueueToOneSlot pins the one-slot floor: an
// unset queue size would be a zero-length channel the gate can never claim.
func TestNewWithExportersClampsAnEmptyQueueToOneSlot(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 0
	em, err := NewWithExporters(cfg, stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	if em.queueSize != 1 {
		t.Errorf("queueSize = %d, want the clamp to 1", em.queueSize)
	}
	if !em.Emit(context.Background(), reportsOf("deprecation")[0]) {
		t.Error("the one-slot queue refused its only report")
	}
}

// TestEveryCounterIsRegistered pins the declarations: the export-failure
// counter in particular is only reached on a failed export, so a registration
// that never ran would leave a nil instrument behind until the first outage.
func TestEveryCounterIsRegistered(t *testing.T) {
	em, err := NewWithExporters(testConfig(), stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	for name, instrument := range map[string]any{
		"received":   em.received,
		"accepted":   em.accepted,
		"dropped":    em.dropped,
		"exportFail": em.exportFail,
	} {
		if reflect.ValueOf(instrument).IsNil() {
			t.Errorf("counter %q was never registered", name)
		}
	}
}

// failingWriter refuses every write, which is the only failure the stdout
// exporter can meet.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// TestStdoutExporterReportsAWriteFailure pins that a broken stdout reaches the
// caller as an error, so the batch processor drops the batch instead of
// reporting it delivered.
func TestStdoutExporterReportsAWriteFailure(t *testing.T) {
	exp := &stdoutExporter{w: failingWriter{}}
	var record sdklog.Record
	record.SetEventName("unwritten")
	if err := exp.Export(context.Background(), []sdklog.Record{record}); err == nil {
		t.Error("a write that failed was reported as a delivered record")
	}
}

func TestShutdownReturnsNil(t *testing.T) {
	em, err := NewWithExporters(testConfig(), stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	if err := em.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestCountersRecordMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	em, err := NewWithExporters(testConfig(), stubExporter{}, reader)
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	ctx := context.Background()
	em.CountReceived(ctx, "coep", intake.DomainBrowser)
	em.CountAccepted(ctx, "coep", intake.DomainBrowser)
	em.CountDropped(ctx, ReasonTooLarge, "coep")
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	wantCounters := map[string]int64{
		"report.relay.received": 1,
		"report.relay.accepted": 1,
		"report.relay.dropped":  1,
	}
	found := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if want, ok := wantCounters[m.Name]; ok {
				found[m.Name] = true
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Errorf("metric %q is not a sum", m.Name)
					continue
				}
				if len(sum.DataPoints) == 0 {
					t.Errorf("metric %q has no data points", m.Name)
					continue
				}
				if sum.DataPoints[0].Value != want {
					t.Errorf("metric %q = %d, want %d", m.Name, sum.DataPoints[0].Value, want)
				}
			}
		}
	}
	for name := range wantCounters {
		if !found[name] {
			t.Errorf("metric %q was not collected", name)
		}
	}
}

func TestSeverityNumber(t *testing.T) {
	cases := map[string]log.Severity{
		"ERROR": log.SeverityError,
		"WARN":  log.SeverityWarn,
		"INFO":  log.SeverityInfo,
		"OTHER": log.SeverityInfo,
	}
	for input, want := range cases {
		if got := severityNumber(input); got != want {
			t.Errorf("severityNumber(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestErrClass(t *testing.T) {
	if got := errClass(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("errClass(DeadlineExceeded) = %q, want timeout", got)
	}
	if got := errClass(errors.New("connection refused")); got != "export-failed" {
		t.Errorf("errClass = %q, want export-failed", got)
	}
}

func TestEmitQueueFull(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 1
	cfg.BatchTimeout = time.Hour
	em, err := NewWithExporters(cfg, stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	if !em.Emit(context.Background(), reportsOf("deprecation")[0]) {
		t.Fatal("first Emit was refused")
	}
	if em.Emit(context.Background(), reportsOf("deprecation")[0]) {
		t.Error("second Emit was admitted past the queue capacity")
	}
}

func TestStdoutExporterExport(t *testing.T) {
	var buf bytes.Buffer
	exp := &stdoutExporter{w: &buf}
	var record sdklog.Record
	record.SetEventName("test-report")
	record.SetSeverityText("WARN")
	if err := exp.Export(context.Background(), []sdklog.Record{record}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout line is not JSON: %v", err)
	}
	if decoded["event_name"] != "test-report" {
		t.Errorf("event_name = %v, want test-report", decoded["event_name"])
	}
}

func TestStdoutExporterShutdown(t *testing.T) {
	exp := &stdoutExporter{w: io.Discard}
	if err := exp.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestCountingExporterShutdown(t *testing.T) {
	em, err := NewWithExporters(testConfig(), stubExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	defer func() { _ = em.Shutdown(context.Background()) }()
	wrapped := &countingExporter{inner: stubExporter{}, em: em}
	if err := wrapped.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
