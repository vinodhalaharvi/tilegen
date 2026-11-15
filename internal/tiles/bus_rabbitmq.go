package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
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

func emitRabbitBus(in compile.BusInput) ([]*sexp.Node, error) {
	return busDecl(in, "RabbitMQ", []*sexp.Node{
		sexp.L(sexp.Sym("field"), sexp.Sym("ch"), sexp.Sym("*amqp.Channel")),
		sexp.L(sexp.Sym("field"), sexp.Sym("exchange"), sexp.Sym("string")),
	}, sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("ch"), sexp.Sym("*amqp.Channel")), sexp.L(sexp.Sym("exchange"), sexp.Sym("string"))),
		fmt.Sprintf("return &%s{ch: ch, exchange: exchange}", in.Impl),
		"Events are published to one topic exchange, with the event type as the routing key.")
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
