package main

import "fmt"

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
	RegisterOffer(&Offer{
		Tile:       "kafka",
		Capability: "event-bus",
		Doc:        "Kafka: a partitioned log, ordered per key, read back at will",
		IllegalFor: map[string]string{
			"no-broker": "a cluster to run, partitions to size, and retention and consumer lag to watch",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 5},
			{Dim: "maintenance", Value: 5},
			{Dim: "dependency", Value: 4, Source: "derived", Note: "a client library with a large transitive graph"},
			{Dim: "runtime", Value: 4},
			{Dim: "operations", Value: 9, Source: "derived", Note: "the heaviest thing in the registry to run: brokers, partitions, rebalances, lag, and someone on call for them"},
		},
		Impl: &BusTransport{
			Suffix: "KafkaBus",
			Topics: []string{"kafka"},
			Import: [2]string{"kgo", "github.com/twmb/franz-go/pkg/kgo"},
			Emit:   emitKafkaBus,
			Hint:   kafkaHint,
		},
	})
	registerAlias("kafka", "kafka")

	RegisterOffer(&Offer{
		Tile:       "redis-streams",
		Capability: "event-bus",
		Doc:        "Redis Streams: an append-only log with consumer groups",
		IllegalFor: map[string]string{
			"no-broker": "there is a Redis to run",
			"durable":   "durability is an operator setting: under the default append-only policy a second of writes can be lost on a crash",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 4},
			{Dim: "maintenance", Value: 3},
			{Dim: "dependency", Value: 2},
			{Dim: "runtime", Value: 2},
			{Dim: "operations", Value: 3, Source: "derived", Note: "one server, but stream trimming and group lag to watch"},
		},
		Impl: &BusTransport{
			Suffix: "RedisBus",
			Topics: []string{"redis"},
			Import: [2]string{"redis", "github.com/redis/go-redis/v9"},
			Emit:   emitRedisBus,
			Hint:   redisHint,
		},
	})
	registerAlias("redis-streams", "redis")

	RegisterOffer(&Offer{
		Tile:       "rabbitmq",
		Capability: "event-bus",
		Doc:        "RabbitMQ: exchanges, bindings and competing consumers",
		IllegalFor: map[string]string{
			"no-broker": "a broker to run, and exchanges and bindings to get right",
			"replay":    "a classic queue removes a message when it is acknowledged; there is nothing left to read back",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 4},
			{Dim: "maintenance", Value: 4},
			{Dim: "dependency", Value: 3},
			{Dim: "runtime", Value: 3},
			{Dim: "operations", Value: 5, Source: "derived", Note: "a broker to run, plus the topology to declare and keep in step"},
		},
		Impl: &BusTransport{
			Suffix: "RabbitBus",
			Topics: []string{"rabbitmq", "amqp"},
			Import: [2]string{"amqp", "github.com/rabbitmq/amqp091-go"},
			Emit:   emitRabbitBus,
			Hint:   rabbitHint,
		},
	})
	registerAlias("rabbitmq", "rabbitmq")
}

func emitKafkaBus(in BusInput) ([]*Node, error) {
	return busDecl(in, "Kafka", []*Node{
		L(Sym("field"), Sym("client"), Sym("*kgo.Client")),
		L(Sym("field"), Sym("topic"), Sym("string")),
	}, L(Sym("params"), L(Sym("client"), Sym("*kgo.Client")), L(Sym("topic"), Sym("string"))),
		fmt.Sprintf("return &%s{client: client, topic: topic}", in.Impl),
		"Events are published to one topic, keyed so that everything about one entity stays in order.")
}

func emitRedisBus(in BusInput) ([]*Node, error) {
	return busDecl(in, "Redis Streams", []*Node{
		L(Sym("field"), Sym("rdb"), Sym("*redis.Client")),
		L(Sym("field"), Sym("stream"), Sym("string")),
	}, L(Sym("params"), L(Sym("rdb"), Sym("*redis.Client")), L(Sym("stream"), Sym("string"))),
		fmt.Sprintf("return &%s{rdb: rdb, stream: stream}", in.Impl),
		"Events are appended to one stream, so a handler that starts later can read back what it missed.")
}

func emitRabbitBus(in BusInput) ([]*Node, error) {
	return busDecl(in, "RabbitMQ", []*Node{
		L(Sym("field"), Sym("ch"), Sym("*amqp.Channel")),
		L(Sym("field"), Sym("exchange"), Sym("string")),
	}, L(Sym("params"), L(Sym("ch"), Sym("*amqp.Channel")), L(Sym("exchange"), Sym("string"))),
		fmt.Sprintf("return &%s{ch: ch, exchange: exchange}", in.Impl),
		"Events are published to one topic exchange, with the event type as the routing key.")
}

// busDecl is the shape every remote transport has: a struct holding a
// client and a name, and a constructor taking both. The methods are holes,
// so this is all the tile has to build.
func busDecl(in BusInput, what string, fields []*Node, params *Node, body, note string) ([]*Node, error) {
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s is a Bus backed by %s. %s", in.Impl, what, note))))
	decl.List = append(decl.List, fields...)
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s returns a %s over the given client.", in.Impl, in.Impl))),
		params,
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(body)))
	return []*Node{decl, ctor}, nil
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
