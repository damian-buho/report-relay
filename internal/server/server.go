// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Package server is the HTTP surface: the public intake and the admin port.
package server

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"kiota.ch/damian-buho/report-relay/internal/config"
	"kiota.ch/damian-buho/report-relay/internal/guard"
	"kiota.ch/damian-buho/report-relay/internal/intake"
	"kiota.ch/damian-buho/report-relay/internal/telemetry"
)

// Timeouts returns the read, write and idle deadlines for a listener. The read
// deadline is generous enough for a slow sender but short enough that a stalled
// upload cannot hold a connection open indefinitely.
func Timeouts() (read, write, idle time.Duration) {
	return 15 * time.Second, 10 * time.Second, 120 * time.Second
}

// Server holds the handlers of both listeners and the guards they share.
type Server struct {
	cfg      config.Config
	log      *slog.Logger
	emitter  *telemetry.Emitter
	limiter  *guard.Limiter
	limits   guard.Limits
	handlers map[string]http.HandlerFunc
	ready    func() bool
}

// New builds the intake and admin handlers.
func New(cfg config.Config, log *slog.Logger, emitter *telemetry.Emitter, ready func() bool) *Server {
	s := &Server{
		cfg:     cfg,
		log:     log,
		emitter: emitter,
		limiter: guard.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		limits: guard.Limits{
			MaxBodyBytes:  cfg.MaxBodyBytes,
			MaxJSONDepth:  cfg.MaxJSONDepth,
			MaxArrayItems: cfg.MaxArrayItems,
		},
		ready: ready,
	}
	s.handlers = map[string]http.HandlerFunc{
		intake.MediaReportingAPI: s.intake(intake.SourceReportingAPI, cfg.ReportingAPIOn),
		intake.MediaCSPReport:    s.intake(intake.SourceCSP, cfg.CSPOn),
		intake.MediaTLSRPTJSON:   s.intake(intake.SourceTLSRPT, cfg.TLSRPTOn),
		intake.MediaTLSRPTGzip:   s.intake(intake.SourceTLSRPT, cfg.TLSRPTOn),
	}
	return s
}

// IntakeHandler returns the public intake mux.
func (s *Server) IntakeHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /", s.handlePreflight)
	mux.HandleFunc("POST /", s.recover(s.handleIntake))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeCORS(w, r)
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	return mux
}

// recover turns a panicking decode into a 500 instead of a dead process. The
// intake parses attacker bytes on every request; insurance is cheap here.
func (s *Server) recover(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.emitter.CountDropped(r.Context(), telemetry.ReasonInvalid, "")
				s.log.Error("intake panic recovered", "panic", rec)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}

// AdminHandler returns the admin mux: process health and exporter readiness.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.ready != nil && !s.ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("exporter unreachable\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// handlePreflight answers the CORS preflight a browser sends before a
// cross-origin report POST. Reporting is cross-origin by nature, so an unset
// allow-list means any origin; setting one narrows it.
func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	s.writeCORS(w, r)
	origin := r.Header.Get("Origin")
	if origin != "" && !s.originAllowed(origin) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
		w.Header().Set("Access-Control-Allow-Headers", requested)
	} else {
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" || !s.originAllowed(origin) {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Add("Vary", "Origin")
}

func (s *Server) originAllowed(origin string) bool {
	if len(s.cfg.AllowedOrigins) == 0 || slices.Contains(s.cfg.AllowedOrigins, "*") {
		return true
	}
	return slices.Contains(s.cfg.AllowedOrigins, origin)
}

// handleIntake routes on the media type and lets each format's handler apply
// the guards it needs. The rate limit runs before the route lookup, so garbage
// content types cost a token like everything else instead of bypassing it.
func (s *Server) handleIntake(w http.ResponseWriter, r *http.Request) {
	s.writeCORS(w, r)
	clientIP := guard.ClientIP(r, s.cfg.TrustProxy)
	if !s.limiter.Allow(clientIP) {
		s.emitter.CountDropped(r.Context(), telemetry.ReasonRateLimited, "")
		s.log.Warn("rate limited", "client_ip", clientIP,
			"rate_limit_rps", s.cfg.RateLimitRPS, "burst", s.cfg.RateLimitBurst)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	mediaType := intake.MediaType(r.Header.Get("Content-Type"))
	handler, ok := s.handlers[mediaType]
	if !ok {
		s.emitter.CountDropped(r.Context(), telemetry.ReasonUnsupported, "")
		s.log.Warn("unsupported content type", "content_type", mediaType,
			"client_ip", clientIP)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	handler(w, r)
}

// intake returns the handler for one wire format. A disabled intake answers 503
// rather than 404, so an operator who switched it off can tell that from a
// wrong URL.
func (s *Server) intake(source string, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaType := intake.MediaType(r.Header.Get("Content-Type"))
		if !enabled {
			s.emitter.CountDropped(r.Context(), telemetry.ReasonDisabled, "")
			s.log.Warn("intake disabled", "source", source, "media_type", mediaType)
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		clientIP := guard.ClientIP(r, s.cfg.TrustProxy)
		body, err := guard.ReadBody(r, s.cfg.MaxBodyBytes)
		if err != nil {
			reason := telemetry.ReasonInvalid
			if errors.Is(err, guard.ErrTooLarge) {
				reason = telemetry.ReasonTooLarge
			}
			s.emitter.CountDropped(r.Context(), reason, "")
			s.log.Warn("body rejected", "reason", reason, "source", source,
				"client_ip", clientIP, "max_body_bytes", s.cfg.MaxBodyBytes, "error", err)
			writeGuardError(w, reason)
			return
		}
		reports, err := intake.Decode(mediaType, body, s.limits, s.cfg.KeepQuery)
		if err != nil {
			reason := telemetry.ReasonInvalid
			switch {
			case errors.Is(err, guard.ErrTooLarge),
				errors.Is(err, guard.ErrTooDeep),
				errors.Is(err, guard.ErrArrayTooLong):
				reason = telemetry.ReasonTooLarge
			case errors.Is(err, intake.ErrInvalidReport):
				reason = telemetry.ReasonSchema
			case errors.Is(err, intake.ErrNoReports), errors.Is(err, intake.ErrUnsupportedType):
				reason = telemetry.ReasonUnsupported
			}
			s.emitter.CountDropped(r.Context(), reason, "")
			s.log.Warn("report rejected", "reason", reason, "source", source,
				"client_ip", clientIP, "bytes", len(body), "error", err)
			writeGuardError(w, reason)
			return
		}
		for _, report := range reports {
			s.emitter.CountReceived(r.Context(), report.Type, report.Domain)
			if s.emitter.Emit(r.Context(), report) {
				s.emitter.CountAccepted(r.Context(), report.Type, report.Domain)
			}
		}
		s.log.Debug("reports accepted", "source", source, "client_ip", clientIP,
			"reports", len(reports), "bytes", len(body))
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeGuardError(w http.ResponseWriter, reason string) {
	switch reason {
	case telemetry.ReasonTooLarge:
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	case telemetry.ReasonUnsupported, telemetry.ReasonSchema:
		w.WriteHeader(http.StatusUnprocessableEntity)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}
