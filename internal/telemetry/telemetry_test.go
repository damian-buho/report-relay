// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"errors"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"kiota.ch/damian-buho/report-relay/internal/config"
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
