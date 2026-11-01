// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package guard

import (
	"container/list"
	"sync"
	"time"
)

// Dedup is a bounded, TTL-based set of recently seen keys: a key already present within its TTL is a duplicate.
type Dedup struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	now     func() time.Time
	entries map[string]*list.Element
	order   *list.List
}

type dedupEntry struct {
	key  string
	seen time.Time
}

// NewDedup returns a dedup cache holding at most size keys for ttl. A size of
// zero disables storage, so Seen always reports a new key.
func NewDedup(size int, ttl time.Duration) *Dedup {
	return NewDedupWithClock(size, ttl, time.Now)
}

// NewDedupWithClock is NewDedup with an injectable clock, so a test can move
// time forward without sleeping.
func NewDedupWithClock(size int, ttl time.Duration, now func() time.Time) *Dedup {
	if size < 0 {
		size = 0
	}
	if now == nil {
		now = time.Now
	}
	return &Dedup{
		max:     size,
		ttl:     ttl,
		now:     now,
		entries: make(map[string]*list.Element),
		order:   list.New(),
	}
}

// NewDedupCache, NewLRU and NewLRUCache are aliases for callers that name the cache differently.
func NewDedupCache(size int, ttl time.Duration) *Dedup {
	return NewDedup(size, ttl)
}

func NewDedupCacheWithClock(size int, ttl time.Duration, now func() time.Time) *Dedup {
	return NewDedupWithClock(size, ttl, now)
}

func NewLRU(size int, ttl time.Duration) *Dedup {
	return NewDedup(size, ttl)
}

func NewLRUWithClock(size int, ttl time.Duration, now func() time.Time) *Dedup {
	return NewDedupWithClock(size, ttl, now)
}

func NewLRUCache(size int, ttl time.Duration) *Dedup {
	return NewDedup(size, ttl)
}

func NewLRUCacheWithClock(size int, ttl time.Duration, now func() time.Time) *Dedup {
	return NewDedupWithClock(size, ttl, now)
}

// Seen reports whether key was seen within the TTL. A new key is recorded before
// returning, so the next call with the same key is a duplicate.
func (d *Dedup) Seen(key string) bool {
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.max <= 0 {
		return false
	}
	if el, ok := d.entries[key]; ok {
		entry := el.Value.(*dedupEntry)
		if now.Sub(entry.seen) <= d.ttl {
			d.order.MoveToFront(el)
			entry.seen = now
			return true
		}
		d.order.Remove(el)
		delete(d.entries, key)
	}
	if d.order.Len() >= d.max {
		if oldest := d.order.Back(); oldest != nil {
			d.order.Remove(oldest)
			delete(d.entries, oldest.Value.(*dedupEntry).key)
		}
	}
	d.order.PushFront(&dedupEntry{key: key, seen: now})
	d.entries[key] = d.order.Front()
	return false
}

// Add records key and reports whether it was newly added.
func (d *Dedup) Add(key string) bool {
	return !d.Seen(key)
}

// Contains reports whether key is present and unexpired without recording it.
func (d *Dedup) Contains(key string) bool {
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	el, ok := d.entries[key]
	if !ok {
		return false
	}
	return now.Sub(el.Value.(*dedupEntry).seen) <= d.ttl
}

// Allow reports whether key may proceed, recording it when allowed.
func (d *Dedup) Allow(key string) bool {
	return !d.Seen(key)
}

// IsDuplicate is an alias for Seen.
func (d *Dedup) IsDuplicate(key string) bool {
	return d.Seen(key)
}

// Check is an alias for Seen.
func (d *Dedup) Check(key string) bool {
	return d.Seen(key)
}
