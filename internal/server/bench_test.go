// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"kiota.ch/damian-buho/report-relay/internal/intake"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

// discardExporter counts exported records and keeps none: the recorder in
// server_test.go retains every record, which OOMs the host on a long
// benchmark run. Memory here stays bounded by the batch drain, not by N.
type discardExporter struct{ records atomic.Uint64 }

func (d *discardExporter) Export(_ context.Context, batch []sdklog.Record) error {
	d.records.Add(uint64(len(batch)))
	return nil
}

func (d *discardExporter) Shutdown(context.Context) error { return nil }

func (d *discardExporter) ForceFlush(context.Context) error { return nil }

func benchServer(b *testing.B) *Server {
	b.Helper()
	cfg := testConfig()
	cfg.QueueSize = 1 << 20 // the gate must never trip mid-benchmark
	cfg.RateLimitRPS = 1e9  // the limiter must never trip mid-benchmark
	cfg.RateLimitBurst = 1e9
	em, err := telemetry.NewWithExporters(cfg, &discardExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		b.Fatalf("NewWithExporters: %v", err)
	}
	b.Cleanup(func() { _ = em.Shutdown(context.Background()) })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, log, em, func() bool { return true })
}

func benchPost(srv *Server) *httptest.ResponseRecorder {
	return benchPostIP(srv, "127.0.0.1:4318")
}

func benchPostIP(srv *Server, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oneReport))
	req.Header.Set("Content-Type", intake.MediaReportingAPI)
	req.RemoteAddr = remoteAddr // fixed per benchmark, so the limiter path under test is explicit
	rec := httptest.NewRecorder()
	srv.IntakeHandler().ServeHTTP(rec, req)
	return rec
}

// BenchmarkIntakeSerial measures one request end to end without the network.
func BenchmarkIntakeSerial(b *testing.B) {
	srv := benchServer(b)
	b.ResetTimer()
	for range b.N {
		if rec := benchPost(srv); rec.Code != http.StatusNoContent {
			b.Fatalf("status = %d, want 204", rec.Code)
		}
	}
}

// BenchmarkIntakeParallel measures the same path under goroutine pressure.
func BenchmarkIntakeParallel(b *testing.B) {
	srv := benchServer(b)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if rec := benchPost(srv); rec.Code != http.StatusNoContent {
				b.Errorf("status = %d, want 204", rec.Code)
				return
			}
		}
	})
}

// BenchmarkIntakeParallelManyIPs measures the same path with a distinct
// source address per request, exercising the limiter's insert path.
func BenchmarkIntakeParallelManyIPs(b *testing.B) {
	srv := benchServer(b)
	var n atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := n.Add(1)
			addr := net.JoinHostPort(fmt.Sprintf("10.%d.%d.%d", byte(i>>16), byte(i>>8), byte(i)), "4318")
			if rec := benchPostIP(srv, addr); rec.Code != http.StatusNoContent {
				b.Errorf("status = %d, want 204", rec.Code)
				return
			}
		}
	})
}
