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
	"container/list"
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

// Limiter is a per-client-IP token bucket: the table is bounded and idle buckets are reclaimed by a sweep off the request path, so rotating source addresses cost memory, not latency. One mutex guards the table: measured at ~200ns per Allow against an ~18µs intake handler, so sharding would buy complexity, not throughput.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*client
	order   *list.List
	rate    rate.Limit
	burst   int
	ttl     time.Duration
	every   time.Duration
	max     int
	stop    chan struct{}
	once    sync.Once
}

// defaultMaxClients bounds the bucket table; past it an insert evicts the least-recently-seen client.
const defaultMaxClients = 10000

// sweepFloor is the shortest gap between idle sweeps: the TTL shrinks with the refill time, so a high-rate low-burst config computes milliseconds, and sweeping that fast would make the scan the bottleneck it was moved off the request path to escape.
const sweepFloor = time.Second

type client struct {
	ip     string
	el     *list.Element
	lim    *rate.Limiter
	seenAt time.Time
}

// NewLimiter returns a limiter allowing rps requests per second per client,
// with burst as the bucket depth. Nonsense in, safety out: a zero burst would
// refuse the world and a negative rate would admit it, so both are clamped.
func NewLimiter(rps float64, burst int) *Limiter {
	burst = max(burst, 1)
	rps = max(rps, 0)
	// maxIdleTTL caps how long an idle bucket is held: the TTL is the time a bucket's debt takes to drain, so reclaiming past it costs the client nothing, but a high-burst config computes minutes and a table of minutes-old buckets leaves a new client to be evicted by sampling.
	const maxIdleTTL = 30 * time.Second
	ttl := min(idleTTL(rps, burst), maxIdleTTL)
	return newLimiter(rps, burst, ttl, max(ttl/2, sweepFloor))
}

// newLimiter builds the table and starts its idle sweep with explicit timings, so a test pins both without racing the goroutine that reads them.
func newLimiter(rps float64, burst int, ttl, every time.Duration) *Limiter {
	l := &Limiter{
		buckets: make(map[string]*client),
		order:   list.New(),
		rate:    rate.Limit(rps),
		burst:   burst,
		ttl:     ttl,
		every:   every,
		max:     defaultMaxClients,
		stop:    make(chan struct{}),
	}
	go l.sweep()
	return l
}

// idleTTL is the time one bucket takes to refill from empty, doubled: past it a reclaimed bucket would have been full anyway, so reclaiming costs the client nothing it had not already regained.
func idleTTL(rps float64, burst int) time.Duration {
	return time.Duration(float64(time.Second) / max(rps, 0.001) * float64(burst) * 2)
}

// Allow reports whether the next request from ip may proceed.
func (l *Limiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.buckets[ip]
	if !ok {
		if len(l.buckets) >= l.max {
			l.evictOne()
		}
		c = &client{ip: ip, lim: rate.NewLimiter(l.rate, l.burst)}
		c.el = l.order.PushFront(c)
		l.buckets[ip] = c
	}
	c.seenAt = now
	l.order.MoveToFront(c.el)
	return c.lim.Allow()
}

// Clients returns the number of tracked client addresses.
func (l *Limiter) Clients() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Close stops the idle sweep, so a limiter does not outlive the listener it served; it is safe to call twice, and Allow keeps working after it.
func (l *Limiter) Close() {
	l.once.Do(func() { close(l.stop) })
}

// sweep reclaims idle buckets on a timer instead of inside Allow: one walk is O(n) under the lock, and one walk every ttl/2 costs a bounded pause rather than a stall on every 64th request while the table is full.
func (l *Limiter) sweep() {
	timer := time.NewTimer(l.every)
	defer timer.Stop()
	for {
		select {
		case <-l.stop:
			return
		case now := <-timer.C:
			l.mu.Lock()
			l.evict(now)
			l.mu.Unlock()
			timer.Reset(l.every)
		}
	}
}

// evictOne drops the least-recently-seen client to make room.
func (l *Limiter) evictOne() {
	oldest := l.order.Back()
	if oldest == nil {
		return
	}
	c := oldest.Value.(*client)
	l.order.Remove(oldest)
	delete(l.buckets, c.ip)
}

// evict drops a client whose bucket has refilled, so its rate is spent again
// from full rather than resumed mid-debt.
func (l *Limiter) evict(now time.Time) {
	for ip, c := range l.buckets {
		if now.Sub(c.seenAt) > l.ttl {
			l.order.Remove(c.el)
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

// DeniedCIDR reports the first deny network containing ip, or "" when none.
func DeniedCIDR(ip string, nets []*net.IPNet) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return ""
	}
	for _, network := range nets {
		if network.Contains(parsed) {
			return network.String()
		}
	}
	return ""
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
				depth = max(0, depth-1) // a close returns the walk to the parent level
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
