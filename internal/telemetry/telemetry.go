// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Package telemetry turns reports into OpenTelemetry log records and counts the
// service's own behaviour as metrics over the same OTLP endpoint.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/intake"
)

// Drop reasons, as the value of the reason attribute on a dropped report.
const (
	ReasonTooLarge    = "too-large"
	ReasonRateLimited = "rate-limited"
	ReasonUnsupported = "unsupported-content-type"
	ReasonDisabled    = "intake-disabled"
	ReasonInvalid     = "invalid"
	ReasonSchema      = "schema"
	ReasonQueueFull   = "queue-full"
)

// Emitter is the service's own telemetry surface: one log record per report,
// plus the counters describing what the intake did with it.
type Emitter struct {
	logger     log.Logger
	logs       *sdklog.LoggerProvider
	metrics    *sdkmetric.MeterProvider
	received   metric.Int64Counter
	accepted   metric.Int64Counter
	dropped    metric.Int64Counter
	exportFail metric.Int64Counter
}

// New builds the log and metric pipelines from the standard OTEL_* environment
// plus the operator's own REPORT_RELAY_* limits.
func New(ctx context.Context, cfg config.Config) (*Emitter, error) {
	logExp, err := otlploghttp.New(ctx, otlploghttp.WithRetry(retryConfig(cfg)))
	if err != nil {
		return nil, fmt.Errorf("otlp log exporter: %w", err)
	}
	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig(retryConfig(cfg))))
	if err != nil {
		return nil, fmt.Errorf("otlp metric exporter: %w", err)
	}
	return NewWithExporters(cfg, logExp, newMeterProvider(cfg, metricExp))
}

// NewWithExporters is the seam that lets a caller supply the log exporter and
// the metric reader, which is what an in-process test needs.
func NewWithExporters(cfg config.Config, logExp sdklog.Exporter, reader sdkmetric.Reader) (*Emitter, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceNamespace(cfg.Namespace),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}
	logs := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(
			logExp,
			sdklog.WithMaxQueueSize(cfg.QueueSize),
			sdklog.WithExportInterval(cfg.BatchTimeout),
			sdklog.WithExportTimeout(cfg.ExportTimeout),
		)),
	)
	metrics := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	em := &Emitter{logger: logs.Logger("report-relay"), logs: logs, metrics: metrics}
	if err := em.registerCounters(); err != nil {
		return nil, err
	}
	quietSDKErrors()
	return em, nil
}

// quietSDKErrors routes the SDK's self-diagnostics into slog at debug level.
// Its default handler prints an unreachable collector on every export interval,
// which is exactly the case an operator most needs a quiet log in.
func quietSDKErrors() {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Debug("otel sdk", "error", err)
	}))
}

// newMeterProvider returns the periodic reader that pushes the service's own
// metrics to the collector on an interval, bounded by the export timeout.
func newMeterProvider(cfg config.Config, exp sdkmetric.Exporter) sdkmetric.Reader {
	return sdkmetric.NewPeriodicReader(exp,
		sdkmetric.WithInterval(cfg.BatchTimeout),
		sdkmetric.WithTimeout(cfg.ExportTimeout),
	)
}

// retryConfig is the export retry policy: exponential backoff with jitter,
// bounded by a total elapsed time, so a dead collector costs a bounded delay
// rather than an unbounded stall.
func retryConfig(cfg config.Config) otlploghttp.RetryConfig {
	return otlploghttp.RetryConfig{
		Enabled:         true,
		InitialInterval: cfg.ExportInitialBackoff,
		MaxInterval:     cfg.ExportMaxBackoff,
		MaxElapsedTime:  cfg.ExportMaxElapsed,
	}
}

// registerCounters declares the four counters the service reports on itself.
func (e *Emitter) registerCounters() error {
	meter := e.metrics.Meter("kiota.ch/damian-buho/report-relay")
	declare := func(name, desc, unit string) (metric.Int64Counter, error) {
		return meter.Int64Counter(name,
			metric.WithDescription(desc), metric.WithUnit(unit))
	}
	var err error
	if e.received, err = declare("report.relay.received",
		"Reports a decoder accepted from the wire", "{report}"); err != nil {
		return err
	}
	if e.accepted, err = declare("report.relay.accepted",
		"Reports queued for OTLP export", "{report}"); err != nil {
		return err
	}
	if e.dropped, err = declare("report.relay.dropped",
		"Reports dropped, labelled by reason", "{report}"); err != nil {
		return err
	}
	if e.exportFail, err = declare("report.relay.export_failures",
		"OTLP exports that failed after every retry", "{export}"); err != nil {
		return err
	}
	return nil
}

// CountReceived records a report a decoder produced.
func (e *Emitter) CountReceived(ctx context.Context, reportType, domain string) {
	e.received.Add(ctx, 1, metric.WithAttributes(
		attribute.String("report_type", reportType),
		attribute.String("domain", domain),
	))
}

// CountAccepted records a report queued for export.
func (e *Emitter) CountAccepted(ctx context.Context, reportType, domain string) {
	e.accepted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("report_type", reportType),
		attribute.String("domain", domain),
	))
}

// CountDropped records a dropped report and the variable that caused the drop.
func (e *Emitter) CountDropped(ctx context.Context, reason, reportType string) {
	e.dropped.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
		attribute.String("report_type", reportType),
	))
}

// CountExportFailure records one export batch that failed after every retry.
func (e *Emitter) CountExportFailure(ctx context.Context, err error) {
	e.exportFail.Add(ctx, 1, metric.WithAttributes(attribute.String("error", errClass(err))))
}

// Emit sends one report as one log record. The record carries event.name and
// event.domain because the fleet's Alloy pipeline promotes exactly those to
// Loki labels; without them the line arrives unlabelled and no query finds it.
func (e *Emitter) Emit(ctx context.Context, r intake.Report) {
	now := time.Now()
	var record log.Record
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	record.SetSeverityText(r.Severity())
	record.SetSeverity(severityNumber(r.Severity()))
	record.SetEventName(r.Type)
	record.AddAttributes(
		attribute.String("event.domain", r.Domain),
		attribute.String("report.type", r.Type),
		attribute.String("report.source", r.Source),
	)
	if r.URL != "" {
		record.AddAttributes(attribute.String("report.url", r.URL))
		if host := hostOf(r.URL); host != "" {
			record.AddAttributes(attribute.String("report.url_host", host))
		}
	}
	if r.Age > 0 {
		record.AddAttributes(attribute.Int64("report.age_ms", r.Age))
	}
	for key, val := range r.Body {
		record.AddAttributes(attribute.String("report."+key, stringify(val)))
	}
	e.logger.Emit(ctx, record)
}

func hostOf(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx >= 0 {
		rawURL = rawURL[idx+3:]
	}
	if idx := strings.IndexAny(rawURL, "/?#"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.LastIndex(rawURL, "@"); idx >= 0 {
		rawURL = rawURL[idx+1:]
	}
	if idx := strings.LastIndex(rawURL, ":"); idx >= 0 && !strings.Contains(rawURL[idx:], "]") {
		rawURL = rawURL[:idx]
	}
	return rawURL
}

func severityNumber(text string) log.Severity {
	switch text {
	case "ERROR":
		return log.SeverityError
	case "WARN":
		return log.SeverityWarn
	default:
		return log.SeverityInfo
	}
}

// Flush pushes everything queued to the collector now. The intake does not call
// it — the answer must not wait on the export path — but a caller that needs the
// records to have landed (a test, or a deliberate checkpoint) can.
func (e *Emitter) Flush(ctx context.Context) error {
	return e.logs.ForceFlush(ctx)
}

// Shutdown drains both pipelines within the deadline the caller sets.
func (e *Emitter) Shutdown(ctx context.Context) error {
	logErr := e.logs.ForceFlush(ctx)
	metricErr := e.metrics.Shutdown(ctx)
	return errors.Join(logErr, metricErr)
}

// stringify renders a decoded JSON value as the flat string an OTLP log
// attribute carries, so a nested body still lands as one queryable line.
func stringify(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	}
}

// errClass reduces an export error to a stable label, so the failure counter
// does not carry a message that changes on every attempt.
func errClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "export-failed"
}
