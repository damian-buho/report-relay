// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Command report-relay accepts the security reports browsers and mail servers
// send and emits one OpenTelemetry log record per report.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/server"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

var version = "unknown"

func main() {
	os.Exit(run())
}

// run starts the service and returns the process exit code. Keeping os.Exit in
// main alone satisfies gocritic's exitAfterDefer, so every defer below runs.
func run() int {
	showVersion := flag.Bool("version", false, "Print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return 0
	}

	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	// SIGINT and SIGTERM cancel this context, so the HTTP servers and the
	// telemetry pipelines unwind together instead of being killed mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	emitter, err := telemetry.New(ctx, cfg)
	if err != nil {
		log.Error("telemetry pipeline failed to start", "error", err)
		return 1
	}

	readTimeout, writeTimeout, idleTimeout := server.Timeouts()
	srv := server.New(cfg, log, emitter, func() bool { return true })
	intakeSrv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           srv.IntakeHandler(),
		ReadHeaderTimeout: readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	adminSrv := &http.Server{
		Addr:              ":" + cfg.AdminPort,
		Handler:           srv.AdminHandler(),
		ReadHeaderTimeout: readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	errs := make(chan error, 2)
	go serve(intakeSrv, "intake", errs)
	go serve(adminSrv, "admin", errs)

	log.Info("serving",
		"version", version,
		"intake_addr", intakeSrv.Addr,
		"admin_addr", adminSrv.Addr,
		"otlp_endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	)

	exitCode := 0
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received, draining", "deadline", cfg.ShutdownTimeout)
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listener failed", "error", err)
			exitCode = 1
		}
	}

	// The drain deadline covers both listeners and the export queue, so a slow
	// collector delays shutdown by a bounded amount and no more.
	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := intakeSrv.Shutdown(drainCtx); err != nil {
		log.Error("intake shutdown incomplete", "error", err)
		exitCode = 1
	}
	if err := adminSrv.Shutdown(drainCtx); err != nil {
		log.Error("admin shutdown incomplete", "error", err)
		exitCode = 1
	}
	if err := emitter.Shutdown(drainCtx); err != nil {
		log.Error("telemetry drain incomplete", "error", err)
		exitCode = 1
	}
	log.Info("stopped", "exit_code", exitCode)
	return exitCode
}

func serve(srv *http.Server, name string, errs chan<- error) {
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}
