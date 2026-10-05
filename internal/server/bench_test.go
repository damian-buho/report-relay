// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"kiota.ch/damian-buho/report-relay/internal/intake"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

func benchServer(b *testing.B) *Server {
	b.Helper()
	cfg := testConfig()
	cfg.QueueSize = 1 << 20 // the gate must never trip mid-benchmark
	cfg.RateLimitRPS = 1e9  // the limiter must never trip mid-benchmark
	cfg.RateLimitBurst = 1e9
	em, err := telemetry.NewWithExporters(cfg, &recorder{}, sdkmetric.NewManualReader())
	if err != nil {
		b.Fatalf("NewWithExporters: %v", err)
	}
	b.Cleanup(func() { _ = em.Shutdown(context.Background()) })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, log, em, func() bool { return true })
}

func benchPost(srv *Server) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oneReport))
	req.Header.Set("Content-Type", intake.MediaReportingAPI)
	req.RemoteAddr = "127.0.0.1:4318" // one client, so the limiter table stays at one bucket
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
