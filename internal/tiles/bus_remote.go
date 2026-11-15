package tiles

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// Three more buses, so that event-bus is a real choice rather than a
// formality. Each declares what it cannot do, and those clauses are the
// whole point: a spec that says (replay) rules out the queue, a spec that
// says (no-broker) rules out all three, and a reader can check every
// sentence against the system it describes.
//
// The implementations are deliberately thin. A transport's generated code
// is a struct, a constructor and two holes; what the tile is really for is
// the legality and the price.

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "kafka",
		Capability: "event-bus",
		Doc:        "Kafka: a partitioned log, ordered per key, read back at will",
		IllegalFor: map[string]string{
			"no-broker": "a cluster to run, partitions to size, and retention and consumer lag to watch",
		},
		Satisfies: map[string]string{
			"durable":         "a produce is acknowledged once the partition's replicas have the record",
			"cross-process":   "consumers in other processes read the topic over the network",
			"replay":          "a consumer group can seek to any offset still inside retention",
			"ordered":         "records with the same key go to one partition, and a partition is an ordered log",
			"at-least-once":   "an uncommitted offset is redelivered to whoever takes the partition next",
			"fan-out":         "several consumer groups on one topic each see every record",
			"consumer-groups": "a group shares partitions among its members and rebalances them",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 5},
			{Dim: "maintenance", Value: 5},
			{Dim: "dependency", Value: 4, Source: "derived", Note: "a client library with a large transitive graph"},
			{Dim: "runtime", Value: 4},
			{Dim: "operations", Value: 9, Source: "derived", Note: "the heaviest thing in the registry to run: brokers, partitions, rebalances, lag, and someone on call for them"},
		},
		Impl: &compile.BusTransport{
			Suffix: "KafkaBus",
			Topics: []string{"kafka"},
			Import: [2]string{"kgo", "github.com/twmb/franz-go/pkg/kgo"},
			Emit:   emitKafkaBus,
			Hint:   kafkaHint,
		},
	})
	compile.RegisterAlias("kafka", "kafka")

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

	compile.RegisterOffer(&compile.Offer{
		Tile:       "rabbitmq",
		Capability: "event-bus",
		Doc:        "RabbitMQ: exchanges, bindings and competing consumers",
		IllegalFor: map[string]string{
			"no-broker": "a broker to run, and exchanges and bindings to get right",
			"replay":    "a classic queue removes a message when it is acknowledged; there is nothing left to read back",
		},
		Satisfies: map[string]string{
			"cross-process":   "consumers in other processes bind to the exchange over the network",
			"at-least-once":   "an unacknowledged message is requeued when the channel closes",
			"fan-out":         "several queues bound to one exchange each receive every message",
			"consumer-groups": "several consumers on one queue compete for its messages",
		},
		Unverified: map[string]string{
			"durable": "a durable exchange, a durable queue and persistent messages are all needed, and this tile declares none of them",
			"ordered": "one consumer on one queue sees publisher order, but nobody has checked what requeueing does to it",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 4},
			{Dim: "maintenance", Value: 4},
			{Dim: "dependency", Value: 3},
			{Dim: "runtime", Value: 3},
			{Dim: "operations", Value: 5, Source: "derived", Note: "a broker to run, plus the topology to declare and keep in step"},
		},
		Impl: &compile.BusTransport{
			Suffix: "RabbitBus",
			Topics: []string{"rabbitmq", "amqp"},
			Import: [2]string{"amqp", "github.com/rabbitmq/amqp091-go"},
			Emit:   emitRabbitBus,
			Hint:   rabbitHint,
		},
	})
	compile.RegisterAlias("rabbitmq", "rabbitmq")
}

func emitKafkaBus(in compile.BusInput) ([]*sexp.Node, error) {
	return busDecl(in, "Kafka", []*sexp.Node{
		sexp.L(sexp.Sym("field"), sexp.Sym("client"), sexp.Sym("*kgo.Client")),
		sexp.L(sexp.Sym("field"), sexp.Sym("topic"), sexp.Sym("string")),
	}, sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("client"), sexp.Sym("*kgo.Client")), sexp.L(sexp.Sym("topic"), sexp.Sym("string"))),
		fmt.Sprintf("return &%s{client: client, topic: topic}", in.Impl),
		"Events are published to one topic, keyed so that everything about one entity stays in order.")
}

func emitRedisBus(in compile.BusInput) ([]*sexp.Node, error) {
	return busDecl(in, "Redis Streams", []*sexp.Node{
		sexp.L(sexp.Sym("field"), sexp.Sym("rdb"), sexp.Sym("*redis.Client")),
		sexp.L(sexp.Sym("field"), sexp.Sym("stream"), sexp.Sym("string")),
	}, sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("rdb"), sexp.Sym("*redis.Client")), sexp.L(sexp.Sym("stream"), sexp.Sym("string"))),
		fmt.Sprintf("return &%s{rdb: rdb, stream: stream}", in.Impl),
		"Events are appended to one stream, so a handler that starts later can read back what it missed.")
}

func emitRabbitBus(in compile.BusInput) ([]*sexp.Node, error) {
	return busDecl(in, "RabbitMQ", []*sexp.Node{
		sexp.L(sexp.Sym("field"), sexp.Sym("ch"), sexp.Sym("*amqp.Channel")),
		sexp.L(sexp.Sym("field"), sexp.Sym("exchange"), sexp.Sym("string")),
	}, sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("ch"), sexp.Sym("*amqp.Channel")), sexp.L(sexp.Sym("exchange"), sexp.Sym("string"))),
		fmt.Sprintf("return &%s{ch: ch, exchange: exchange}", in.Impl),
		"Events are published to one topic exchange, with the event type as the routing key.")
}

// busDecl is the shape every remote transport has: a struct holding a
// client and a name, and a constructor taking both. The methods are holes,
// so this is all the tile has to build.
func busDecl(in compile.BusInput, what string, fields []*sexp.Node, params *sexp.Node, body, note string) ([]*sexp.Node, error) {
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s is a Bus backed by %s. %s", in.Impl, what, note))))
	decl.List = append(decl.List, fields...)
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s returns a %s over the given client.", in.Impl, in.Impl))),
		params,
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(body)))
	return []*sexp.Node{decl, ctor}, nil
}

func kafkaHint(method, event string) string {
	switch method {
	case "Publish":
		return fmt.Sprintf("Encode e as JSON and produce it to s.topic with kgo, using the entity's id as the record key "+
			"so that everything about one entity stays in order. The record's type header is %q. Return the produce error.", event)
	case "On":
		return fmt.Sprintf("Consume s.topic with a consumer group, decode records whose type header is %q as JSON into %s, "+
			"and call h for each. Commit only after h returns nil, so a failed handler sees the record again. "+
			"Return a func that stops the consumer.", event, event)
	}
	return ""
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

func rabbitHint(method, event string) string {
	switch method {
	case "Publish":
		return fmt.Sprintf("Publish e as JSON to s.exchange with routing key %q, marked persistent. Return the publish error.", event)
	case "On":
		return fmt.Sprintf("Declare a queue bound to s.exchange with routing key %q, consume it, decode each delivery as JSON "+
			"into %s, and call h. Ack on nil and Nack with requeue on error. Return a func that cancels the consumer.", event, event)
	}
	return ""
}
