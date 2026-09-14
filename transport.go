package main

// A transport carries messages in from outside the program. An event bus
// carries a package's own events between its parts. They are different
// jobs with different vocabularies, and some systems do both.
//
// Kafka is the worked example, and it registers twice: once here and once
// in bus_remote.go. That is deliberate, and it is not a workaround for a
// missing list of capabilities on Offer. A tile's cost, its legality and
// the code it emits are all per-capability, exactly as a struct
// implementing two interfaces has two different method sets:
//
//   - cost differs: consuming a topic with offsets and a rebalance
//     callback is more to write than publishing to one;
//   - legality differs: (replay) is a question about a bus with a log,
//     and means nothing at an inbound edge that has already been read;
//   - the emitted code differs: a source, not a Bus.
//
// One Offer with a list of capabilities would force one answer to all
// three. Two offers give three answers, and RegisterOffer already allows
// it: its duplicate check is scoped to the capability.
//
// No new requirement words appear here. Everything transport asks for is
// already in the shared vocabulary, which is the property that makes
// (durable) mean one thing across the language.

func init() {
	RegisterCapability(&Capability{
		Name: "transport",
		Doc:  "carries messages in from outside the program",
		Requirements: []string{"cross-process", "durable", "replay", "ordered",
			"at-least-once", "no-broker"},
	})

	// Kafka, the second time. Compare the numbers with the event-bus
	// offer in bus_remote.go: same cluster, different work.
	RegisterOffer(&Offer{
		Tile:       "kafka",
		Capability: "transport",
		Doc:        "Kafka as an inbound edge: consume a topic, at your own offset",
		IllegalFor: map[string]string{
			"no-broker": "a cluster to run, partitions to size, and retention and consumer lag to watch",
		},
		Satisfies: map[string]string{
			"cross-process": "the producers are other systems entirely; that is what an inbound edge is",
			"durable":       "a record is on the partition's replicas before it is acknowledged",
			"replay":        "a consumer can start at any offset still inside retention",
			"ordered":       "a partition is an ordered log, and one key goes to one partition",
			"at-least-once": "an uncommitted offset is redelivered after a rebalance or a restart",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 7, Source: "derived", Note: "a consumer group, offset commits after the handler, and a rebalance callback: more than the publish side"},
			{Dim: "maintenance", Value: 5},
			{Dim: "dependency", Value: 4, Source: "derived", Note: "a client library with a large transitive graph"},
			{Dim: "runtime", Value: 4},
			{Dim: "operations", Value: 9, Source: "derived", Note: "the heaviest thing in the registry to run: brokers, partitions, rebalances, lag, and someone on call for them"},
		},
		Impl: &Transport{
			Suffix: "KafkaSource",
			Topics: []string{"kafka"},
			Import: [2]string{"kgo", "github.com/twmb/franz-go/pkg/kgo"},
		},
	})

	// Core NATS, which is the honest opposite: nothing is kept, so it is
	// illegal for three of the six and very cheap for the rest.
	RegisterOffer(&Offer{
		Tile:       "nats-core",
		Capability: "transport",
		Doc:        "core NATS: subjects and queue groups, with nothing kept",
		IllegalFor: map[string]string{
			"durable":       "core NATS is fire-and-forget: a message with no connected consumer is gone",
			"replay":        "core NATS keeps no stream; JetStream is the same broker with a log",
			"at-least-once": "core NATS delivers at most once and does not redeliver on failure",
			"no-broker":     "there is a server to run, though only one binary and no state",
		},
		Satisfies: map[string]string{
			"cross-process": "publishers in other processes reach this subject over the network",
		},
		Unverified: map[string]string{
			"ordered": "messages from one publisher arrive in order on one connection, but nobody has checked what holds across a reconnect",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 2, Source: "derived", Note: "subscribe to a subject and decode; there are no offsets to keep"},
			{Dim: "maintenance", Value: 1},
			{Dim: "dependency", Value: 2},
			{Dim: "runtime", Value: 1},
			{Dim: "operations", Value: 2, Source: "derived", Note: "one binary, no storage, nothing to size"},
		},
		Impl: &Transport{
			Suffix: "NatsSource",
			Topics: []string{"nats"},
			Import: [2]string{"nats", "github.com/nats-io/nats.go"},
		},
	})
	// No alias here: bus_nats.go already registers "nats" for this tile,
	// and one tile has one alias however many capabilities it offers.

	// MQTT is missing on purpose. Every clause it would carry is
	// broker-dependent (Mosquitto, EMQX and HiveMQ differ on what a
	// persistent session and a shared subscription guarantee), and a
	// clause written from memory is the failure this registry exists to
	// avoid. It needs someone who runs one.
}

// Transport is what a transport offer contributes: the implementation
// type's name and the library its code needs. Emit and Hint arrive with
// the (ingest ...) form, which is what defines the input a source is
// generated from; declaring func fields that nothing calls would only
// pretend the shape is settled.
type Transport struct {
	Suffix string    // the implementation type: KafkaSource
	Topics []string  // GitHub topics a project using it gets
	Import [2]string // qualifier and import path its code needs, if any
}
