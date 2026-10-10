// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package guard

import (
	"fmt"
	"sync/atomic"
	"testing"

	"golang.org/x/time/rate"
)

// BenchmarkLimiterAllowSameIP measures one bucket under goroutine pressure.
func BenchmarkLimiterAllowSameIP(b *testing.B) {
	l := NewLimiter(1e9, 1e9)
	defer l.Close()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow("127.0.0.1")
		}
	})
}

// BenchmarkLimiterAllowManyIPs measures the insert and eviction path.
func BenchmarkLimiterAllowManyIPs(b *testing.B) {
	l := NewLimiter(1e9, 1e9)
	var n atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := n.Add(1)
			l.Allow(fmt.Sprintf("10.%d.%d.%d", byte(i>>16), byte(i>>8), byte(i)))
		}
	})
}

// BenchmarkLimiterAllowAtTableCeiling measures what a flood leaves behind: a
// full table and a returning client, so an Allow does no insert and no
// evictOne, and the only work beyond the bucket is the amortized idle scan.
func BenchmarkLimiterAllowAtTableCeiling(b *testing.B) {
	l := NewLimiter(1e9, 1e9)
	defer l.Close()
	for i := range defaultMaxClients {
		l.Allow(fmt.Sprintf("10.%d.%d.%d", byte(i>>16), byte(i>>8), byte(i)))
	}
	if got := l.Clients(); got != defaultMaxClients {
		b.Fatalf("table holds %d buckets, want %d", got, defaultMaxClients)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow("10.0.0.1")
		}
	})
}

// BenchmarkBucketAlone is the floor: one token bucket, no table, no lock of ours.
func BenchmarkBucketAlone(b *testing.B) {
	l := rate.NewLimiter(rate.Limit(1e9), 1e9)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow()
		}
	})
}
