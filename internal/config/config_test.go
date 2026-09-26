// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package config

import (
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		HTTPPort:        "8080",
		AdminPort:       "8081",
		MaxBodyBytes:    65536,
		MaxJSONDepth:    32,
		MaxArrayItems:   512,
		RateLimitRPS:    20,
		RateLimitBurst:  40,
		QueueSize:       2048,
		ExportTimeout:   10 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		BatchTimeout:    5 * time.Second,
	}
}

func TestValidateAcceptsSaneValues(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
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
		"burst":   func(c *Config) { c.RateLimitBurst = 0 },
		"rps":     func(c *Config) { c.RateLimitRPS = -1 },
		"http":    func(c *Config) { c.HTTPPort = "notaport" },
		"admin":   func(c *Config) { c.AdminPort = "80" },
		"body":    func(c *Config) { c.MaxBodyBytes = 0 },
		"depth":   func(c *Config) { c.MaxJSONDepth = 0 },
		"array":   func(c *Config) { c.MaxArrayItems = -3 },
		"queue":   func(c *Config) { c.QueueSize = 0 },
		"timeout": func(c *Config) { c.ExportTimeout = 0 },
		"proxy":   func(c *Config) { c.TrustedProxyCIDRs = []string{"not-a-cidr"} },
	}
	for name, mutate := range cases {
		cfg := validConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: a footgun value passed validation", name)
		}
	}
}
