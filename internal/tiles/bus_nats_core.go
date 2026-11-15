package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "nats-core",
		Engine:     "nats",
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
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "subscribe and publish on a subject; there is nothing to acknowledge"},
			{Dim: "maintenance", Value: 2},
			{Dim: "dependency", Value: 4},
			{Dim: "runtime", Value: 1},
			{Dim: "operations", Value: 2, Source: "derived", Note: "one binary, no storage, nothing to size"},
		},
		Impl: &compile.BusTransport{
			Suffix: "NatsBus",
			Emit:   emitNatsBus,
			Topics: []string{"nats"},
			Import: [2]string{"nats", "github.com/nats-io/nats.go"},
			Hint:   natsHint,
		},
	})
	compile.RegisterAlias("nats-core", "nats")
}

// emitNatsBus declares the implementation and leaves one hole per method:
// publishing and subscribing are a few lines of NATS API each, and which
// encoding and subject naming a team wants is not tilegen's to decide.
func emitNatsBus(in compile.BusInput) ([]*sexp.Node, error) {
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s is a Bus backed by core NATS. Events are published on a subject\nper event type, so handlers in other processes receive them. Nothing is\nkept: a message published with no subscriber connected is gone.", in.Impl))),
		sexp.L(sexp.Sym("field"), sexp.Sym("conn"), sexp.Sym("*nats.Conn")),
		sexp.L(sexp.Sym("field"), sexp.Sym("subject"), sexp.Sym("string")))
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s returns a %s publishing under the given subject prefix.", in.Impl, in.Impl))),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("conn"), sexp.Sym("*nats.Conn")), sexp.L(sexp.Sym("subject"), sexp.Sym("string"))),
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &%s{conn: conn, subject: subject}", in.Impl))))
	return []*sexp.Node{decl, ctor}, nil
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

// Core NATS registers a second time, as an inbound edge: nothing is kept,
// so it is illegal for three of the six requirements and very cheap for
// the rest.
//
// There is no alias here: the registration above already gives this tile
// "nats", and one tile has one alias however many capabilities it offers.
func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "nats-core",
		Engine:     "nats",
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
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 2, Source: "derived", Note: "subscribe to a subject and decode; there are no offsets to keep"},
			{Dim: "maintenance", Value: 1},
			{Dim: "dependency", Value: 2},
			{Dim: "runtime", Value: 1},
			{Dim: "operations", Value: 2, Source: "derived", Note: "one binary, no storage, nothing to size"},
		},
		Impl: &compile.Transport{
			Suffix: "NatsSource",
			Topics: []string{"nats"},
			Import: [2]string{"nats", "github.com/nats-io/nats.go"},
		},
	})
}
