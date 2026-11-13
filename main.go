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
	"time"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/intake"
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
	if err := cfg.Validate(); err != nil {
		log.Error("invalid configuration", "error", err)
		return 1
	}
	intake.RegisterTypo(cfg.TypoOn)

	// SIGINT and SIGTERM cancel this context, so the HTTP servers and the
	// telemetry pipelines unwind together instead of being killed mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	emitter, err := telemetry.New(ctx, cfg)
	if err != nil {
		log.Error("telemetry pipeline failed to start", "error", err)
		return 1
	}
	if !telemetry.OTLPConfigured() {
		log.Info("otlp endpoint unset, writing records to stdout")
	}

	readTimeout, writeTimeout, idleTimeout := server.Timeouts()
	srv := server.New(cfg, log, emitter, func() bool { return !telemetry.OTLPConfigured() || emitter.ExportHealthy() })
	intakeSrv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           srv.IntakeHandler(),
		ReadHeaderTimeout: readTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	adminSrv := &http.Server{
		Addr:              ":" + cfg.AdminPort,
		Handler:           srv.AdminHandler(),
		ReadHeaderTimeout: readTimeout,
		ReadTimeout:       readTimeout,
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
		if err := pendingListenerError(errs); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listener failed", "error", err)
			exitCode = 1
		}
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listener failed", "error", err)
			exitCode = 1
		}
	}

	exitCode = drain(exitCode, intakeSrv, adminSrv, srv, emitter, log, cfg.ShutdownTimeout, cfg.ShutdownTimeout)
	log.Info("stopped", "exit_code", exitCode)
	return exitCode
}

// pendingListenerError returns the error a listener reported, or nil when no listener has failed yet.
func pendingListenerError(errs <-chan error) error {
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

// drain shuts each listener down within its own timeout, then the emitter, and returns 1 if any step failed.
func drain(exitCode int, intakeSrv, adminSrv *http.Server, srv *server.Server, emitter *telemetry.Emitter, log *slog.Logger, drainTimeout, emitTimeout time.Duration) int {
	for _, l := range []struct {
		name string
		http *http.Server
	}{{"intake", intakeSrv}, {"admin", adminSrv}} {
		if err := drainListener(l.http, l.name, drainTimeout, log); err != nil {
			exitCode = 1
		}
	}
	// The limiter sweeps on its own timer, so stop it once the listener it served is gone.
	srv.Close()
	// The emitter drains on its own deadline: a slow listener drain must not starve the export queue of its own bounded wait.
	emitCtx, emitCancel := context.WithTimeout(context.Background(), emitTimeout)
	defer emitCancel()
	if err := emitter.Shutdown(emitCtx); err != nil {
		log.Error("telemetry drain incomplete", "error", err)
		exitCode = 1
	}
	return exitCode
}

// drainListener gives one listener its own drain deadline and force-closes it when the drain gives up.
func drainListener(srv *http.Server, name string, timeout time.Duration, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err == nil {
		return nil
	}
	log.Error(name+" shutdown incomplete", "error", err)
	err = errors.Join(err, srv.Close())
	return err
}

func serve(srv *http.Server, name string, errs chan<- error) {
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}
