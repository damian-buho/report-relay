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
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
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
	queueSize  int
	queued     atomic.Int64
	failures   atomic.Uint64
}

// exportFailThreshold is the consecutive export failures after which the
// service stops calling itself ready. One failure is a blip; three is a dead
// collector, and /readyz should say so.
const exportFailThreshold = 3

// New builds the log and metric pipelines from the standard OTEL_* environment
// plus the operator's own REPORT_RELAY_* limits. Without an OTLP endpoint the
// records go to standard output instead, so the relay runs with no collector.
func New(ctx context.Context, cfg config.Config) (*Emitter, error) {
	if !OTLPConfigured() {
		return NewWithExporters(cfg, newStdoutExporter(), sdkmetric.NewManualReader())
	}
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
	queueSize := cfg.QueueSize
	if queueSize < 1 {
		queueSize = 1
	}
	em := &Emitter{
		metrics: sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(reader),
		),
		queueSize: queueSize,
	}
	if err := em.registerCounters(); err != nil {
		return nil, err
	}
	em.logs = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(
			&countingExporter{inner: logExp, em: em},
			sdklog.WithMaxQueueSize(queueSize),
			sdklog.WithExportInterval(cfg.BatchTimeout),
			sdklog.WithExportTimeout(cfg.ExportTimeout),
		)),
	)
	em.logger = em.logs.Logger("report-relay")
	quietSDKErrors()
	return em, nil
}

// countingExporter wraps the log exporter so export failures are counted and
// logged instead of vanishing into the SDK. The batch processor drops a failed
// batch after the call returns, so each failure also releases its queue slots.
type countingExporter struct {
	inner sdklog.Exporter
	em    *Emitter
}

// Export forwards the batch and records the outcome against the emitter.
func (c *countingExporter) Export(ctx context.Context, batch []sdklog.Record) error {
	err := c.inner.Export(ctx, batch)
	c.em.queued.Add(-int64(len(batch)))
	if err != nil {
		c.em.CountExportFailure(ctx, err)
		return err
	}
	c.em.failures.Store(0)
	return nil
}

// Shutdown forwards the shutdown to the wrapped exporter.
func (c *countingExporter) Shutdown(ctx context.Context) error {
	return c.inner.Shutdown(ctx)
}

// ForceFlush forwards the flush to the wrapped exporter.
func (c *countingExporter) ForceFlush(ctx context.Context) error {
	return c.inner.ForceFlush(ctx)
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
		attribute.String("report_type", intake.SanitizeType(reportType)),
		attribute.String("domain", domain),
	))
}

// CountAccepted records a report queued for export.
func (e *Emitter) CountAccepted(ctx context.Context, reportType, domain string) {
	e.accepted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("report_type", intake.SanitizeType(reportType)),
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
// It also logs the failure where an operator will see it: the SDK's own error
// handler is demoted to debug, so without this line a dead collector is silent.
func (e *Emitter) CountExportFailure(ctx context.Context, err error) {
	class := errClass(err)
	e.exportFail.Add(ctx, 1, metric.WithAttributes(attribute.String("error", class)))
	e.failures.Add(1)
	slog.Warn("otlp export failed", "error_class", class, "error", err)
}

// ExportHealthy reports whether recent exports succeeded. After a run of
// consecutive failures the collector is presumed dead and /readyz says so.
func (e *Emitter) ExportHealthy() bool {
	return e.failures.Load() < exportFailThreshold
}

// Emit queues one report as one log record and reports whether it was queued.
// The SDK drops the oldest record once its own queue is full, silently losing
// a report the intake already answered 204 for. This gate drops first instead:
// while the estimated backlog is at capacity the report is counted queue-full
// and never enqueued, so the drop has a metric and the SDK queue never fills.
func (e *Emitter) Emit(ctx context.Context, r intake.Report) bool {
	r.Type = intake.SanitizeType(r.Type)
	for {
		// The check and the claim are one atomic step, so a burst cannot overshoot the bound.
		backlog := e.queued.Load()
		if backlog >= int64(e.queueSize) {
			e.dropped.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", ReasonQueueFull),
				attribute.String("report_type", r.Type),
			))
			slog.Warn("queue full, report dropped", "report_type", r.Type, "queue_size", e.queueSize)
			return false
		}
		if e.queued.CompareAndSwap(backlog, backlog+1) {
			break
		}
	}
	now := time.Now()
	var record log.Record
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	record.SetSeverityText(r.Severity())
	record.SetSeverity(severityNumber(r.Severity()))
	record.SetEventName(r.Type)
	record.AddAttributes(
		// event.name is ALSO an attribute, not only the first-class EventName
		// field: the collector's attributes processor promotes `event.name` to a
		// label, and it reads ATTRIBUTES. A report type that never became a label
		// is invisible to every query that filters on it.
		attribute.String("event.name", r.Type),
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
	return true
}

// hostOf returns the host of a URL for the url_host attribute. Parsing is
// delegated to net/url, which already knows about IPv6 brackets, ports,
// userinfo and case: the hand-rolled version lost IPv6 tail segments.
func hostOf(rawURL string) string {
	candidate := rawURL
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimSuffix(host, ".")
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
