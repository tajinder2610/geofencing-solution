package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterBurstAndRefill(t *testing.T) {
	clock := time.Unix(0, 0)
	l := New(2, 3) // 2 rps, burst 3
	l.now = func() time.Time { return clock }

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d should be allowed within burst", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 {
		t.Fatalf("4th request should be denied with a wait, got ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("different keys must not share a bucket")
	}

	clock = clock.Add(500 * time.Millisecond) // refills one token
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("request should be allowed after refill")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("only one token should have refilled")
	}
}
