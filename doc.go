// Package leakybucketgcra implements a Redis-backed rate limiter using the
// Generic Cell Rate Algorithm (GCRA).
// State for each key is stored in Redis via
// a single Lua script, enabling low-latency distributed limits across processes.
// The script uses redis.replicate_commands (Redis 3.2+)
// Find optional demos under cmd/.
package leakybucketgcra
