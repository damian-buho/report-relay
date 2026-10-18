// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/server"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

const testBin = "report-relay"

type stubLogExporter struct{}

func (stubLogExporter) Export(context.Context, []sdklog.Record) error { return nil }
func (stubLogExporter) Shutdown(context.Context) error                { return nil }
func (stubLogExporter) ForceFlush(context.Context) error              { return nil }

func TestRunVersion(t *testing.T) {
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	}()
	os.Args = []string{testBin, "--version"}
	flag.CommandLine = flag.NewFlagSet("report-relay", flag.ContinueOnError)
	if got := run(); got != 0 {
		t.Errorf("run() = %d, want 0", got)
	}
}

func TestRunInvalidConfig(t *testing.T) {
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	}()
	os.Args = []string{testBin}
	flag.CommandLine = flag.NewFlagSet(testBin, flag.ContinueOnError)
	t.Setenv("REPORT_RELAY_HTTP_PORT", "1")
	if got := run(); got != 1 {
		t.Errorf("run() = %d, want 1", got)
	}
}

func TestServe(t *testing.T) {
	srv := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	errs := make(chan error, 1)
	go serve(srv, "test", errs)
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	select {
	case err := <-errs:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	default:
	}
}

func TestServePortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Addr: "127.0.0.1:" + strconv.Itoa(port), Handler: http.NewServeMux()}
	errs := make(chan error, 1)
	go serve(srv, "test", errs)
	select {
	case err := <-errs:
		if err == nil {
			t.Error("serve sent nil error for a port in use")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not report an error for a port in use")
	}
}

func TestRunIntakePortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	t.Setenv("REPORT_RELAY_HTTP_PORT", strconv.Itoa(port))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	}()
	os.Args = []string{testBin}
	flag.CommandLine = flag.NewFlagSet(testBin, flag.ContinueOnError)
	if got := run(); got != 1 {
		t.Errorf("run() = %d, want 1", got)
	}
}

// drainSetup builds the fixtures drain needs: two HTTP servers with a blocking
// handler, a server, an emitter and a logger. It returns both server
// addresses so a test can hold a connection open on each.
func drainSetup(t *testing.T) (intakeSrv, adminSrv *http.Server, srv *server.Server, emitter *telemetry.Emitter, log *slog.Logger, intakeAddr, adminAddr string) {
	t.Helper()
	cfg := config.Config{
		ServiceName: "test",
		Namespace:   "test",
		QueueSize:   10,
		HTTPPort:    "0",
		AdminPort:   "0",
	}
	log = slog.New(slog.NewTextHandler(io.Discard, nil))
	emitter, err := telemetry.NewWithExporters(cfg, stubLogExporter{}, sdkmetric.NewManualReader())
	if err != nil {
		t.Fatalf("NewWithExporters: %v", err)
	}
	srv = server.New(cfg, log, emitter, func() bool { return true })
	release := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-release })
	intakeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	adminLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	intakeSrv = &http.Server{Handler: handler}
	adminSrv = &http.Server{Handler: handler}
	go func() { _ = intakeSrv.Serve(intakeLn) }()
	go func() { _ = adminSrv.Serve(adminLn) }()
	t.Cleanup(func() {
		close(release)
		_ = intakeSrv.Close()
		_ = adminSrv.Close()
		_ = emitter.Shutdown(context.Background())
	})
	return intakeSrv, adminSrv, srv, emitter, log, intakeLn.Addr().String(), adminLn.Addr().String()
}

// TestDrainCancelled holds one connection per server open and cancels the
// context, so Shutdown returns context.Canceled and drain reports failure.
func TestDrainCancelled(t *testing.T) {
	intakeSrv, adminSrv, srv, emitter, log, intakeAddr, adminAddr := drainSetup(t)
	for _, target := range []string{intakeAddr, adminAddr} {
		go func(t string) {
			resp, err := http.Get("http://" + t + "/")
			if err == nil {
				defer func() { _ = resp.Body.Close() }()
				time.Sleep(5 * time.Second)
			}
		}(target)
	}
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := drain(ctx, 0, intakeSrv, adminSrv, srv, emitter, log, time.Second); code != 1 {
		t.Errorf("drain = %d, want 1", code)
	}
}

// TestDrainEmitterTimeout passes a zero emitter timeout, so the emitter
// shutdown fails and drain reports failure.
func TestDrainEmitterTimeout(t *testing.T) {
	intakeSrv, adminSrv, srv, emitter, log, _, _ := drainSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if code := drain(ctx, 0, intakeSrv, adminSrv, srv, emitter, log, 0); code != 1 {
		t.Errorf("drain = %d, want 1", code)
	}
}

// TestRunReadyEndpoint starts the service, hits /readyz on the admin server
// (which calls the ready closure), then shuts down cleanly.
func TestRunReadyEndpoint(t *testing.T) {
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	httpPort := httpLn.Addr().(*net.TCPAddr).Port
	httpLn.Close()
	adminLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	adminPort := adminLn.Addr().(*net.TCPAddr).Port
	adminLn.Close()
	t.Setenv("REPORT_RELAY_HTTP_PORT", strconv.Itoa(httpPort))
	t.Setenv("REPORT_RELAY_ADMIN_PORT", strconv.Itoa(adminPort))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	}()
	os.Args = []string{testBin}
	flag.CommandLine = flag.NewFlagSet(testBin, flag.ContinueOnError)
	done := make(chan int, 1)
	go func() {
		done <- run()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(adminPort))
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(adminPort) + "/readyz")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	_ = resp.Body.Close()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("syscall.Kill: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("run() = %d, want 0", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run() did not return after SIGINT")
	}
}

func TestRunMainPath(t *testing.T) {
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	httpPort := httpLn.Addr().(*net.TCPAddr).Port
	httpLn.Close()
	adminLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	adminPort := adminLn.Addr().(*net.TCPAddr).Port
	adminLn.Close()

	t.Setenv("REPORT_RELAY_HTTP_PORT", strconv.Itoa(httpPort))
	t.Setenv("REPORT_RELAY_ADMIN_PORT", strconv.Itoa(adminPort))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")

	oldArgs := os.Args
	oldFlags := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	}()
	os.Args = []string{testBin}
	flag.CommandLine = flag.NewFlagSet(testBin, flag.ContinueOnError)
	done := make(chan int, 1)
	go func() {
		done <- run()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(httpPort))
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("syscall.Kill: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("run() = %d, want 0", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run() did not return after SIGINT")
	}
}
