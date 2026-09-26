// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// OTLPConfigured reports whether an OTLP log endpoint is named. It is the
// single source of truth for the exporter choice below and for /readyz.
func OTLPConfigured() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != ""
}

// stdoutExporter is the no-collector fallback: one JSON line per record on the
// given writer. It never buffers and never retries, so a broken pipe is the
// only failure it can report.
type stdoutExporter struct {
	mu sync.Mutex
	w  io.Writer
}

// newStdoutExporter writes records to standard output.
func newStdoutExporter() *stdoutExporter {
	return &stdoutExporter{w: os.Stdout}
}

// Export renders the batch as JSON lines. A marshal failure skips the record
// rather than the batch, so one odd attribute never costs its neighbours.
func (e *stdoutExporter) Export(_ context.Context, batch []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range batch {
		line, err := json.Marshal(stdoutLine(&batch[i]))
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(e.w, "%s\n", line); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown drains nothing: every Export already wrote through.
func (e *stdoutExporter) Shutdown(context.Context) error { return nil }

// ForceFlush writes through on every Export, so there is nothing to flush.
func (e *stdoutExporter) ForceFlush(context.Context) error { return nil }

// stdoutLine flattens one record into the JSON object a line carries.
func stdoutLine(r *sdklog.Record) map[string]any {
	out := map[string]any{
		"timestamp":          r.Timestamp().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		"observed_timestamp": r.ObservedTimestamp().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		"severity":           r.Severity().String(),
		"severity_text":      r.SeverityText(),
		"event_name":         r.EventName(),
	}
	if res := r.Resource(); res != nil {
		for _, kv := range res.Attributes() {
			out["resource."+string(kv.Key)] = attrValue(kv.Value)
		}
	}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = attrValue(kv.Value)
		return true
	})
	return out
}

// attrValue renders an attribute value as plain JSON. Slices keep their items,
// maps keep their keys; anything else rides as the string the SDK prints.
func attrValue(v attribute.Value) any {
	switch v.Type() {
	case attribute.BOOL:
		return v.AsBool()
	case attribute.INT64:
		return v.AsInt64()
	case attribute.FLOAT64:
		return v.AsFloat64()
	case attribute.STRING:
		return v.AsString()
	case attribute.BOOLSLICE:
		items := v.AsBoolSlice()
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out
	case attribute.INT64SLICE:
		items := v.AsInt64Slice()
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out
	case attribute.FLOAT64SLICE:
		items := v.AsFloat64Slice()
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out
	case attribute.STRINGSLICE:
		items := v.AsStringSlice()
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out
	case attribute.BYTESLICE:
		return v.AsByteSlice()
	case attribute.SLICE:
		items := v.AsSlice()
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = attrValue(item)
		}
		return out
	case attribute.MAP:
		out := map[string]any{}
		for _, kv := range v.AsMap() {
			out[string(kv.Key)] = attrValue(kv.Value)
		}
		return out
	default:
		return v.String()
	}
}
