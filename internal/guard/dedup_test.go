// SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
//
// SPDX-License-Identifier: MIT

package guard

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a movable time source, so a TTL test advances without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestNewDedupAliasesShareOneImplementation(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	built := []*Dedup{
		NewDedup(4, time.Minute),
		NewDedupCache(4, time.Minute),
		NewLRU(4, time.Minute),
		NewLRUCache(4, time.Minute),
		NewDedupWithClock(4, time.Minute, clock.Now),
		NewDedupCacheWithClock(4, time.Minute, clock.Now),
		NewLRUWithClock(4, time.Minute, clock.Now),
		NewLRUCacheWithClock(4, time.Minute, clock.Now),
	}
	for i, d := range built {
		if d == nil {
			t.Fatalf("constructor %d returned nil", i)
		}
		if d.max != 4 {
			t.Errorf("constructor %d: max = %d, want 4", i, d.max)
		}
		if d.ttl != time.Minute {
			t.Errorf("constructor %d: ttl = %v, want 1m", i, d.ttl)
		}
		if d.now == nil {
			t.Errorf("constructor %d: no clock installed", i)
		}
	}
}

func TestNewDedupWithClockRepairsNonsense(t *testing.T) {
	d := NewDedupWithClock(-1, time.Minute, nil)
	if d.max != 0 {
		t.Errorf("max = %d, want the negative size clamped to 0", d.max)
	}
	if d.now == nil {
		t.Fatal("a nil clock was installed, so Now panics on the first use")
	}
	base := d.now()
	if advanced := d.now(); !advanced.After(base) && advanced.Before(base) {
		t.Errorf("clock = %v, want a real time source", advanced)
	}
}

func TestSeenRecordsANewKeyAndReportsTheRepeat(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewDedupWithClock(8, time.Minute, clock.Now)
	if d.Seen("k") {
		t.Error("the first sighting reported a duplicate")
	}
	if !d.Seen("k") {
		t.Error("the second sighting of the same key was not a duplicate")
	}
	if !d.Contains("k") {
		t.Error("Contains missed a live key Seen recorded")
	}
}

// TestSeenAtTheTTLBoundaryIsStillADuplicate pins the inclusive comparison:
// exactly at the TTL the key is live, one tick past it is not.
func TestSeenAtTheTTLBoundaryIsStillADuplicate(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewDedupWithClock(8, time.Minute, clock.Now)
	d.Seen("k")
	clock.Advance(time.Minute)
	if !d.Seen("k") {
		t.Error("a key at exactly the TTL must still count as seen")
	}
}

func TestSeenForgetsAKeyPastTheTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewDedupWithClock(8, time.Minute, clock.Now)
	d.Seen("k")
	clock.Advance(time.Minute + time.Nanosecond)
	if d.Seen("k") {
		t.Error("a key one tick past the TTL was still a duplicate")
	}
	if !d.Seen("k") {
		t.Error("the expired key was not re-recorded, so its next sighting read as a duplicate")
	}
}

func TestContainsForgetsAKeyPastTheTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewDedupWithClock(8, time.Minute, clock.Now)
	d.Seen("k")
	clock.Advance(time.Minute + time.Nanosecond)
	if d.Contains("k") {
		t.Error("Contains kept an expired key alive")
	}
}

func TestDedupSizeZeroRecordsNothing(t *testing.T) {
	d := NewDedup(0, time.Minute)
	for range 3 {
		if d.Seen("k") {
			t.Error("a size of zero must disable storage, so every key is new")
		}
	}
	if d.Contains("k") {
		t.Error("Contains reported a key a disabled cache cannot hold")
	}
}

func TestDedupEvictsTheOldestEntryAtCapacity(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewDedupWithClock(2, time.Minute, clock.Now)
	for _, key := range []string{"a", "b"} {
		d.Seen(key)
	}
	if !d.Contains("b") {
		t.Fatal("the newest key is missing before the eviction")
	}
	clock.Advance(time.Second)
	d.Seen("c")
	if d.Contains("a") {
		t.Error("the oldest key survived past the size cap")
	}
	if !d.Contains("b") || !d.Contains("c") {
		t.Error("keys inside the cap were dropped: a=newest-first eviction is wrong")
	}
}

func TestDedupContainsReportsAbsentKeys(t *testing.T) {
	d := NewDedup(4, time.Minute)
	if d.Contains("never-seen") {
		t.Error("Contains reported a key that was never recorded")
	}
}

// TestDedupAllowAliasesAdmitANewKeyOnce pins the inverted polarity of the
// allow-style aliases: admitted while new, refused once Seen recorded the key.
func TestDedupAllowAliasesAdmitANewKeyOnce(t *testing.T) {
	for _, call := range []func(string) bool{NewDedup(4, time.Minute).Add, NewDedup(4, time.Minute).Allow} {
		if !call("k") {
			t.Error("the first call must admit a new key")
		}
		if call("k") {
			t.Error("the second call missed the record the first one wrote")
		}
	}
}

// TestDedupReportAliasesNameTheDuplicate pins the Seen polarity of the
// reporting aliases: the first sighting is not a duplicate, the second is.
func TestDedupReportAliasesNameTheDuplicate(t *testing.T) {
	for _, call := range []func(string) bool{NewDedup(4, time.Minute).IsDuplicate, NewDedup(4, time.Minute).Check} {
		if call("k") {
			t.Error("the first sighting must not be a duplicate")
		}
		if !call("k") {
			t.Error("the second sighting must be the duplicate Seen recorded")
		}
	}
}

// TestDedupConcurrentSeenLeavesOneRecordPerKey pins the mutex: every writer
// must observe one shared view, so a key recorded once reads as a duplicate to
// the next reader whatever the interleaving.
func TestDedupConcurrentSeenLeavesOneRecordPerKey(t *testing.T) {
	d := NewDedup(1024, time.Minute)
	const workers, keys = 8, 64
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for k := range keys {
				d.Seen(keyFor(w, k))
			}
		}(w)
	}
	wg.Wait()
	for w := range workers {
		for k := range keys {
			if !d.Contains(keyFor(w, k)) {
				t.Fatalf("key (%d,%d) was lost under concurrent writes", w, k)
			}
		}
	}
}

func keyFor(worker, index int) string {
	return string(rune('a'+worker)) + string(rune('A'+index%26)) + string(rune('a'+index/26))
}
