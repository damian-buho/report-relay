// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

// Package guard holds the intake guards: the body cap, the per-client rate
// limit and the JSON depth and array limits. Every guard is on by default,
// because the intake is public by design.
package guard

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ErrTooLarge is returned when a body exceeds the configured cap.
var ErrTooLarge = errors.New("body exceeds the configured cap")

// ErrTooDeep is returned when a JSON document nests deeper than the cap.
var ErrTooDeep = errors.New("json nests deeper than the configured cap")

// ErrArrayTooLong is returned when a JSON array holds more items than the cap.
var ErrArrayTooLong = errors.New("json array holds more items than the configured cap")

// Limits are the caps every accepted body must satisfy.
type Limits struct {
	MaxBodyBytes  int64
	MaxJSONDepth  int
	MaxArrayItems int
}

// Limiter is a per-client-IP token bucket. Clients are evicted once they have
// been idle long enough for the bucket to be full again, so a public intake
// does not accumulate one entry per address that ever posted.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*client
	rate    rate.Limit
	burst   int
	ttl     time.Duration
}

type client struct {
	lim    *rate.Limiter
	seenAt time.Time
}

// NewLimiter returns a limiter allowing rps requests per second per client,
// with burst as the bucket depth.
func NewLimiter(rps float64, burst int) *Limiter {
	return &Limiter{
		buckets: make(map[string]*client),
		rate:    rate.Limit(rps),
		burst:   burst,
		ttl:     time.Duration(float64(time.Second) / max(rps, 0.001) * float64(burst) * 2),
	}
}

// Allow reports whether the next request from ip may proceed.
func (l *Limiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evict(now)
	c, ok := l.buckets[ip]
	if !ok {
		c = &client{lim: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[ip] = c
	}
	c.seenAt = now
	return c.lim.Allow()
}

// Clients returns the number of tracked client addresses.
func (l *Limiter) Clients() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// evict drops a client whose bucket has refilled, so its rate is spent again
// from full rather than resumed mid-debt.
func (l *Limiter) evict(now time.Time) {
	for ip, c := range l.buckets {
		if now.Sub(c.seenAt) > l.ttl {
			delete(l.buckets, ip)
		}
	}
}

// ClientIP returns the address the request came from. A forwarded header is
// honoured only when the operator declared the intake to sit behind a proxy
// they control, because an unverified header lets a caller pick its own bucket.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if first, _, found := strings.Cut(fwd, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(fwd)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ReadBody reads at most limits.MaxBodyBytes from the request, transparently
// gunzipping a gzip-encoded body. The cap applies to the DECOMPRESSED bytes, so
// a compression bomb is bounded by the same number as a plain body.
func ReadBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if isGzip(r) {
		return readGzipBody(r, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrTooLarge
	}
	return body, nil
}

// readGzipBody caps the DECOMPRESSED stream. Capping the transferred bytes
// instead would let a body that compresses to a few hundred bytes expand past
// every limit, so the cap is applied where the parsing happens.
func readGzipBody(r *http.Request, maxBytes int64) ([]byte, error) {
	zr, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, fmt.Errorf("gzip body: %w", err)
	}
	defer zr.Close()
	body, err := io.ReadAll(io.LimitReader(zr, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("gzip body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrTooLarge
	}
	return body, nil
}

func isGzip(r *http.Request) bool {
	enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	return enc == "gzip" || enc == "x-gzip"
}

// Decode unmarshals a body into dst and then enforces the depth and array caps.
// encoding/json already refuses absurd nesting, so these caps are the ones an
// operator tunes, not a defence against a parser bug.
func Decode(body []byte, dst any, limits Limits) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	// The shape check reads the same bytes again: the decoder above consumed the
	// value, so its token stream is already at EOF.
	if err := checkShape(body, limits); err != nil {
		return err
	}
	return nil
}

// DecodeBody unmarshals a nested JSON fragment that the body cap already
// bounded, so it needs no further limit of its own.
func DecodeBody(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// checkShape walks the raw token stream, which costs no allocation and needs no
// reflection over the decoded value.
func checkShape(body []byte, limits Limits) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			continue
		}
		switch delim {
		case '{', '[':
			depth++
			if depth > limits.MaxJSONDepth {
				return ErrTooDeep
			}
		case '}', ']':
			depth--
		}
	}
}

// CountItems returns the number of elements in a JSON array, refusing one that
// is longer than the cap. An empty or absent array counts as zero.
func CountItems(body []byte, limits Limits) (int, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return 0, nil
	}
	count := 0
	for dec.More() {
		count++
		if count > limits.MaxArrayItems {
			return 0, ErrArrayTooLong
		}
		if err := dec.Decode(&json.RawMessage{}); err != nil {
			return 0, fmt.Errorf("decode: %w", err)
		}
	}
	return count, nil
}
