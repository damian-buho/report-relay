// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Package server is the HTTP surface: the public intake and the admin port.
package server

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
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
	cfg         config.Config
	log         *slog.Logger
	emitter     *telemetry.Emitter
	limiter     *guard.Limiter
	typoLimiter *guard.Limiter
	typoDedup   *guard.Dedup
	limits      guard.Limits
	trusted     []*net.IPNet
	handlers    map[string]http.HandlerFunc
	ready       func() bool
}

// New builds the intake and admin handlers.
func New(cfg config.Config, log *slog.Logger, emitter *telemetry.Emitter, ready func() bool) *Server {
	trusted, err := guard.ParseTrustedProxies(cfg.TrustedProxyCIDRs)
	if err != nil {
		log.Warn("bad trusted proxies, trusting none", "error", err)
	}
	s := &Server{
		cfg:         cfg,
		log:         log,
		emitter:     emitter,
		limiter:     guard.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		typoLimiter: newTypoLimiter(cfg),
		typoDedup:   newTypoDedup(cfg),
		limits: guard.Limits{
			MaxBodyBytes:  cfg.MaxBodyBytes,
			MaxJSONDepth:  cfg.MaxJSONDepth,
			MaxArrayItems: cfg.MaxArrayItems,
			MaxBodyKeys:   cfg.MaxBodyKeys,
		},
		trusted: trusted,
		ready:   ready,
	}
	s.handlers = map[string]http.HandlerFunc{
		intake.MediaReportingAPI: s.intake(intake.SourceReportingAPI, cfg.ReportingAPIOn),
		intake.MediaCSPReport:    s.intake(intake.SourceCSP, cfg.CSPOn),
		intake.MediaTLSRPTJSON:   s.intake(intake.SourceTLSRPT, cfg.TLSRPTOn),
		intake.MediaTLSRPTGzip:   s.intake(intake.SourceTLSRPT, cfg.TLSRPTOn),
		intake.MediaExpectCT:     s.intake(intake.SourceExpectCT, cfg.ExpectCTOn),
		intake.MediaHPKP:         s.intake(intake.SourceHPKP, cfg.HPKPOn),
		intake.MediaIODEF:        s.intake(intake.SourceIODEF, cfg.IODEFOn),
		intake.MediaXML:          s.intake(intake.SourceIODEF, cfg.IODEFOn),
		intake.MediaTextXML:      s.intake(intake.SourceIODEF, cfg.IODEFOn),
	}
	return s
}

// Close releases what no request owns: the rate limiter's idle sweep, which would otherwise tick for a stopped listener.
func (s *Server) Close() {
	s.limiter.Close()
	if s.typoLimiter != nil {
		s.typoLimiter.Close()
	}
}

func newTypoLimiter(cfg config.Config) *guard.Limiter {
	rps := cfg.TypoRateLimitRPS
	if rps <= 0 && cfg.TypoRateLimitPerHour > 0 {
		rps = float64(cfg.TypoRateLimitPerHour) / 3600.0
	}
	burst := cfg.TypoRateLimitBurst
	if burst < 1 {
		burst = 5
	}
	return guard.NewLimiter(rps, burst)
}

func newTypoDedup(cfg config.Config) *guard.Dedup {
	if !cfg.TypoDedupOn || cfg.TypoDedupSize <= 0 {
		return nil
	}
	ttl := cfg.TypoDedupTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return guard.NewDedup(cfg.TypoDedupSize, ttl)
}

func typoCommentMaxURLs(cfg config.Config) int {
	for _, v := range []int{cfg.TypoCommentMaxURLs, cfg.TypoMaxCommentURLs, cfg.TypoMaxURLs} {
		if v != 0 {
			return v
		}
	}
	return cfg.TypoCommentMaxURLs
}

func countURLs(s string) int {
	lower := strings.ToLower(s)
	return strings.Count(lower, "http://") + strings.Count(lower, "https://")
}

func typoDedupKey(report intake.Report) string {
	hostPath := report.URL
	if u, err := url.Parse(report.URL); err == nil {
		hostPath = u.Host + u.Path
	}
	selection, _ := report.Body[intake.FieldSelection].(string)
	prefix, _ := report.Body[intake.FieldPrefix].(string)
	suffix, _ := report.Body[intake.FieldSuffix].(string)
	return strings.Join([]string{hostPath, selection, prefix, suffix}, "\x00")
}

// IntakeHandler returns the public intake mux.
func (s *Server) IntakeHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /", s.recover(s.handlePreflight))
	mux.HandleFunc("POST /", s.recover(s.handleIntake))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeCORS(w, r)
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	return mux
}

// recover turns a panicking handler into a 500 instead of a dead process: both intake routes parse attacker bytes on every request.
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
	// Unguarded on purpose: no request-derived input reaches either handler.
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
	// A preflight costs a token like a report POST: without one, a client that skips the 600s cache floods the verb the limiter never sees.
	if _, allowed := s.allowClient(w, r); !allowed {
		return
	}
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

// allowClient resolves the caller and spends one rate token, answering 429 with Retry-After itself when the token is refused; only the POST path counts the drop, because a preflight carries no report.
func (s *Server) allowClient(w http.ResponseWriter, r *http.Request) (string, bool) {
	clientIP := guard.ClientIP(r, s.cfg.TrustProxy, s.trusted)
	if !s.limiter.Allow(clientIP) {
		s.log.Warn("rate limited", "client_ip", clientIP,
			"type", "exploit", "reason", telemetry.ReasonRateLimited, "origin", r.Header.Get("Origin"),
			"user_agent", r.Header.Get("User-Agent"), "content_type", r.Header.Get("Content-Type"),
			"rate_limit_rps", s.cfg.RateLimitRPS, "burst", s.cfg.RateLimitBurst)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return clientIP, false
	}
	return clientIP, true
}

// handleIntake routes on the media type and lets each format's handler apply
// the guards it needs. The rate limit runs before the route lookup, so garbage
// content types cost a token like everything else instead of bypassing it.
func (s *Server) handleIntake(w http.ResponseWriter, r *http.Request) {
	s.writeCORS(w, r)
	clientIP, allowed := s.allowClient(w, r)
	if !allowed {
		s.emitter.CountDropped(r.Context(), telemetry.ReasonRateLimited, "")
		return
	}
	mediaType := intake.MediaType(r.Header.Get("Content-Type"))
	handler, ok := s.handlers[mediaType]
	if !ok {
		s.emitter.CountDropped(r.Context(), telemetry.ReasonUnsupported, "")
		s.log.Warn("unsupported content type", "content_type", mediaType,
			"client_ip", clientIP, "type", "exploit", "origin", r.Header.Get("Origin"),
			"user_agent", r.Header.Get("User-Agent"))
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
		clientIP := guard.ClientIP(r, s.cfg.TrustProxy, s.trusted)
		body, err := guard.ReadBody(r, s.cfg.MaxBodyBytes)
		if err != nil {
			reason := telemetry.ReasonInvalid
			if errors.Is(err, guard.ErrTooLarge) {
				reason = telemetry.ReasonTooLarge
			}
			s.emitter.CountDropped(r.Context(), reason, "")
			s.log.Warn("body rejected", "reason", reason, "source", source,
				"client_ip", clientIP, "type", "exploit", "origin", r.Header.Get("Origin"),
				"user_agent", r.Header.Get("User-Agent"), "max_body_bytes", s.cfg.MaxBodyBytes, "error", err)
			writeGuardError(w, reason)
			return
		}
		reports, err := intake.Decode(mediaType, body, s.limits, s.cfg.KeepQuery, s.cfg.KeepQueryTypes)
		if err != nil {
			reason := telemetry.ReasonInvalid
			switch {
			case errors.Is(err, guard.ErrTooLarge),
				errors.Is(err, guard.ErrTooDeep),
				errors.Is(err, guard.ErrTooManyKeys),
				errors.Is(err, guard.ErrArrayTooLong):
				reason = telemetry.ReasonTooLarge
			case errors.Is(err, intake.ErrDisabled):
				reason = telemetry.ReasonDisabled
			case errors.Is(err, intake.ErrBadURL):
				reason = telemetry.ReasonInvalid
			case errors.Is(err, intake.ErrInvalidReport):
				reason = telemetry.ReasonSchema
			case errors.Is(err, intake.ErrNoReports), errors.Is(err, intake.ErrUnsupportedType):
				reason = telemetry.ReasonUnsupported
			}
			s.emitter.CountDropped(r.Context(), reason, "")
			s.log.Warn("report rejected", "reason", reason, "source", source,
				"client_ip", clientIP, "type", "exploit", "origin", r.Header.Get("Origin"),
				"user_agent", r.Header.Get("User-Agent"), "bytes", len(body), "error", err)
			writeGuardError(w, reason)
			return
		}
		// A second, much stricter budget applies to the typo intake alone, so a human report flood cannot exhaust the shared per-IP bucket the other report types need.
		hasTypo := false
		for _, report := range reports {
			if report.Type == intake.TypeTypo {
				hasTypo = true
				break
			}
		}
		if hasTypo && s.typoLimiter != nil && !s.typoLimiter.Allow(clientIP) {
			s.emitter.CountDropped(r.Context(), telemetry.ReasonRateLimited, "")
			s.log.Warn("typo rate limited", "client_ip", clientIP,
				"type", "exploit", "reason", telemetry.ReasonRateLimited, "origin", r.Header.Get("Origin"),
				"user_agent", r.Header.Get("User-Agent"), "content_type", r.Header.Get("Content-Type"),
				"typo_rate_per_hour", s.cfg.TypoRateLimitPerHour, "typo_burst", s.cfg.TypoRateLimitBurst)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// A typo report's page URL must belong to the request's own origin: rejects javascript:/data: URLs and cross-site flooding; a missing Origin is rejected only when an allow-list is configured.
		origin := r.Header.Get("Origin")
		originRequired := len(s.cfg.AllowedOrigins) > 0
		for _, report := range reports {
			if report.Type != intake.TypeTypo {
				continue
			}
			if !intake.TypoURLAllowed(report.URL, origin, originRequired) {
				s.emitter.CountDropped(r.Context(), telemetry.ReasonInvalid, "")
				s.log.Warn("typo origin rejected", "client_ip", clientIP,
					"type", "exploit", "reason", telemetry.ReasonInvalid, "url", report.URL, "origin", origin,
					"user_agent", r.Header.Get("User-Agent"), "content_type", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		// Refuse comments carrying more than the configured number of URLs, so a typo report cannot be used as a link farm.
		maxURLs := typoCommentMaxURLs(s.cfg)
		if maxURLs > 0 {
			for _, report := range reports {
				if report.Type != intake.TypeTypo {
					continue
				}
				comment, _ := report.Body[intake.FieldComment].(string)
				if countURLs(comment) > maxURLs {
					s.emitter.CountDropped(r.Context(), telemetry.ReasonInvalid, "")
					s.log.Warn("typo comment has too many urls", "client_ip", clientIP,
						"type", "exploit", "reason", telemetry.ReasonInvalid, "urls", countURLs(comment), "max", maxURLs,
						"origin", origin, "user_agent", r.Header.Get("User-Agent"),
						"content_type", r.Header.Get("Content-Type"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
		}
		// Collapse repeated typo reports: a report whose (host+path, selection, prefix, suffix) was seen within the TTL is dropped and counted rather than logged.
		if s.typoDedup != nil {
			// avoid aliasing the backing array when we append
			filtered := make([]intake.Report, 0, len(reports))
			for _, report := range reports {
				if report.Type != intake.TypeTypo {
					filtered = append(filtered, report)
					continue
				}
				key := typoDedupKey(report)
				if s.typoDedup.Seen(key) {
					s.emitter.CountDropped(r.Context(), telemetry.ReasonDuplicate, "")
					s.log.Warn("typo duplicate dropped", "client_ip", clientIP,
						"type", "exploit", "reason", telemetry.ReasonDuplicate, "key", key, "origin", origin,
						"user_agent", r.Header.Get("User-Agent"), "content_type", r.Header.Get("Content-Type"))
					continue
				}
				filtered = append(filtered, report)
			}
			if len(filtered) == 0 {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			reports = filtered
		}
		for _, report := range reports {
			s.emitter.CountReceived(r.Context(), report.Type, report.Domain)
		}
		// One claim for the whole batch: a 429 must leave nothing enqueued for the sender's retry to deliver twice.
		if !s.emitter.EmitBatch(r.Context(), reports) {
			s.log.Warn("queue full, batch refused", "source", source,
				"client_ip", clientIP, "type", "exploit", "reports", len(reports))
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		for _, report := range reports {
			s.emitter.CountAccepted(r.Context(), report.Type, report.Domain)
		}
		s.log.Debug("reports accepted", "source", source, "client_ip", clientIP,
			"reports", len(reports), "bytes", len(body))
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeGuardError(w http.ResponseWriter, reason string) {
	switch reason {
	case telemetry.ReasonDisabled:
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
	case telemetry.ReasonTooLarge:
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	case telemetry.ReasonUnsupported, telemetry.ReasonSchema:
		w.WriteHeader(http.StatusUnprocessableEntity)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}
