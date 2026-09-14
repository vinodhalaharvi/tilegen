package main

import "fmt"

// NATS, as two tiles rather than one.
//
// There used to be a single "nats-bus" that declared itself illegal only
// for (no-broker) and emitted s.conn.Publish. Selection therefore chose it
// for a spec asking (durable) and (replay), and the code it generated
// satisfied neither: core NATS drops a message with no connected
// subscriber. The tile was not lying, it was silent, and silence used to
// read as yes.
//
// The two halves are different systems that happen to ship in one binary,
// and they hold different types, which is what makes a claim reachable
// from the hole rather than merely asserted beside it: core NATS gets a
// *nats.Conn, JetStream gets a jetstream.JetStream. A reviewer who sees a
// durability claim next to a *nats.Conn now has something to object to.
func init() {
	RegisterOffer(&Offer{
		Tile:       "nats-core",
		Capability: "event-bus",
		Doc:        "core NATS: subjects and queue groups, with nothing kept",
		IllegalFor: map[string]string{
			"durable":       "core NATS is fire-and-forget: a message with no connected subscriber is gone",
			"replay":        "core NATS keeps no stream; there is nothing to read back",
			"at-least-once": "core NATS delivers at most once and does not redeliver on failure",
			"no-broker":     "there is a server to run, though only one binary and no state",
		},
		Satisfies: map[string]string{
			"cross-process":   "subscribers in other processes receive what is published on a subject",
			"fan-out":         "every plain subscriber to a subject receives every message",
			"consumer-groups": "a queue group delivers each message to one of its members",
		},
		Unverified: map[string]string{
			"ordered": "messages from one publisher arrive in order on one connection, but nobody has checked what holds across a reconnect",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "subscribe and publish on a subject; there is nothing to acknowledge"},
			{Dim: "maintenance", Value: 2},
			{Dim: "dependency", Value: 4},
			{Dim: "runtime", Value: 1},
			{Dim: "operations", Value: 2, Source: "derived", Note: "one binary, no storage, nothing to size"},
		},
		Impl: &BusTransport{
			Suffix: "NatsBus",
			Emit:   emitNatsBus,
			Topics: []string{"nats"},
			Import: [2]string{"nats", "github.com/nats-io/nats.go"},
			Hint:   natsHint,
		},
	})
	registerAlias("nats-core", "nats")

	RegisterOffer(&Offer{
		Tile:       "nats-jetstream",
		Capability: "event-bus",
		Doc:        "NATS JetStream: the same server with a log, so events are kept and can be read back",
		IllegalFor: map[string]string{
			"no-broker": "a server to run, with streams and consumers to size and retain",
		},
		Satisfies: map[string]string{
			"durable":         "a publish is acknowledged only once the stream has stored the message",
			"cross-process":   "consumers in other processes read the stream over the network",
			"replay":          "a consumer can start at any sequence or time in the stream",
			"at-least-once":   "an unacknowledged message is redelivered until it is acknowledged or expires",
			"fan-out":         "several consumers on one stream each see every message",
			"consumer-groups": "a queue consumer shares its messages among its members",
			"ordered":         "a stream is an ordered log, and a consumer reads it in sequence",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 5, Source: "derived", Note: "publish with an ack, and a consumer with explicit acknowledgement"},
			{Dim: "maintenance", Value: 3},
			{Dim: "dependency", Value: 4},
			{Dim: "runtime", Value: 2},
			{Dim: "operations", Value: 4, Source: "derived", Note: "one binary with no dependencies, but streams, consumers and retention to configure"},
		},
		Impl: &BusTransport{
			Suffix: "JetStreamBus",
			Emit:   emitJetStreamBus,
			Topics: []string{"nats", "jetstream"},
			Import: [2]string{"jetstream", "github.com/nats-io/nats.go/jetstream"},
			Hint:   jetStreamHint,
		},
	})
	registerAlias("nats-jetstream", "jetstream")
}

// emitNatsBus declares the implementation and leaves one hole per method:
// publishing and subscribing are a few lines of NATS API each, and which
// encoding and subject naming a team wants is not tilegen's to decide.
func emitNatsBus(in BusInput) ([]*Node, error) {
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s is a Bus backed by core NATS. Events are published on a subject\nper event type, so handlers in other processes receive them. Nothing is\nkept: a message published with no subscriber connected is gone.", in.Impl))),
		L(Sym("field"), Sym("conn"), Sym("*nats.Conn")),
		L(Sym("field"), Sym("subject"), Sym("string")))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s returns a %s publishing under the given subject prefix.", in.Impl, in.Impl))),
		L(Sym("params"), L(Sym("conn"), Sym("*nats.Conn")), L(Sym("subject"), Sym("string"))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{conn: conn, subject: subject}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

func natsHint(method, event string) string {
	switch method {
	case "Publish":
		return fmt.Sprintf("Encode e as JSON and publish it on s.subject+\".%s\" with s.conn. Return the publish error. "+
			"This is core NATS, so a successful publish means the server accepted the message, not that anyone received it.", event)
	case "On":
		return fmt.Sprintf("Subscribe to s.subject+\".%s\" with s.conn, decode each message as JSON into %s, and call h. "+
			"Return a func that unsubscribes. Deliver errors from h to the connection's error handler, since NATS has no reply here.", event, event)
	}
	return ""
}

// emitJetStreamBus holds a jetstream.JetStream rather than a *nats.Conn,
// so the durability this tile claims is something the hole can reach for.
// A tile that claims (durable) and hands the LLM a bare connection is
// claiming something it gave nobody the means to do.
func emitJetStreamBus(in BusInput) ([]*Node, error) {
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s is a Bus backed by NATS JetStream. Events go to a stream, so a\nconsumer that was not running still receives them, and a consumer can read\nback from any point.", in.Impl))),
		L(Sym("field"), Sym("js"), Sym("jetstream.JetStream")),
		L(Sym("field"), Sym("subject"), Sym("string")))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s returns a %s publishing under the given subject prefix.\nThe stream covering that prefix must already exist.", in.Impl, in.Impl))),
		L(Sym("params"), L(Sym("js"), Sym("jetstream.JetStream")), L(Sym("subject"), Sym("string"))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{js: js, subject: subject}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

func jetStreamHint(method, event string) string {
	switch method {
	case "Publish":
		return fmt.Sprintf("Encode e as JSON and publish it on s.subject+\".%s\" with s.js.Publish, which returns only once "+
			"the stream has stored the message. Return that error. Do not reach for a plain connection publish: this tile was "+
			"chosen because the spec asked for durability, and an unacknowledged publish does not provide it.", event)
	case "On":
		return fmt.Sprintf("Create or look up a consumer on the stream covering s.subject+\".%s\", decode each message as JSON "+
			"into %s, and call h. Acknowledge only after h returns nil, and negatively acknowledge otherwise, so a failed handler "+
			"sees the message again. Return a func that stops the consumer.", event, event)
	}
	return ""
}
