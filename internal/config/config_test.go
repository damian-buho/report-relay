// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		HTTPPort:             "8080",
		AdminPort:            "8081",
		MaxBodyBytes:         65536,
		MaxJSONDepth:         32,
		MaxArrayItems:        512,
		MaxBodyKeys:          512,
		RateLimitRPS:         20,
		RateLimitBurst:       40,
		QueueSize:            2048,
		ExportTimeout:        10 * time.Second,
		ShutdownTimeout:      15 * time.Second,
		BatchTimeout:         5 * time.Second,
		ExportInitialBackoff: 500 * time.Millisecond,
		ExportMaxBackoff:     30 * time.Second,
		ExportMaxElapsed:     2 * time.Minute,
	}
}

func TestValidateAcceptsSaneValues(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestExportBudgetTakesTheLongerKnob(t *testing.T) {
	cfg := validConfig()
	if got := cfg.ExportBudget(); got != cfg.ExportMaxElapsed {
		t.Errorf("ExportBudget = %v, want the %v retry budget", got, cfg.ExportMaxElapsed)
	}
	cfg.ExportMaxElapsed = time.Second
	if got := cfg.ExportBudget(); got != cfg.ExportTimeout {
		t.Errorf("ExportBudget = %v, want the %v attempt deadline", got, cfg.ExportTimeout)
	}
}

func TestValidateAcceptsTrustedProxyNets(t *testing.T) {
	cfg := validConfig()
	cfg.TrustProxy = true
	cfg.TrustedProxyCIDRs = []string{"10.0.0.0/8", "192.0.2.1"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsFootguns(t *testing.T) {
	cases := map[string]func(*Config){
		"burst":           func(c *Config) { c.RateLimitBurst = 0 },
		"rps":             func(c *Config) { c.RateLimitRPS = -1 },
		"http":            func(c *Config) { c.HTTPPort = "notaport" },
		"admin":           func(c *Config) { c.AdminPort = "80" },
		"body":            func(c *Config) { c.MaxBodyBytes = 0 },
		"depth":           func(c *Config) { c.MaxJSONDepth = 0 },
		"array":           func(c *Config) { c.MaxArrayItems = -3 },
		"keys":            func(c *Config) { c.MaxBodyKeys = 0 },
		"queue":           func(c *Config) { c.QueueSize = 0 },
		"timeout":         func(c *Config) { c.ExportTimeout = 0 },
		"initial backoff": func(c *Config) { c.ExportInitialBackoff = 0 },
		"max backoff":     func(c *Config) { c.ExportMaxBackoff = -time.Second },
		"max elapsed":     func(c *Config) { c.ExportMaxElapsed = 0 },
		"budget":          func(c *Config) { c.ExportMaxElapsed = c.ExportInitialBackoff / 2 },
		"proxy":           func(c *Config) { c.TrustedProxyCIDRs = []string{"not-a-cidr"} },
	}
	for name, mutate := range cases {
		cfg := validConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: a footgun value passed validation", name)
		}
	}
}

// TestValidateAcceptsEveryCapAtItsFloor pins each "want >= 1" contract at the
// value it admits: a cap of exactly 1 is legal, and one step up is legal too, so
// a boundary moved by one is caught rather than argued about.
func TestValidateAcceptsEveryCapAtItsFloor(t *testing.T) {
	for _, size := range []int64{1, 2} {
		cfg := validConfig()
		cfg.RateLimitBurst = int(size)
		cfg.MaxBodyBytes = size
		cfg.MaxJSONDepth = int(size)
		cfg.MaxArrayItems = int(size)
		cfg.MaxBodyKeys = int(size)
		cfg.QueueSize = int(size)
		if err := cfg.Validate(); err != nil {
			t.Errorf("caps at %d were refused: %v", size, err)
		}
	}
}

func TestValidateAcceptsAZeroRateRefusalBoundary(t *testing.T) {
	// The contract is "want > 0": a rate of zero disables the intake, so it is
	// the value at the boundary the error text names.
	cfg := validConfig()
	cfg.RateLimitRPS = 0
	if err := cfg.Validate(); err == nil {
		t.Error("a zero rate passed validation: it refuses every request at runtime")
	}
	cfg.RateLimitRPS = 0.5
	if err := cfg.Validate(); err != nil {
		t.Errorf("the smallest positive rate was refused: %v", err)
	}
}

func TestValidateAcceptsAnExportBudgetEqualToTheBackoff(t *testing.T) {
	cfg := validConfig()
	cfg.ExportMaxElapsed = cfg.ExportInitialBackoff
	if err := cfg.Validate(); err != nil {
		t.Errorf("a budget equal to the initial backoff was refused: %v", err)
	}
	cfg.ExportMaxElapsed = cfg.ExportInitialBackoff - time.Nanosecond
	if err := cfg.Validate(); err == nil {
		t.Error("a budget one nanosecond below the initial backoff passed validation")
	}
}

// TestValidateAcceptsThePortRangeEdges pins the unprivileged range the image
// binds in: 1025 and 65535 are legal, 1024 and 65536 are not.
func TestValidateAcceptsThePortRangeEdges(t *testing.T) {
	for _, port := range []string{"1025", "65535"} {
		cfg := validConfig()
		cfg.HTTPPort = port
		cfg.AdminPort = port
		if err := cfg.Validate(); err != nil {
			t.Errorf("port %s was refused: %v", port, err)
		}
	}
	for name, mutate := range map[string]func(*Config){
		"low intake": func(c *Config) { c.HTTPPort = "1024" },
		"high admin": func(c *Config) { c.AdminPort = "65536" },
	} {
		cfg := validConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: a port outside 1025-65535 passed validation", name)
		}
	}
}

// captureWarnings redirects the default slog logger into a builder, because the
// startup warnings are part of what Validate reports to an operator.
func captureWarnings(t *testing.T) *strings.Builder {
	t.Helper()
	out := &strings.Builder{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return out
}

func TestValidateWarnsWhenTrustProxyHasNoProxyToTrust(t *testing.T) {
	out := captureWarnings(t)
	cfg := validConfig()
	cfg.TrustProxy = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !strings.Contains(out.String(), "trusted proxies") {
		t.Errorf("logs = %q, want the forwarded-headers warning", out.String())
	}
}

func TestValidateWarnsWhenTypoIntakeHasNoAllowedOrigin(t *testing.T) {
	out := captureWarnings(t)
	cfg := validConfig()
	cfg.TypoOn = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !strings.Contains(out.String(), "allowed origins") {
		t.Errorf("logs = %q, want the typo origin warning", out.String())
	}
}

func TestValidateIsQuietWhenNoFlagNeedsAWarning(t *testing.T) {
	out := captureWarnings(t)
	cfg := validConfig()
	cfg.TrustProxy = false
	cfg.TypoOn = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if out.String() != "" {
		t.Errorf("logs = %q, want no warning with both flags off", out.String())
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	for _, key := range []string{
		"REPORT_RELAY_SERVICE_NAME", "REPORT_RELAY_SERVICE_NAMESPACE",
		"REPORT_RELAY_LOG_LEVEL", "REPORT_RELAY_HTTP_PORT", "REPORT_RELAY_ADMIN_PORT",
		"REPORT_RELAY_MAX_BODY_BYTES", "REPORT_RELAY_MAX_JSON_DEPTH",
		"REPORT_RELAY_MAX_ARRAY_ITEMS", "REPORT_RELAY_MAX_BODY_KEYS",
		"REPORT_RELAY_RATE_LIMIT_RPS", "REPORT_RELAY_RATE_LIMIT_BURST",
		"REPORT_RELAY_ALLOWED_ORIGINS", "REPORT_RELAY_KEEP_QUERY",
		"REPORT_RELAY_TRUST_PROXY", "REPORT_RELAY_TRUSTED_PROXIES",
		"REPORT_RELAY_EXPORT_TIMEOUT", "REPORT_RELAY_SHUTDOWN_TIMEOUT",
		"REPORT_RELAY_ENABLE_REPORTING_API", "REPORT_RELAY_ENABLE_CSP",
		"REPORT_RELAY_ENABLE_TLSRPT", "REPORT_RELAY_ENABLE_EXPECT_CT",
		"REPORT_RELAY_ENABLE_HPKP", "REPORT_RELAY_ENABLE_IODEF",
		"REPORT_RELAY_ENABLE_TYPO", "REPORT_RELAY_QUEUE_SIZE",
		"REPORT_RELAY_BATCH_TIMEOUT", "REPORT_RELAY_EXPORT_INITIAL_BACKOFF",
		"REPORT_RELAY_EXPORT_MAX_BACKOFF", "REPORT_RELAY_EXPORT_MAX_ELAPSED",
	} {
		t.Setenv(key, "")
	}
	cfg := Load()
	if cfg.ServiceName != "report-relay" {
		t.Errorf("ServiceName = %q, want report-relay", cfg.ServiceName)
	}
	if cfg.Namespace != "me.dbuho" {
		t.Errorf("Namespace = %q, want me.dbuho", cfg.Namespace)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, want 8080", cfg.HTTPPort)
	}
	if cfg.AdminPort != "8081" {
		t.Errorf("AdminPort = %q, want 8081", cfg.AdminPort)
	}
	if cfg.MaxBodyBytes != 65536 {
		t.Errorf("MaxBodyBytes = %d, want 65536", cfg.MaxBodyBytes)
	}
	if cfg.MaxJSONDepth != 32 {
		t.Errorf("MaxJSONDepth = %d, want 32", cfg.MaxJSONDepth)
	}
	if cfg.MaxArrayItems != 512 {
		t.Errorf("MaxArrayItems = %d, want 512", cfg.MaxArrayItems)
	}
	if cfg.MaxBodyKeys != 512 {
		t.Errorf("MaxBodyKeys = %d, want 512", cfg.MaxBodyKeys)
	}
	if cfg.RateLimitRPS != 20 {
		t.Errorf("RateLimitRPS = %v, want 20", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 40 {
		t.Errorf("RateLimitBurst = %d, want 40", cfg.RateLimitBurst)
	}
	if cfg.AllowedOrigins != nil {
		t.Errorf("AllowedOrigins = %v, want nil", cfg.AllowedOrigins)
	}
	if cfg.KeepQuery {
		t.Error("KeepQuery = true, want false")
	}
	if cfg.TrustProxy {
		t.Error("TrustProxy = true, want false")
	}
	if cfg.TrustedProxyCIDRs != nil {
		t.Errorf("TrustedProxyCIDRs = %v, want nil", cfg.TrustedProxyCIDRs)
	}
	if cfg.ExportTimeout != 10*time.Second {
		t.Errorf("ExportTimeout = %v, want 10s", cfg.ExportTimeout)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
	if !cfg.ReportingAPIOn {
		t.Error("ReportingAPIOn = false, want true")
	}
	if !cfg.CSPOn {
		t.Error("CSPOn = false, want true")
	}
	if !cfg.TLSRPTOn {
		t.Error("TLSRPTOn = false, want true")
	}
	if !cfg.ExpectCTOn {
		t.Error("ExpectCTOn = false, want true")
	}
	if !cfg.HPKPOn {
		t.Error("HPKPOn = false, want true")
	}
	if !cfg.IODEFOn {
		t.Error("IODEFOn = false, want true")
	}
	if !cfg.TypoOn {
		t.Error("TypoOn = false, want true")
	}
	if cfg.QueueSize != 2048 {
		t.Errorf("QueueSize = %d, want 2048", cfg.QueueSize)
	}
	if cfg.BatchTimeout != 5*time.Second {
		t.Errorf("BatchTimeout = %v, want 5s", cfg.BatchTimeout)
	}
	if cfg.ExportInitialBackoff != 500*time.Millisecond {
		t.Errorf("ExportInitialBackoff = %v, want 500ms", cfg.ExportInitialBackoff)
	}
	if cfg.ExportMaxBackoff != 30*time.Second {
		t.Errorf("ExportMaxBackoff = %v, want 30s", cfg.ExportMaxBackoff)
	}
	if cfg.ExportMaxElapsed != 2*time.Minute {
		t.Errorf("ExportMaxElapsed = %v, want 2m", cfg.ExportMaxElapsed)
	}
}

func TestLoadReadsEnvVars(t *testing.T) {
	t.Setenv("REPORT_RELAY_SERVICE_NAME", "custom-relay")
	t.Setenv("REPORT_RELAY_HTTP_PORT", "9090")
	t.Setenv("REPORT_RELAY_RATE_LIMIT_RPS", "100")
	t.Setenv("REPORT_RELAY_KEEP_QUERY", "true")
	t.Setenv("REPORT_RELAY_ENABLE_CSP", "false")
	t.Setenv("REPORT_RELAY_ALLOWED_ORIGINS", "https://a.example, https://b.example")
	t.Setenv("REPORT_RELAY_EXPORT_TIMEOUT", "30s")
	t.Setenv("REPORT_RELAY_LOG_LEVEL", "debug")
	cfg := Load()
	if cfg.ServiceName != "custom-relay" {
		t.Errorf("ServiceName = %q, want custom-relay", cfg.ServiceName)
	}
	if cfg.HTTPPort != "9090" {
		t.Errorf("HTTPPort = %q, want 9090", cfg.HTTPPort)
	}
	if cfg.RateLimitRPS != 100 {
		t.Errorf("RateLimitRPS = %v, want 100", cfg.RateLimitRPS)
	}
	if !cfg.KeepQuery {
		t.Error("KeepQuery = false, want true")
	}
	if cfg.CSPOn {
		t.Error("CSPOn = true, want false")
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://a.example" || cfg.AllowedOrigins[1] != "https://b.example" {
		t.Errorf("AllowedOrigins = %v, want two entries", cfg.AllowedOrigins)
	}
	if cfg.ExportTimeout != 30*time.Second {
		t.Errorf("ExportTimeout = %v, want 30s", cfg.ExportTimeout)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestLoadFallsBackOnInvalidValues(t *testing.T) {
	t.Setenv("REPORT_RELAY_RATE_LIMIT_RPS", "notanumber")
	t.Setenv("REPORT_RELAY_KEEP_QUERY", "notabool")
	t.Setenv("REPORT_RELAY_EXPORT_TIMEOUT", "notaduration")
	t.Setenv("REPORT_RELAY_LOG_LEVEL", "notalevel")
	cfg := Load()
	if cfg.RateLimitRPS != 20 {
		t.Errorf("RateLimitRPS = %v, want default 20", cfg.RateLimitRPS)
	}
	if cfg.KeepQuery {
		t.Error("KeepQuery = true, want default false")
	}
	if cfg.ExportTimeout != 10*time.Second {
		t.Errorf("ExportTimeout = %v, want default 10s", cfg.ExportTimeout)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want default info", cfg.LogLevel)
	}
}

func TestLoadPassesThroughInvalidStringValues(t *testing.T) {
	t.Setenv("REPORT_RELAY_HTTP_PORT", "notaport")
	cfg := Load()
	if cfg.HTTPPort != "notaport" {
		t.Errorf("HTTPPort = %q, want notaport (envOr does not validate)", cfg.HTTPPort)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("TEST_OR", "value")
	if got := envOr("TEST_OR", "fallback"); got != "value" {
		t.Errorf("envOr = %q, want value", got)
	}
	t.Setenv("TEST_OR", "")
	if got := envOr("TEST_OR", "fallback"); got != "fallback" {
		t.Errorf("envOr = %q, want fallback", got)
	}
}

func TestEnvIntOr(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	if got := envIntOr("TEST_INT", 10); got != 42 {
		t.Errorf("envIntOr = %d, want 42", got)
	}
	t.Setenv("TEST_INT", "garbage")
	if got := envIntOr("TEST_INT", 10); got != 10 {
		t.Errorf("envIntOr = %d, want 10", got)
	}
}

func TestEnvFloatOr(t *testing.T) {
	t.Setenv("TEST_FLOAT", "3.5")
	if got := envFloatOr("TEST_FLOAT", 1.5); got != 3.5 {
		t.Errorf("envFloatOr = %v, want 3.5", got)
	}
	t.Setenv("TEST_FLOAT", "garbage")
	if got := envFloatOr("TEST_FLOAT", 1.5); got != 1.5 {
		t.Errorf("envFloatOr = %v, want 1.5", got)
	}
}

func TestEnvBoolOr(t *testing.T) {
	t.Setenv("TEST_BOOL", "true")
	if !envBoolOr("TEST_BOOL", false) {
		t.Error("envBoolOr = false, want true")
	}
	t.Setenv("TEST_BOOL", "garbage")
	if envBoolOr("TEST_BOOL", false) {
		t.Error("envBoolOr = true, want false")
	}
}

func TestEnvDurationOr(t *testing.T) {
	t.Setenv("TEST_DUR", "5s")
	if got := envDurationOr("TEST_DUR", time.Second); got != 5*time.Second {
		t.Errorf("envDurationOr = %v, want 5s", got)
	}
	t.Setenv("TEST_DUR", "garbage")
	if got := envDurationOr("TEST_DUR", time.Second); got != time.Second {
		t.Errorf("envDurationOr = %v, want 1s", got)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo,
		"warn": slog.LevelWarn, "error": slog.LevelError,
		"DEBUG": slog.LevelDebug, "INFO": slog.LevelInfo,
		"WARN": slog.LevelWarn, "ERROR": slog.LevelError,
	}
	for input, want := range cases {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseLevelFallsBack(t *testing.T) {
	if got := parseLevel("garbage"); got != slog.LevelInfo {
		t.Errorf("parseLevel = %v, want info", got)
	}
}

func TestSplitList(t *testing.T) {
	cases := map[string][]string{
		"":            nil,
		"   ":         nil,
		"a":           {"a"},
		"a,b,c":       {"a", "b", "c"},
		" a , b , c ": {"a", "b", "c"},
		"a,,b":        {"a", "b"},
	}
	for input, want := range cases {
		got := splitList(input)
		if len(got) != len(want) {
			t.Errorf("splitList(%q) = %v, want %v", input, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitList(%q) = %v, want %v", input, got, want)
				break
			}
		}
	}
}

func TestLogEmitsConfiguration(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	validConfig().Log(logger)
	out := buf.String()
	for _, key := range []string{
		"service_name", "http_port", "admin_port", "rate_limit_rps",
		"export_timeout", "queue_size", "reporting_api",
	} {
		if !strings.Contains(out, key) {
			t.Errorf("Log output missing %q", key)
		}
	}
}
