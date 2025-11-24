// Package leakybucketgcra implements a Redis-backed rate limiter using the
// Generic Cell Rate Algorithm (GCRA). It stores limiter state in Redis via a
// preloaded Lua script, enabling low-latency distributed rate limits.
// Find optional demos under cmd/.
package leakybucketgcra
