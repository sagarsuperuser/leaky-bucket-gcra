package leakybucketgcra_test

import (
	"fmt"
	"time"

	gcra "github.com/sagarsuperuser/leaky-bucket-gcra"
)

// example is just to show the behaviour, integrated with fake clock and client
// ; check cmd/ for real Redis usage.
func ExampleLimiter_Allow() {
	clock := newTestTime(time.Unix(0, 0))
	mock := newMockClient(clock)
	limiter := gcra.NewLimiter(mock)
	limit := gcra.PerSecond(2, 2) // 2 req/sec, burst 2

	res1, _ := limiter.Allow("user:42", limit)
	res2, _ := limiter.Allow("user:42", limit)
	res3, _ := limiter.Allow("user:42", limit)
	clock.advance(500 * time.Millisecond) // wait long enough for one token to replenish
	res4, _ := limiter.Allow("user:42", limit)

	fmt.Printf("first: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res1.Allowed, res1.Remaining, FormatDur(res1.RetryAfter), FormatDur(res1.ResetAfter))
	fmt.Printf("second: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res2.Allowed, res2.Remaining, FormatDur(res2.RetryAfter), FormatDur(res2.ResetAfter))
	fmt.Printf("third: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res3.Allowed, res3.Remaining, FormatDur(res3.RetryAfter), FormatDur(res3.ResetAfter))
	fmt.Printf("after wait: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res4.Allowed, res4.Remaining, FormatDur(res4.RetryAfter), FormatDur(res4.ResetAfter))

	// Output:
	// first: allowed=1 remaining=1 retry_after=none reset_after=0.5s
	// second: allowed=1 remaining=0 retry_after=none reset_after=1.0s
	// third: allowed=0 remaining=0 retry_after=0.5s reset_after=1.0s
	// after wait: allowed=1 remaining=0 retry_after=none reset_after=1.0s
}

// example is just to show the behaviour, integrated with fake clock and client
// ; check cmd/ for real Redis usage.
func ExampleLimiter_AllowN() {
	clock := newTestTime(time.Unix(0, 0))
	mock := newMockClient(clock)
	limiter := gcra.NewLimiter(mock)
	limit := gcra.PerMinute(60, 300) // 60 req/min, burst 300

	res, _ := limiter.AllowN("account:99", limit, 3)

	clock.advance(3 * time.Second) // advance enough to replenish burst again

	res1, _ := limiter.AllowN("account:99", limit, 300)

	fmt.Printf("first: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res.Allowed, res.Remaining, FormatDur(res.RetryAfter), FormatDur(res.ResetAfter))
	fmt.Printf("second: allowed=%d remaining=%d retry_after=%s reset_after=%s\n",
		res1.Allowed, res1.Remaining, FormatDur(res1.RetryAfter), FormatDur(res1.ResetAfter))

	// Output:
	// first: allowed=3 remaining=297 retry_after=none reset_after=3.0s
	// second: allowed=300 remaining=0 retry_after=none reset_after=300.0s
}

func ExampleLimiter_Reset() {
	// example is just to show the behaviour, integrated with fake clock and client
	// ; check cmd/ for real Redis usage.
	clock := newTestTime(time.Unix(0, 0))
	mock := newMockClient(clock)
	limiter := gcra.NewLimiter(mock)
	limit := gcra.PerSecond(1, 1) // 1 req/sec, burst 1

	limited, _ := limiter.Allow("session:1", limit) // allowed on first call
	stateBefore, _ := limiter.Peek("session:1")
	limiter.Reset("session:1") // clear state
	stateAfter, _ := limiter.Peek("session:1")

	fmt.Printf("before reset allowed=%d remaining=%d\n", limited.Allowed, limited.Remaining)
	fmt.Printf("state before reset=%s\n", FormatDur(stateBefore))
	fmt.Printf("state after reset=%s\n", FormatDur(stateAfter))

	// Output:
	// before reset allowed=1 remaining=0
	// state before reset=1.0s
	// state after reset=none
}

func FormatDur(d *time.Duration) string {
	if d == nil {
		return "none"
	}
	seconds := d.Seconds()
	return fmt.Sprintf("%.1fs", seconds)
}
