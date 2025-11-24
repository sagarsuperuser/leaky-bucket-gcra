package leakybucketgcra

import (
	"fmt"
	"strconv"
	"time"
)

const redisPrefix = ""

// Limit describes the rate configuration.
type Limit struct {
	Rate   int64
	Burst  int64
	Period time.Duration
}

func (l Limit) String() string {
	return fmt.Sprintf("%d req/%s (burst %d)", l.Rate, fmtDur(l.Period), l.Burst)
}

func (l Limit) IsZero() bool {
	return l == Limit{}
}

func fmtDur(d time.Duration) string {
	switch d {
	case time.Second:
		return "s"
	case time.Minute:
		return "m"
	case time.Hour:
		return "h"
	}
	return d.String()
}

// PerSecond returns a Limit for requests per second with the given rate and burst.
func PerSecond(rate, burst int64) Limit {
	return Limit{Rate: rate, Period: time.Second, Burst: burst}
}

// PerMinute returns a Limit for requests per minute with the given rate and burst.
func PerMinute(rate, burst int64) Limit {
	return Limit{Rate: rate, Period: time.Minute, Burst: burst}
}

// PerHour returns a Limit for requests per hour with the given rate and burst.
func PerHour(rate, burst int64) Limit {
	return Limit{Rate: rate, Period: time.Hour, Burst: burst}
}

// PerDay returns a Limit for requests per day with the given rate and burst.
func PerDay(rate, burst int64) Limit {
	return Limit{Rate: rate, Period: 24 * time.Hour, Burst: burst}
}

// RateLimitResult captures the limiter decision and relevant metadata for a key.
// All durations are relative to the time the check was performed.
type RateLimitResult struct {
	// Rate configuration provided while querying the limiter.
	Limit Limit

	// Allowed is the number of requests permitted for this invocation (0 when limited).
	Allowed int64

	// Remaining is the number of requests remaining in the current rate limit window.
	Remaining int64

	// RetryAfter is how long to wait before a subsequent request may be allowed.
	// It is nil when the current request is allowed or a retry hint is not applicable.
	RetryAfter *time.Duration

	// ResetAfter is the time until the limiter returns to a fully replenished state.
	ResetAfter *time.Duration
}

// Limiter controls how frequently events are allowed to happen.
type Limiter struct {
	rdb Client
}

// NewLimiter returns a new Limiter.
func NewLimiter(rdb Client) *Limiter {
	return &Limiter{rdb: rdb}
}

// Allow is a shortcut for AllowN with cost 1.
func (l Limiter) Allow(key string, limit Limit) (*RateLimitResult, error) {
	return l.AllowN(key, limit, 1)
}

// AllowN reports whether n events may happen at time now (cost = n).
func (l Limiter) AllowN(key string, limit Limit, n int64) (*RateLimitResult, error) {
	if n <= 0 {
		return nil, fmt.Errorf("invalid cost: %d; must be > 0", n)
	}
	if limit.Burst < 0 {
		return nil, fmt.Errorf("invalid Limit: %#v,  burst must be greater than zero", limit)
	}

	if limit.Period <= 0 {
		return nil, fmt.Errorf("invalid Limit: %#v,  period must be greater than zero", limit)
	}

	if limit.Rate <= 0 {
		return nil, fmt.Errorf("invalid Limit: %#v,  rate must be greater than zero", limit)
	}

	res, err := l.runAllow(key, limit, n)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Reset removes any tracking for this key.
func (l Limiter) Reset(key string) error {
	return l.rdb.DoCmd(nil, "DEL", redisPrefix+key)
}

// Internal helpers -----------------------------------------------------------
func (l Limiter) runAllow(key string, limit Limit, cost int64) (*RateLimitResult, error) {
	var resp []interface{}

	err := l.rdb.EvalScript(
		&resp,
		allowNScriptSrc,
		[]string{redisPrefix + key},
		strconv.FormatInt(limit.Burst, 10),
		strconv.FormatInt(limit.Rate, 10),
		strconv.FormatFloat(limit.Period.Seconds(), 'f', -1, 64),
		strconv.FormatInt(cost, 10),
	)
	if err != nil {
		return nil, err
	}
	if len(resp) != 4 {
		return nil, fmt.Errorf("unexpected redis response, got %d items", len(resp))
	}

	allowed, err := strconv.ParseInt(fmt.Sprint(resp[0]), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse limited: %w", err)
	}
	remaining, err := strconv.ParseInt(fmt.Sprint(resp[1]), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse remaining: %w", err)
	}
	retryAfter, err := parseDurationSeconds(resp[2])
	if err != nil {
		return nil, fmt.Errorf("parse retry_after: %w", err)
	}
	resetAfter, err := parseDurationSeconds(resp[3])
	if err != nil {
		return nil, fmt.Errorf("parse reset_after: %w", err)
	}

	return &RateLimitResult{
		Limit:      limit,
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAfter: resetAfter,
	}, nil
}

func parseDurationSeconds(raw interface{}) (*time.Duration, error) {
	s, err := normalizeString(raw)
	if err != nil {
		return nil, err
	}
	if s == "-1" || s == "" {
		return nil, nil
	}
	seconds, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	d := time.Duration(seconds * float64(time.Second))
	return &d, nil
}

func normalizeString(v interface{}) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	case []byte:
		return string(t), nil
	default:
		return "", fmt.Errorf("unexpected type %T for normalizeString", v)
	}
}
