package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "redis-streams",
		Engine:     "redis",
		Capability: "event-bus",
		Doc:        "Redis Streams: an append-only log with consumer groups",
		IllegalFor: map[string]string{
			"no-broker": "there is a Redis to run",
			"durable":   "durability is an operator setting: under the default append-only policy a second of writes can be lost on a crash",
		},
		Satisfies: map[string]string{
			"cross-process":   "clients in other processes read the stream over the network",
			"replay":          "a stream is read from any id, so a late reader can start at the beginning",
			"ordered":         "one stream is an ordered sequence of ids",
			"at-least-once":   "an unacknowledged entry stays in the pending list and can be claimed again",
			"fan-out":         "several groups on one stream each see every entry",
			"consumer-groups": "a group shares entries among its consumers",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 4},
			{Dim: "maintenance", Value: 3},
			{Dim: "dependency", Value: 2},
			{Dim: "runtime", Value: 2},
			{Dim: "operations", Value: 3, Source: "derived", Note: "one server, but stream trimming and group lag to watch"},
		},
		Impl: &compile.BusTransport{
			Suffix: "RedisBus",
			Topics: []string{"redis"},
			Import: [2]string{"redis", "github.com/redis/go-redis/v9"},
			Emit:   emitRedisBus,
			Hint:   redisHint,
		},
	})
	compile.RegisterAlias("redis-streams", "redis")
}

func emitRedisBus(in compile.BusInput) ([]*sexp.Node, error) {
	return busDecl(in, "Redis Streams", []*sexp.Node{
		sexp.L(sexp.Sym("field"), sexp.Sym("rdb"), sexp.Sym("*redis.Client")),
		sexp.L(sexp.Sym("field"), sexp.Sym("stream"), sexp.Sym("string")),
	}, sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("rdb"), sexp.Sym("*redis.Client")), sexp.L(sexp.Sym("stream"), sexp.Sym("string"))),
		fmt.Sprintf("return &%s{rdb: rdb, stream: stream}", in.Impl),
		"Events are appended to one stream, so a handler that starts later can read back what it missed.")
}

func redisHint(method, event string) string {
	switch method {
	case "Publish":
		return fmt.Sprintf("XADD e to s.stream as JSON under a field named %q. Return the error from the command.", event)
	case "On":
		return fmt.Sprintf("Read s.stream with XREADGROUP, decode entries carrying %q as JSON into %s, call h, and XACK "+
			"only when h returns nil so a failed entry stays in the pending list. Return a func that stops the reader.", event, event)
	}
	return ""
}
