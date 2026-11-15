package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "nats-jetstream",
		Engine:     "nats",
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
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 5, Source: "derived", Note: "publish with an ack, and a consumer with explicit acknowledgement"},
			{Dim: "maintenance", Value: 3},
			{Dim: "dependency", Value: 4},
			{Dim: "runtime", Value: 2},
			{Dim: "operations", Value: 4, Source: "derived", Note: "one binary with no dependencies, but streams, consumers and retention to configure"},
		},
		Impl: &compile.BusTransport{
			Suffix: "JetStreamBus",
			Emit:   emitJetStreamBus,
			Topics: []string{"nats", "jetstream"},
			Import: [2]string{"jetstream", "github.com/nats-io/nats.go/jetstream"},
			Hint:   jetStreamHint,
		},
	})
	compile.RegisterAlias("nats-jetstream", "jetstream")
}

// emitJetStreamBus holds a jetstream.JetStream rather than a *nats.Conn,
// so the durability this tile claims is something the hole can reach for.
// A tile that claims (durable) and hands the LLM a bare connection is
// claiming something it gave nobody the means to do.
func emitJetStreamBus(in compile.BusInput) ([]*sexp.Node, error) {
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s is a Bus backed by NATS JetStream. Events go to a stream, so a\nconsumer that was not running still receives them, and a consumer can read\nback from any point.", in.Impl))),
		sexp.L(sexp.Sym("field"), sexp.Sym("js"), sexp.Sym("jetstream.JetStream")),
		sexp.L(sexp.Sym("field"), sexp.Sym("subject"), sexp.Sym("string")))
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s returns a %s publishing under the given subject prefix.\nThe stream covering that prefix must already exist.", in.Impl, in.Impl))),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("js"), sexp.Sym("jetstream.JetStream")), sexp.L(sexp.Sym("subject"), sexp.Sym("string"))),
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &%s{js: js, subject: subject}", in.Impl))))
	return []*sexp.Node{decl, ctor}, nil
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
