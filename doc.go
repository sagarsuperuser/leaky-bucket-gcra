// Package leakybucketgcra implements a Redis-backed rate limiter using the
// Generic Cell Rate Algorithm (GCRA). State for each key is stored in Redis via
// a single Lua script, enabling low-latency distributed limits across
// processes. The script uses redis.replicate_commands (Redis 3.2+) and the
// Redis TIME command for its clock source, so callers do not need to pass
// timestamps.
//
// Per-key state is set with an expiry that matches the fully replenished
// deadline; calling Reset deletes the key explicitly. If a cost exceeds the
// configured burst it is denied with no retry hint (RetryAfter will be nil).
//
// Find optional demos under cmd/.
//
// Example:
//
//	client, _ := leakybucketgcra.NewRadixClient("tcp", "127.0.0.1:6379", 4, false)
//	defer client.Close()
//
//	limiter := leakybucketgcra.NewLimiter(client)
//	limit := leakybucketgcra.PerSecond(10, 20)
//	res, _ := limiter.Allow("user:42", limit)
//	_ = res
package leakybucketgcra
