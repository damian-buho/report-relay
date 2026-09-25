// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Package config reads the REPORT_RELAY_* environment into the values the rest
// of the service uses. The OTLP exporter reads the standard OTEL_* variables
// itself, so nothing here duplicates them.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the whole effective configuration of one process.
type Config struct {
	ServiceName          string
	Namespace            string
	LogLevel             slog.Level
	HTTPPort             string
	AdminPort            string
	MaxBodyBytes         int64
	MaxJSONDepth         int
	MaxArrayItems        int
	RateLimitRPS         float64
	RateLimitBurst       int
	AllowedOrigins       []string
	KeepQuery            bool
	TrustProxy           bool
	ExportTimeout        time.Duration
	ShutdownTimeout      time.Duration
	ReportingAPIOn       bool
	CSPOn                bool
	TLSRPTOn             bool
	QueueSize            int
	BatchTimeout         time.Duration
	ExportInitialBackoff time.Duration
	ExportMaxBackoff     time.Duration
	ExportMaxElapsed     time.Duration
}

// Load reads the environment and returns the effective configuration, warning
// on every value that had to fall back to its default.
func Load() Config {
	cfg := Config{
		ServiceName:     envOr("REPORT_RELAY_SERVICE_NAME", "report-relay"),
		Namespace:       envOr("REPORT_RELAY_SERVICE_NAMESPACE", "me.dbuho"),
		LogLevel:        parseLevel(envOr("REPORT_RELAY_LOG_LEVEL", "info")),
		HTTPPort:        envOr("REPORT_RELAY_HTTP_PORT", "8080"),
		AdminPort:       envOr("REPORT_RELAY_ADMIN_PORT", "8081"),
		MaxBodyBytes:    int64(envIntOr("REPORT_RELAY_MAX_BODY_BYTES", 65536)),
		MaxJSONDepth:    envIntOr("REPORT_RELAY_MAX_JSON_DEPTH", 32),
		MaxArrayItems:   envIntOr("REPORT_RELAY_MAX_ARRAY_ITEMS", 512),
		RateLimitRPS:    envFloatOr("REPORT_RELAY_RATE_LIMIT_RPS", 20),
		RateLimitBurst:  envIntOr("REPORT_RELAY_RATE_LIMIT_BURST", 40),
		AllowedOrigins:  splitList(os.Getenv("REPORT_RELAY_ALLOWED_ORIGINS")),
		KeepQuery:       envBoolOr("REPORT_RELAY_KEEP_QUERY", false),
		TrustProxy:      envBoolOr("REPORT_RELAY_TRUST_PROXY", false),
		ExportTimeout:   envDurationOr("REPORT_RELAY_EXPORT_TIMEOUT", 10*time.Second),
		ShutdownTimeout: envDurationOr("REPORT_RELAY_SHUTDOWN_TIMEOUT", 15*time.Second),
		ReportingAPIOn:  envBoolOr("REPORT_RELAY_ENABLE_REPORTING_API", true),
		CSPOn:           envBoolOr("REPORT_RELAY_ENABLE_CSP", true),
		TLSRPTOn:        envBoolOr("REPORT_RELAY_ENABLE_TLSRPT", true),
		QueueSize:       envIntOr("REPORT_RELAY_QUEUE_SIZE", 2048),
		BatchTimeout:    envDurationOr("REPORT_RELAY_BATCH_TIMEOUT", 5*time.Second),

		ExportInitialBackoff: envDurationOr("REPORT_RELAY_EXPORT_INITIAL_BACKOFF", 500*time.Millisecond),
		ExportMaxBackoff:     envDurationOr("REPORT_RELAY_EXPORT_MAX_BACKOFF", 30*time.Second),
		ExportMaxElapsed:     envDurationOr("REPORT_RELAY_EXPORT_MAX_ELAPSED", 2*time.Minute),
	}
	cfg.Log(slog.Default())
	return cfg
}

// Log emits one line per effective value, so an operator reads the running
// configuration from the log instead of from the deployment manifest.
func (c Config) Log(log *slog.Logger) {
	log.Info("configuration",
		"service_name", c.ServiceName,
		"service_namespace", c.Namespace,
		"http_port", c.HTTPPort,
		"admin_port", c.AdminPort,
		"max_body_bytes", c.MaxBodyBytes,
		"max_json_depth", c.MaxJSONDepth,
		"max_array_items", c.MaxArrayItems,
		"rate_limit_rps", c.RateLimitRPS,
		"rate_limit_burst", c.RateLimitBurst,
		"allowed_origins", c.AllowedOrigins,
		"keep_query", c.KeepQuery,
		"trust_proxy", c.TrustProxy,
		"export_timeout", c.ExportTimeout,
		"shutdown_timeout", c.ShutdownTimeout,
		"queue_size", c.QueueSize,
		"batch_timeout", c.BatchTimeout,
		"export_initial_backoff", c.ExportInitialBackoff,
		"export_max_backoff", c.ExportMaxBackoff,
		"export_max_elapsed", c.ExportMaxElapsed,
		"reporting_api", c.ReportingAPIOn,
		"csp", c.CSPOn,
		"tlsrpt", c.TLSRPTOn,
	)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		slog.Warn("invalid env value, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func envFloatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
		slog.Warn("invalid env value, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		slog.Warn("invalid env value, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("invalid env value, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func parseLevel(v string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(v)); err != nil {
		slog.Warn("invalid log level, using default", "value", v, "default", "info")
		return slog.LevelInfo
	}
	return level
}

func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
