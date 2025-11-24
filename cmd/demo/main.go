package main

import (
	"fmt"
	"log"
	"time"

	gcra "github.com/sagarsuperuser/leaky-bucket-gcra"
)

func main() {
	// Simple demo runner for the GCRA script via radix.
	client, err := gcra.NewRadixClient("tcp", "127.0.0.1:6379", 4, false)
	if err != nil {
		log.Fatalf("redis client: %v", err)
	}
	defer client.Close()

	limiter := gcra.NewLimiter(client)
	limit := gcra.PerSecond(3, 3) // 3 req/sec, burst 3
	key := "demo:gcra"

	// Clean slate.
	if err := limiter.Reset(key); err != nil {
		log.Fatalf("reset: %v", err)
	}

	fmt.Println("---- issuing 5 sequential requests (cost=1) ----")
	for i := 1; i <= 5; i++ {
		res, err := limiter.Allow(key, limit)
		if err != nil {
			log.Fatalf("allow %d: %v", i, err)
		}
		fmt.Printf("#%d allowed=%d remaining=%d retry_after=%v reset_after=%v\n",
			i, res.Allowed, res.Remaining, res.RetryAfter, res.ResetAfter)
	}

	fmt.Println("\n---- large cost request (cost=5) ----")
	res, err := limiter.AllowN(key, limit, 5)
	if err != nil {
		log.Fatalf("allowN: %v", err)
	}
	fmt.Printf("cost=5 allowed=%d remaining=%d retry_after=%v reset_after=%v\n",
		res.Allowed, res.Remaining, res.RetryAfter, res.ResetAfter)

	fmt.Println("\nWait 1s and try again (cost=1)...")
	time.Sleep(1000 * time.Millisecond)
	res, err = limiter.Allow(key, limit)
	if err != nil {
		log.Fatalf("allow retry: %v", err)
	}
	fmt.Printf("retry allowed=%d remaining=%d retry_after=%v reset_after=%v\n",
		res.Allowed, res.Remaining, res.RetryAfter, res.ResetAfter)
}
