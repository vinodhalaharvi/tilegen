package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

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
}

func emitKafkaBus(in compile.BusInput) ([]*sexp.Node, error) {
	return busDecl(in, "Kafka", []*sexp.Node{
		sexp.L(sexp.Sym("field"), sexp.Sym("client"), sexp.Sym("*kgo.Client")),
		sexp.L(sexp.Sym("field"), sexp.Sym("topic"), sexp.Sym("string")),
	}, sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("client"), sexp.Sym("*kgo.Client")), sexp.L(sexp.Sym("topic"), sexp.Sym("string"))),
		fmt.Sprintf("return &%s{client: client, topic: topic}", in.Impl),
		"Events are published to one topic, keyed so that everything about one entity stays in order.")
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
