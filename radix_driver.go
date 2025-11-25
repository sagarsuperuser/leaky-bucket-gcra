package leakybucketgcra

import (
	"github.com/mediocregopher/radix/v3"
)

// Client matches the redis Client interface from the https://github.com/envoyproxy/ratelimit/blob/main/src/redis/driver.go
type Client interface {
	DoCmd(rcv interface{}, cmd, key string, args ...interface{}) error
	EvalScript(rcv interface{}, script string, keys []string, args ...interface{}) error
	PipeAppend(pipeline Pipeline, rcv interface{}, cmd, key string, args ...interface{}) Pipeline
	PipeDo(pipeline Pipeline) error
	Close() error
	NumActiveConns() int
	ImplicitPipeliningEnabled() bool
}

// Pipeline is a queue of radix actions for pipelined execution.
type Pipeline []radix.CmdAction

// RadixClient is a concrete implementation backed by a radix Client (pool/cluster/sentinel).
type RadixClient struct {
	client             radix.Client
	poolSize           int
	implicitPipelining bool
}

// NewRadixClient builds a radix-backed Client with the given pool size and options.
// It's implementation executes Lua script that uses redis.replicate_commands,
// so Redis 3.2+ is required. When implicitPipelining is true PipeDo executes
// commands sequentially; when false PipeDo issues a single pipeline round-trip.
func NewRadixClient(network, addr string, size int, implicitPipelining bool, opts ...radix.PoolOpt) (*RadixClient, error) {
	pool, err := radix.NewPool(network, addr, size, opts...)
	if err != nil {
		return nil, err
	}
	return &RadixClient{
		client:   pool,
		poolSize: size,
		// implicitPipelining can be set to true to let PipeDo execute each command sequentially,
		// executing pipelines implicitly with default radix behavior.
		implicitPipelining: implicitPipelining,
	}, nil
}

// DoCmd executes a single redis command.
func (c *RadixClient) DoCmd(rcv interface{}, cmd, key string, args ...interface{}) error {
	return c.client.Do(radix.FlatCmd(rcv, cmd, key, args...))
}

// EvalScript executes a Lua script with one or more keys.
func (c *RadixClient) EvalScript(rcv interface{}, script string, keys []string, args ...interface{}) error {
	// Use EvalScript for SHA/caching; it will handle SCRIPT LOAD/EVALSHA.
	es := radix.NewEvalScript(len(keys), script)
	return c.client.Do(es.FlatCmd(rcv, keys, args...))
}

// PipeAppend appends a command onto the pipeline queue.
func (c *RadixClient) PipeAppend(pipeline Pipeline, rcv interface{}, cmd, key string, args ...interface{}) Pipeline {
	return append(pipeline, radix.FlatCmd(rcv, cmd, key, args...))
}

// PipeDo writes multiple commands to redis, either implicitly (sequential Do)
// or via a single pipeline round-trip based on implicitPipelining flag
func (c *RadixClient) PipeDo(pipeline Pipeline) error {
	if c.implicitPipelining {
		for _, action := range pipeline {
			if err := c.client.Do(action); err != nil {
				return err
			}
		}
		return nil
	}
	return c.client.Do(radix.Pipeline(pipeline...))
}

// Close shuts down the underlying client.
func (c *RadixClient) Close() error {
	return c.client.Close()
}

// NumActiveConns returns the number of in-use connections (if backed by a Pool).
func (c *RadixClient) NumActiveConns() int {
	if p, ok := c.client.(*radix.Pool); ok {
		if c.poolSize <= 0 {
			return -1
		}
		avail := p.NumAvailConns()
		active := c.poolSize - avail
		if active < 0 {
			active = 0
		}
		return active
	}
	return -1
}

// ImplicitPipeliningEnabled reports whether implicit pipelining is enabled.
func (c *RadixClient) ImplicitPipeliningEnabled() bool {
	return c.implicitPipelining
}
