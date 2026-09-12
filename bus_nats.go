package main

import "fmt"

// The NATS transport: events cross process boundaries and, with JetStream,
// survive a restart. It costs a dependency and a service to run, and the
// LLM writes the publish and subscribe bodies against the NATS API, so it
// only wins when the spec asks for something local-bus cannot do.
func init() {
	RegisterOffer(&Offer{
		Tile:       "nats-bus",
		Capability: "event-bus",
		Doc:        "NATS bus: events cross processes and survive a restart (JetStream)",
		Cost:       Cost{{"llm-work", 5}, {"maintenance", 3}, {"dependency", 4}, {"runtime", 2}},
		Impl: &BusTransport{
			Suffix: "NatsBus",
			Emit:   emitNatsBus,
			Topics: []string{"nats"},
			Import: [2]string{"nats", "github.com/nats-io/nats.go"},
			Hint:   natsHint,
		},
	})
	registerAlias("nats-bus", "nats")
}

// emitNatsBus declares the implementation and leaves one hole per method:
// publishing and subscribing are a few lines of NATS API each, and which
// encoding and subject naming a team wants is not tilegen's to decide.
func emitNatsBus(in BusInput) ([]*Node, error) {
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s is a Bus backed by NATS. Events are published on a subject per\nevent type, so handlers in other processes receive them too.", in.Impl))),
		L(Sym("field"), Sym("conn"), Sym("*nats.Conn")),
		L(Sym("field"), Sym("subject"), Sym("string")))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s returns a %s publishing under the given subject prefix.", in.Impl, in.Impl))),
		L(Sym("params"), L(Sym("conn"), Sym("*nats.Conn")), L(Sym("subject"), Sym("string"))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{conn: conn, subject: subject}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

// natsHint tells the LLM what each method must do, in NATS terms.
func natsHint(method, event string) string {
	switch method {
	case "Publish":
		return fmt.Sprintf("Encode e as JSON and publish it on s.subject+\".%s\" with s.conn. Return the publish error.", event)
	case "On":
		return fmt.Sprintf("Subscribe to s.subject+\".%s\" with s.conn, decode each message as JSON into %s, and call h. "+
			"Return a func that unsubscribes. Deliver errors from h to the connection's error handler, since NATS has no reply here.", event, event)
	}
	return ""
}
