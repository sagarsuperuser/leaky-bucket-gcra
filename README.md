# Leaky Bucket GCRA

[![Go Reference](https://pkg.go.dev/badge/github.com/sagarsuperuser/leaky-bucket-gcra.svg)](https://pkg.go.dev/github.com/sagarsuperuser/leaky-bucket-gcra)

Redis-backed rate limiting using the Generic Cell Rate Algorithm(a leaky-bucket style limiter).

The limiter uses a single Lua script executed via [radix](https://github.com/mediocregopher/radix) for fast, consistent limits across distributed processes.

## Comparison

This GCRA-based limiter behaves similarly to the token bucket implementation in [`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate). Some of the unit tests in this repository closely mirror those from the Go rate package.
## Requirements

- Go 1.22+
- Redis reachable at `127.0.0.1:6379` (see test/demo instructions below)
- The code requires Redis version 3.2 or newer since it relies on replicate_commands feature.

## Go package docs

Browse the API docs on pkg.go.dev: [documentation](https://pkg.go.dev/github.com/sagarsuperuser/leaky-bucket-gcra)

## Install

```bash
go get "github.com/sagarsuperuser/leaky-bucket-gcra"
```

## Quick start

```go
package main

import (
	"log"

	gcra "github.com/sagarsuperuser/leaky-bucket-gcra"
)

func main() {
	client, err := gcra.NewRadixClient("tcp", "127.0.0.1:6379", 4, false)
	if err != nil {
		log.Fatalf("redis client: %v", err)
	}
	defer client.Close()

	limiter := gcra.NewLimiter(client)
	limit := gcra.PerSecond(10, 20) // 10 req/sec with burst of 20

	res, err := limiter.AllowN("user:42", limit, 3)
	if err != nil {
		log.Fatalf("allow: %v", err)
	}
	if res.Allowed == 0 {
		log.Printf("limited, retry in %v", res.RetryAfter)
		return
	}
	log.Printf("allowed=%d remaining=%d reset_after=%v", res.Allowed, res.Remaining, res.ResetAfter)
}
```

## Demo

Run the sample program (requires Redis on `localhost:6379`):

```bash
go run ./cmd/demo
```

## Testing

Run examples (no Redis needed):

```bash
go test ./...
```

Run integration tests with Redis (e.g., `docker run -p 6379:6379 redis:7-alpine`):

```bash
go test -tags=integration ./...
```
## Inspiration

This code was inspired by Brandur Leach and his work on throttled [throttled](https://github.com/throttled/throttled) and the [blog post](https://brandur.org/rate-limiting).

## References

- https://pkg.go.dev/golang.org/x/time/rate
- https://github.com/rwz/redis-gcra
