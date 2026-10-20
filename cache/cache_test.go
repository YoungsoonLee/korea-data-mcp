package cache

import (
	"testing"
	"time"
)

func TestExpiryAndEviction(t *testing.T) {
	now := time.Unix(0, 0)
	c := New[int](2)
	c.now = func() time.Time { return now }

	c.Set("a", 1, time.Second)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatal("expected hit")
	}
	now = now.Add(2 * time.Second)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected expiry")
	}

	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Set("c", 3, time.Minute)
	if len(c.items) != 2 {
		t.Fatalf("expected max 2 entries, got %d", len(c.items))
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatal("newest entry must survive eviction")
	}
}
