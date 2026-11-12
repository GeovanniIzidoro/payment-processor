package memcache_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/huncoding/payment-processor-course/pkg/memcache"
)

func TestSetGet(t *testing.T) {
	c := memcache.New[string](time.Minute, time.Minute)
	defer c.Close()

	c.Set("k", "v")
	got, ok := c.Get("k")
	if !ok {
		t.Fatal("Get(\"k\") reported the key as missing")
	}
	if got != "v" {
		t.Errorf("Get(\"k\") = %q, want %q", got, "v")
	}
}

func TestExpiration(t *testing.T) {
	c := memcache.New[int](10*time.Millisecond, time.Hour)
	defer c.Close()

	c.Set("k", 42)
	time.Sleep(30 * time.Millisecond)

	if _, ok := c.Get("k"); ok {
		t.Fatal("Get(\"k\") reported an expired key as present")
	}
}

func TestJanitorEvictsExpired(t *testing.T) {
	c := memcache.New[int](20*time.Millisecond, 10*time.Millisecond)
	defer c.Close()

	const entries = 1000
	for i := range entries {
		c.Set(fmt.Sprintf("key-%d", i), i)
	}

	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()

	for c.Len() != 0 {
		select {
		case <-poll.C:
		case <-timeout.C:
			t.Fatalf("Len() = %d, want 0 after janitor sweep", c.Len())
		}
	}

	if got := c.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0 after janitor sweep", got)
	}
}

func TestIncrementIsAtomic(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	add := func(current int, exists bool) int {
		if !exists {
			return 1
		}
		return current + 1
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(100)
	for range 100 {
		go func() {
			defer wg.Done()
			c.Increment("card:123", add)
		}()
	}
	wg.Wait()

	got, ok := c.Get("card:123")
	if !ok {
		t.Fatal("Get(\"card:123\") reported the key as missing")
	}
	if got != 100 {
		t.Errorf("Get(\"card:123\") = %d, want 100", got)
	}
}

func TestConcurrentAccessAcrossShards(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i%50)
			c.Set(key, i)
			_, ok := c.Get(key)
			if !ok {
				t.Errorf("Get(%q) reported the key as missing", key)
			}
		}(i)
	}
	wg.Wait()
}

func TestGetMissingKey(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	got, ok := c.Get("missing")
	if ok {
		t.Fatal("Get(\"missing\") reported a missing key as present")
	}
	if got != 0 {
		t.Errorf("Get(\"missing\") = %d, want zero value 0", got)
	}
}

func TestIncrementExpiredEntryStartsOver(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	c.SetTTL("counter", 99, -time.Second)
	got := c.Increment("counter", func(current int, exists bool) int {
		if exists {
			t.Errorf("Increment callback exists = true for an expired entry")
			return current + 1
		}
		return 1
	})
	if got != 1 {
		t.Errorf("Increment(...) = %d, want 1", got)
	}
	if value, ok := c.Get("counter"); !ok || value != 1 {
		t.Errorf("Get(\"counter\") = (%d, %t), want (1, true)", value, ok)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)

	for range 2 {
		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}
	}
}

func TestCloseError(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)

	if err := c.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}
