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

// ErrTooManyKeys is returned when a JSON body holds more object keys than the
// cap. Every key becomes one log attribute, so the key count bounds the
// record, the way the array cap bounds the batch.
var ErrTooManyKeys = errors.New("json body holds more keys than the configured cap")

// Limits are the caps every accepted body must satisfy.
type Limits struct {
	MaxBodyBytes  int64
	MaxJSONDepth  int
	MaxArrayItems int
	MaxBodyKeys   int
}

// Limiter is a per-client-IP token bucket. The table is bounded and eviction
// is amortized, so rotating source addresses costs memory, not latency. One
// mutex guards the table: measured at ~200ns per Allow against an ~18µs
// intake handler, so sharding would buy complexity, not throughput.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*client
	rate    rate.Limit
	burst   int
	ttl     time.Duration
	ops     uint64
	max     int
}

// defaultMaxClients bounds the bucket table. Past it an insert evicts the
// least-recently-seen of a small sample, so a scanner trades its own stale
// buckets for another's and a persistent offender keeps its spent budget.
const defaultMaxClients = 10000

// evictSample bounds one eviction scan. Eight entries approximate oldest-first
// closely enough to protect an active client, and cheaply enough that a flood
// of distinct addresses cannot turn the scan itself into the bottleneck.
const evictSample = 8

// evictEvery amortizes the idle scan over this many requests. One scan is
// O(n) under the lock; one scan per 64 requests is not.
const evictEvery = 64

type client struct {
	lim    *rate.Limiter
	seenAt time.Time
}

// NewLimiter returns a limiter allowing rps requests per second per client,
// with burst as the bucket depth. Nonsense in, safety out: a zero burst would
// refuse the world and a negative rate would admit it, so both are clamped.
func NewLimiter(rps float64, burst int) *Limiter {
	if burst < 1 {
		burst = 1
	}
	if rps < 0 {
		rps = 0
	}
	return &Limiter{
		buckets: make(map[string]*client),
		rate:    rate.Limit(rps),
		burst:   burst,
		ttl:     time.Duration(float64(time.Second) / max(rps, 0.001) * float64(burst) * 2),
		max:     defaultMaxClients,
	}
}

// Allow reports whether the next request from ip may proceed.
func (l *Limiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops++
	if l.ops%evictEvery == 0 {
		l.evict(now)
	}
	c, ok := l.buckets[ip]
	if !ok {
		if len(l.buckets) >= l.max {
			l.evictOne()
		}
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

// evictOne drops the least-recently-seen entry of a small sample to make
// room. Sampling keeps the scan O(1) under the lock during a flood, while a
// client seen recently is almost never the oldest of the sample.
func (l *Limiter) evictOne() {
	victim := ""
	var oldest time.Time
	n := 0
	for ip, c := range l.buckets {
		if n == 0 || c.seenAt.Before(oldest) {
			victim, oldest = ip, c.seenAt
		}
		if n++; n >= evictSample {
			break
		}
	}
	delete(l.buckets, victim)
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

// ParseTrustedProxies parses comma-split CIDRs and bare IPs into networks a proxy peer may come from.
func ParseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, c := range cidrs {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		if ip := net.ParseIP(c); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", c, err)
		}
		out = append(out, network)
	}
	return out, nil
}

// isTrusted reports whether host is an IP inside one of the trusted networks.
func isTrusted(host string, trusted []*net.IPNet) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	for _, network := range trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteHost splits the socket address, keeping the raw value when it has no port.
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ClientIP returns the socket peer, or the nearest untrusted forwarded hop when the peer itself is a trusted proxy.
func ClientIP(r *http.Request, trustProxy bool, trusted []*net.IPNet) string {
	peer := remoteHost(r)
	if !trustProxy || !isTrusted(peer, trusted) {
		return peer
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if net.ParseIP(hop) == nil {
			continue
		}
		if !isTrusted(hop, trusted) {
			return hop
		}
	}
	return peer
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
	for token := range strings.SplitSeq(strings.ToLower(r.Header.Get("Content-Encoding")), ",") {
		if t := strings.TrimSpace(token); t == "gzip" || t == "x-gzip" {
			return true
		}
	}
	return false
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
// reflection over the decoded value. Keys are counted across the whole body:
// one body key becomes one log attribute, so the total count is the bound.
func checkShape(body []byte, limits Limits) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	depth := 0
	keys := 0
	var stack []frame
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
				if depth > limits.MaxJSONDepth {
					return ErrTooDeep
				}
				stack = append(stack, frame{object: delim == '{', key: true})
			case '}', ']':
				stack = popFrame(stack)
			}
			continue
		}
		top := topFrame(stack)
		if top == nil || !top.object {
			continue
		}
		if _, ok := tok.(string); ok {
			if top.key {
				keys++
				if keys > limits.MaxBodyKeys {
					return fmt.Errorf("body holds more than %d keys: %w", limits.MaxBodyKeys, ErrTooManyKeys)
				}
				top.key = false
			} else {
				top.key = true
			}
			continue
		}
		top.key = true
	}
}

// frame is one open container in the shape walk: whether it takes keys, and
// whether the next string token is one.
type frame struct {
	object bool
	key    bool
}

// topFrame returns the innermost open container, or nil outside any.
func topFrame(stack []frame) *frame {
	if len(stack) == 0 {
		return nil
	}
	return &stack[len(stack)-1]
}

// popFrame closes the innermost container. A closed value completes its
// parent's key, so the parent expects a key (in an object) next.
func popFrame(stack []frame) []frame {
	if len(stack) == 0 {
		return stack
	}
	stack = stack[:len(stack)-1]
	if top := topFrame(stack); top != nil && top.object {
		top.key = true
	}
	return stack
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
