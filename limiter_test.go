package leakybucketgcra

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLimiter(t *testing.T) *Limiter {
	t.Helper()

	client, err := NewRadixClient("tcp", "127.0.0.1:6379", 4, false)
	if err != nil {
		t.Fatalf("redis not available: %v", err)
	}
	if err := client.DoCmd(nil, "PING", ""); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return NewLimiter(client)
}

func newBenchLimiter(b *testing.B) *Limiter {
	b.Helper()

	client, err := NewRadixClient("tcp", "127.0.0.1:6379", 4, false)
	if err != nil {
		b.Fatalf("redis not available: %v", err)
	}
	if err := client.DoCmd(nil, "PING", ""); err != nil {
		b.Fatalf("redis ping failed: %v", err)
	}
	b.Cleanup(func() { client.Close() })
	return NewLimiter(client)
}

func resetKey(t *testing.T, l *Limiter, key string) {
	t.Helper()
	if err := l.Reset(key); err != nil {
		t.Fatalf("reset key: %v", err)
	}
}

func call(t *testing.T, l *Limiter, key string, limit Limit, cost int64) *RateLimitResult {
	t.Helper()
	res, err := l.AllowN(key, limit, cost)
	if err != nil {
		t.Fatalf("AllowN(%s, %#v, 1), %#v: ", key, limit, err)
	}
	return res
}

// Test zero burst, should not allow any requests.
func TestZeroBurstAndRate(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := Limit{Burst: 0, Rate: 1, Period: time.Second} // 1 req/sec, burst 0
	key := "test:zero"
	resetKey(t, limiter, key)

	for i := 0; i < 5; i++ {
		time.Sleep(time.Second)
		res := call(t, limiter, key, limit, 1)
		if res.Allowed > 0 {
			t.Errorf("expected limited")
		}
	}

}

// Test cost bigger than burst
func TestCostBiggerThanBurst(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := Limit{Burst: 5, Rate: 1, Period: time.Second} // 1 req/sec, burst 5
	key := "test:costbigger"
	resetKey(t, limiter, key)

	res := call(t, limiter, key, limit, 10)

	if res.Allowed != 0 {
		t.Errorf("expected limited")
	}
	if res.Remaining != 0 {
		t.Errorf("remaining=%d want 0", res.Remaining)
	}
	require.Nil(t, res.RetryAfter)
	assert.Equal(t, time.Duration(0), *res.ResetAfter)

}

// This test was taken from https://cs.opensource.google/go/x/time/+/master:rate/rate_test.go
// we can infer that our GCRA limiter behaves similarly to the token bucket implementation in
// https://pkg.go.dev/golang.org/x/time/rate under long-running high-QPS conditions.
func TestLongRunningQPS(t *testing.T) {
	// The test runs for a few (fake) seconds executing many requests
	// and then checks that overall number of requests is reasonable
	const (
		rate  = 100
		burst = 100
	)
	var (
		numOK = int32(0)
	)

	limiter := newTestLimiter(t)
	limit := PerSecond(rate, burst) // 100 req/sec, burst 100
	key := "test:longqps"
	resetKey(t, limiter, key)

	start := time.Now()
	end := start.Add(5 * time.Second)
	for time.Now().Before(end) {
		res := call(t, limiter, key, limit, 1)
		if res.Allowed > 0 {
			numOK++
		}
		// This will still offer ~500 requests per second, but won't consume
		// outrageous amount of CPU.
		time.Sleep(2 * time.Millisecond)
	}
	elapsed := time.Since(start)
	ideal := burst + (rate * elapsed.Seconds())

	fmt.Printf("elapsed=%v ideal=%f numOK=%d\n", elapsed, ideal, numOK)

	// We should never get more requests than allowed.
	if want := int32(ideal + 1); numOK > want {
		t.Errorf("numOK = %d, want <= %d (ideal %f)", numOK, want, ideal)
	}

	// We should get at least 99% of the ideal number of requests.
	if want := int32(0.99 * ideal); numOK < want {
		t.Errorf("numOK = %d, want >= %d (ideal %f)", numOK, want, ideal)
	}
}

// Taken from https://cs.opensource.google/go/x/time/+/master:rate/rate_test.go
func TestSimultaneousRequests(t *testing.T) {
	const (
		rate        = 1
		burst       = 5
		numRequests = 15
	)
	var (
		wg    sync.WaitGroup
		numOK = uint32(0)
	)

	limiter := newTestLimiter(t)
	// Very slow replenishing bucket.
	limit := PerSecond(rate, burst) // 1 req/sec, burst 5
	key := "test:simulreqs"
	resetKey(t, limiter, key)

	// Tries to take a token, atomically updates the counter and decreases the wait
	// group counter.
	f := func() {
		defer wg.Done()
		res := call(t, limiter, key, limit, 1)
		if res.Allowed > 0 {
			atomic.AddUint32(&numOK, 1)
		}
	}

	wg.Add(numRequests)
	for i := 0; i < numRequests; i++ {
		go f()
	}
	wg.Wait()
	if numOK != burst {
		t.Errorf("numOK = %d, want %d", numOK, burst)
	}
}

func TestLimitCalculatesRateLimitResult(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := PerMinute(60, 300) // 60 req/min, burst 300
	key := "test:calc"
	resetKey(t, limiter, key)

	for i := 0; i < 100; i++ {
		call(t, limiter, key, limit, 1)
	}
	result := call(t, limiter, key, limit, 1)

	if result.Allowed == 0 {
		t.Errorf("expected allowed")
	}
	if result.Remaining != 199 {
		t.Errorf("remaining=%d want 199", result.Remaining)
	}
	require.Nil(t, result.RetryAfter)
	require.NotNil(t, result.ResetAfter)
	require.InDelta(t, 101*time.Second, *result.ResetAfter, float64(100*time.Millisecond))
}

func TestLimitsDifferentKeysIndependently(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := PerMinute(60, 300) // 60 req/min, burst 300

	resetKey(t, limiter, "test:indep1")
	for i := 0; i < 100; i++ {
		call(t, limiter, "test:indep1", limit, 10)
	}

	resetKey(t, limiter, "test:indep2")
	result := call(t, limiter, "test:indep2", limit, 2)

	if result.Allowed == 0 {
		t.Errorf("expected allowed")
	}
	if result.Remaining != 298 {
		t.Errorf("remaining=%d want 298", result.Remaining)
	}
	require.Nil(t, result.RetryAfter)
	require.NotNil(t, result.ResetAfter)
	require.InDelta(t, 2*time.Second, *result.ResetAfter, float64(100*time.Millisecond))
}

func TestNonUnitCost(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := PerMinute(100, 1000) // 100 req/min, burst 1000 (600 ms per token refill)
	key := "test:remaining"
	resetKey(t, limiter, key)

	result := call(t, limiter, key, limit, 2)

	if result.Allowed == 0 {
		t.Errorf("expected allowed")
	}
	if result.Remaining == 0 {
		t.Errorf("remaining=%d want 998", result.Remaining)
	}
	require.Nil(t, result.RetryAfter)
	require.NotNil(t, result.ResetAfter)
	require.InDelta(t, 1200*time.Millisecond, *result.ResetAfter, float64(10*time.Millisecond))
}

func TestLimitsAfterDepleted(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := PerSecond(10, 10) // 10 req/sec, burst 10
	key := "test:depleted"
	resetKey(t, limiter, key)

	// drain burst
	for i := 0; i < 10; i++ {
		call(t, limiter, key, limit, 1)
	}

	for i := 0; i < 10; i++ {
		result := call(t, limiter, key, limit, 1)
		if result.Allowed != 0 {
			t.Errorf("expected limited after depletion")
		}
		if result.Remaining != 0 {
			t.Errorf("remaining=%d want 0", result.Remaining)
		}
		require.NotNil(t, result.RetryAfter)
		require.NotNil(t, result.ResetAfter)
		require.InDelta(t, 100*time.Millisecond, *result.RetryAfter, float64(20*time.Millisecond))
		require.InDelta(t, 1*time.Second, *result.ResetAfter, float64(20*time.Millisecond))
	}
}

func TestRecoversAfterTime(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := PerSecond(10, 10) // 10 req/sec, burst 10
	key := "test:recover"
	resetKey(t, limiter, key)

	for i := 0; i < 10; i++ {
		call(t, limiter, key, limit, 1)
	}
	limited := call(t, limiter, key, limit, 1)
	if limited.Allowed != 0 {
		t.Errorf("expected limited")
	}
	require.NotNil(t, limited.RetryAfter)
	require.NotNil(t, limited.ResetAfter)
	time.Sleep(*limited.RetryAfter)
	passed := call(t, limiter, key, limit, 1)
	if passed.Allowed == 0 {
		t.Fatalf("expected to pass after wait")
	}
}

func TestPeekReturnsState(t *testing.T) {
	limiter := newTestLimiter(t)
	key := "test:peek"
	resetKey(t, limiter, key)

	limit := PerSecond(2, 2) // 2 req/sec, burst 2
	_, err := limiter.Allow(key, limit)
	require.NoError(t, err)

	state, err := limiter.Peek(key)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Greater(t, *state, time.Duration(0))

	require.NoError(t, limiter.Reset(key))
	state, err = limiter.Peek(key)
	require.NoError(t, err)
	require.Nil(t, state)
}

func TestCostBiggerThanRemaining(t *testing.T) {
	limiter := newTestLimiter(t)
	limit := PerSecond(10, 10) // 10 req/sec, burst 10
	key := "test:remaining"
	resetKey(t, limiter, key)

	call(t, limiter, key, limit, 9)
	result := call(t, limiter, key, limit, 2)

	if result.Allowed != 0 {
		t.Errorf("expected limited")
	}
	if result.Remaining != 0 {
		t.Errorf("remaining=%d want 0", result.Remaining)
	}
	require.NotNil(t, result.RetryAfter)
	require.NotNil(t, result.ResetAfter)
	require.InDelta(t, 100*time.Millisecond, *result.RetryAfter, float64(10*time.Millisecond))
}

func TestRemaining(t *testing.T) {
	type testCase struct {
		burst             int64
		rate              int64
		period            float64
		cost              int64
		repeat            int
		expectedRemaining int64
	}
	tests := []testCase{
		{burst: 4500, rate: 75, period: 60, cost: 1, repeat: 1, expectedRemaining: 4499},
		{burst: 4500, rate: 75, period: 60, cost: 1, repeat: 2, expectedRemaining: 4498},
		{burst: 4500, rate: 75, period: 60, cost: 2, repeat: 1, expectedRemaining: 4498},
		{burst: 1000, rate: 100, period: 60, cost: 200, repeat: 1, expectedRemaining: 800},
		{burst: 1000, rate: 100, period: 60, cost: 200, repeat: 4, expectedRemaining: 200},
		{burst: 1000, rate: 100, period: 60, cost: 200, repeat: 5, expectedRemaining: 0},
		{burst: 1000, rate: 100, period: 60, cost: 1, repeat: 137, expectedRemaining: 863},
		{burst: 1000, rate: 100, period: 60, cost: 1001, repeat: 1, expectedRemaining: 0},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("case-%d", i+1), func(t *testing.T) {
			limiter := newTestLimiter(t)
			key := fmt.Sprintf("test:case:%d", i+1)
			resetKey(t, limiter, key)
			limit := Limit{Burst: tc.burst, Rate: tc.rate, Period: time.Duration(tc.period * float64(time.Second))}

			var res *RateLimitResult
			for j := 0; j < tc.repeat; j++ {
				res = call(t, limiter, key, limit, tc.cost)
			}
			if res.Remaining != tc.expectedRemaining {
				t.Errorf("remaining=%d want %d", res.Remaining, tc.expectedRemaining)
			}
		})
	}
}

func BenchmarkAllowN(b *testing.B) {
	limiter := newBenchLimiter(b)
	limit := PerSecond(1e6, 1e6) // 1 million req/sec, burst 1 million
	key := "bench:allown"

	if err := limiter.Reset(key); err != nil {
		b.Fatalf("reset key: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			res, err := limiter.AllowN(key, limit, 1)
			if err != nil {
				b.Fatalf("AllowN: %v", err)
			}
			if res.Allowed == 0 {
				b.Fatalf("expected allowed")
			}
		}
	})
}
