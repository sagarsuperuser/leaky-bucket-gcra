package leakybucketgcra

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mediocregopher/radix/v3"
)

// testTime is a fake time used for testing.
type testTime struct {
	mu     sync.Mutex
	cur    time.Time
	start  time.Time
	timers []testTimer
}

// testTimer is a fake timer.
type testTimer struct {
	when time.Time
	ch   chan<- time.Time
}

// now returns the current fake time.
func (tt *testTime) now() time.Time {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.cur
}

// Unix returns seconds elapsed since the fake clock start.
func (tt *testTime) unix() float64 {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.cur.Sub(tt.start).Seconds()
}

// newTimer creates a fake timer. It returns the channel,
// a function to stop the timer (which we don't care about),
// and a function to advance to the next timer.
func (tt *testTime) newTimer(dur time.Duration) (<-chan time.Time, func() bool, func()) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	ch := make(chan time.Time, 1)
	timer := testTimer{
		when: tt.cur.Add(dur),
		ch:   ch,
	}
	tt.timers = append(tt.timers, timer)
	return ch, func() bool { return true }, tt.advanceToTimer
}

// since returns the fake time since the given time.
func (tt *testTime) since(t time.Time) time.Duration {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.cur.Sub(t)
}

// advance advances the fake time.
func (tt *testTime) advance(dur time.Duration) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.advanceUnlocked(dur)
}

// advanceUnlocked advances the fake time, assuming it is already locked.
func (tt *testTime) advanceUnlocked(dur time.Duration) {
	tt.cur = tt.cur.Add(dur)
	i := 0
	for i < len(tt.timers) {
		if tt.timers[i].when.After(tt.cur) {
			i++
		} else {
			tt.timers[i].ch <- tt.cur
			copy(tt.timers[i:], tt.timers[i+1:])
			tt.timers = tt.timers[:len(tt.timers)-1]
		}
	}
}

// advanceToTimer advances the time to the next timer.
func (tt *testTime) advanceToTimer() {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if len(tt.timers) == 0 {
		panic("no timer")
	}
	when := tt.timers[0].when
	for _, timer := range tt.timers[1:] {
		if timer.when.Before(when) {
			when = timer.when
		}
	}
	tt.advanceUnlocked(when.Sub(tt.cur))
}

// newTestTime builds a fake clock starting at start.
func newTestTime(start time.Time) *testTime {
	return &testTime{cur: start, start: start}
}

// newMockClient implements the Client interface entirely in-memory for examples.
func newMockClient(clock *testTime) *mockClient {
	return &mockClient{
		store: make(map[string]float64),
		clock: clock,
	}
}

// mockClient simulates the Lua script logic for tests without Redis backend.
type mockClient struct {
	store map[string]float64
	clock *testTime
}

func (m *mockClient) DoCmd(rcv interface{}, cmd, key string, args ...interface{}) error {
	switch strings.ToUpper(cmd) {
	case "DEL":
		delete(m.store, key)
	case "PING":
		// no-op
	case "GET":
		if rcv != nil {
			if v, ok := m.store[key]; ok {
				assign(rcv, fmt.Sprintf("%g", v))
			} else {
				assign(rcv, "")
			}
		}
	}
	return nil
}

func (m *mockClient) EvalScript(rcv interface{}, script string, keys []string, args ...interface{}) error {
	result, err := m.eval(keys[0], args...)
	if err != nil {
		return err
	}
	out, ok := rcv.(*[]interface{})
	if !ok {
		return fmt.Errorf("unexpected receiver type %T", rcv)
	}
	*out = result
	return nil
}

func (m *mockClient) PipeAppend(pipeline Pipeline, rcv interface{}, cmd, key string, args ...interface{}) Pipeline {
	return append(pipeline, radix.FlatCmd(rcv, cmd, key, args...))
}

func (m *mockClient) PipeDo(pipeline Pipeline) error { return nil }
func (m *mockClient) Close() error                   { return nil }
func (m *mockClient) NumActiveConns() int            { return 0 }
func (m *mockClient) ImplicitPipeliningEnabled() bool {
	return false
}

func (m *mockClient) eval(key string, args ...interface{}) ([]interface{}, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("not enough args")
	}

	parse := func(v interface{}) (float64, error) {
		return strconv.ParseFloat(fmt.Sprint(v), 64)
	}

	burst, err := parse(args[0])
	if err != nil {
		return nil, err
	}
	rate, err := parse(args[1])
	if err != nil {
		return nil, err
	}
	period, err := parse(args[2])
	if err != nil {
		return nil, err
	}
	cost, err := parse(args[3])
	if err != nil {
		return nil, err
	}

	emissionInterval := period / rate
	increment := emissionInterval * cost
	burstOffset := emissionInterval * burst
	now := m.clock.unix()

	tat := now
	if prev, ok := m.store[key]; ok {
		tat = prev
	}

	if cost > burst {
		return []interface{}{int64(0), int64(0), "-1", fmt.Sprintf("%f", tat-now)}, nil
	}

	newTAT := math.Max(tat, now) + increment
	allowAt := newTAT - burstOffset
	diff := now - allowAt

	remaining := math.Floor(diff/emissionInterval + 0.5)

	var allowed int64
	var retryAfter float64
	var resetAfter float64

	if remaining < 0 {
		allowed = 0
		remaining = 0
		resetAfter = tat - now
		retryAfter = diff * -1
		if retryAfter > burst {
			retryAfter = -1
		}
	} else {
		allowed = int64(cost)
		resetAfter = newTAT - now
		m.store[key] = newTAT
		retryAfter = -1
	}

	return []interface{}{
		allowed,
		int64(remaining),
		fmt.Sprintf("%g", retryAfter),
		fmt.Sprintf("%g", resetAfter),
	}, nil
}

// assign copies v into rcv if types are compatible.
// It supports a few common types used in this code.
func assign(rcv interface{}, v interface{}) {
	switch t := rcv.(type) {
	case *int64:
		switch val := v.(type) {
		case int64:
			*t = val
		case int:
			*t = int64(val)
		}
	case *int:
		switch val := v.(type) {
		case int64:
			*t = int(val)
		case int:
			*t = val
		}
	case *string:
		switch val := v.(type) {
		case string:
			*t = val
		case fmt.Stringer:
			*t = val.String()
		case []string:
			if len(val) > 0 {
				*t = val[0]
			}
		}
	case *[]byte:
		switch val := v.(type) {
		case string:
			*t = []byte(val)
		}
	case *[]string:
		switch val := v.(type) {
		case []string:
			*t = val
		}
	case *interface{}:
		*t = v
	}
}
