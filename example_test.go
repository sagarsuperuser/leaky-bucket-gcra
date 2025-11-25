package leakybucketgcra

import (
	"fmt"
	"time"
)

// ExampleLimiter_Allow shows basic usage of the Limiter's Allow method.
func ExampleLimiter_Allow() {
	clock := newTestTime(time.Unix(0, 0))
	mock := newMockClient(clock)
	limiter := NewLimiter(mock)
	limit := PerSecond(2, 2) // 2 req/sec, burst 2

	res1, _ := limiter.Allow("user:42", limit)
	res2, _ := limiter.Allow("user:42", limit)
	res3, _ := limiter.Allow("user:42", limit)
	clock.advance(500 * time.Millisecond) // wait long enough for one token to replenish
	res4, _ := limiter.Allow("user:42", limit)

	fmt.Printf("first: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res1.Allowed, res1.Remaining, formatDur(res1.RetryAfter), formatDur(res1.ResetAfter))
	fmt.Printf("second: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res2.Allowed, res2.Remaining, formatDur(res2.RetryAfter), formatDur(res2.ResetAfter))
	fmt.Printf("third: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res3.Allowed, res3.Remaining, formatDur(res3.RetryAfter), formatDur(res3.ResetAfter))
	fmt.Printf("after wait: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res4.Allowed, res4.Remaining, formatDur(res4.RetryAfter), formatDur(res4.ResetAfter))

	// Output:
	// first: allowed=1 remaining=1 retry_after=none reset_after=0.5s
	// second: allowed=1 remaining=0 retry_after=none reset_after=1.0s
	// third: allowed=0 remaining=0 retry_after=0.5s reset_after=1.0s
	// after wait: allowed=1 remaining=0 retry_after=none reset_after=1.0s
}

// ExampleLimiter_AllowN shows usage of the Limiter's AllowN method.
func ExampleLimiter_AllowN() {
	clock := newTestTime(time.Unix(0, 0))
	mock := newMockClient(clock)
	limiter := NewLimiter(mock)
	limit := PerMinute(60, 300) // 60 req/min, burst 300

	res, _ := limiter.AllowN("account:99", limit, 3)

	clock.advance(3 * time.Second) // advance enough to replenish burst again

	res1, _ := limiter.AllowN("account:99", limit, 300)

	fmt.Printf("first: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res.Allowed, res.Remaining, formatDur(res.RetryAfter), formatDur(res.ResetAfter))
	fmt.Printf("second: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res1.Allowed, res1.Remaining, formatDur(res1.RetryAfter), formatDur(res1.ResetAfter))

	// Output:
	// first: allowed=3 remaining=297 retry_after=none reset_after=3.0s
	// second: allowed=300 remaining=0 retry_after=none reset_after=300.0s
}

// ExampleLimiter_Reset shows removing rate limit state for a key.
func ExampleLimiter_Reset() {
	clock := newTestTime(time.Unix(0, 0))
	mock := newMockClient(clock)
	limiter := NewLimiter(mock)
	limit := PerSecond(1, 1) // 1 req/sec, burst 1

	limited, _ := limiter.Allow("session:1", limit) // allowed on first call
	stateBefore, _ := limiter.Peek("session:1")
	limiter.Reset("session:1") // clear state
	stateAfter, _ := limiter.Peek("session:1")

	fmt.Printf("before reset allowed=%d remaining=%d\n", limited.Allowed, limited.Remaining)
	fmt.Printf("state before reset=%s\n", formatDur(stateBefore))
	fmt.Printf("state after reset=%s\n", formatDur(stateAfter))

	// Output:
	// before reset allowed=1 remaining=0
	// state before reset=1.0s
	// state after reset=none
}

func formatDur(d *time.Duration) string {
	if d == nil {
		return "none"
	}
	seconds := d.Seconds()
	return fmt.Sprintf("%.1fs", seconds)
}
