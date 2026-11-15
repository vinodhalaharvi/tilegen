package compile

// A transport carries messages in from outside the program. An event bus
// carries a package's own events between its parts. They are different
// jobs with different vocabularies, and some systems do both.
//
// Kafka is the worked example, and it registers twice in
// internal/tiles/bus_kafka.go: once as a bus and once as a transport.
// That is deliberate, and it is not a workaround for a
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
