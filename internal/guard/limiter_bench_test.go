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
