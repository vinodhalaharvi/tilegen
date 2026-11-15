package tiles

import (
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "local-bus",
		Engine:     compile.NoEngine,
		Capability: "event-bus",
		Doc:        "in-process bus: synchronous delivery, ordered, errors joined",
		Cost:       compile.Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 0}, {Dim: "dependency", Value: 0}, {Dim: "runtime", Value: 1}},
		IllegalFor: map[string]string{
			"durable":         "an in-process bus loses undelivered events on restart",
			"replay":          "nothing is kept: a handler that subscribes later sees only what comes next",
			"cross-process":   "an in-process bus only reaches handlers in this program",
			"consumer-groups": "every handler is called for every event; there is no way to compete for one",
			"at-least-once":   "a handler that returns an error is not called again: the error is joined and returned to the publisher",
		},
		Satisfies: map[string]string{
			"ordered":   "Publish calls every handler in subscription order, in the publisher's goroutine",
			"fan-out":   "every subscriber is called for every event",
			"no-broker": "a slice of handlers and a mutex; there is nothing to run",
		},
		Impl: &compile.BusTransport{Suffix: "LocalBus", Emit: emitLocalBus},
	})
	compile.RegisterAlias("local-bus", "local")
}

// emitLocalBus is the in-process transport: one handler list per event,
// delivered synchronously in the publisher's goroutine.
func emitLocalBus(in compile.BusInput) ([]*sexp.Node, error) {
	ctxParam := ""
	if in.CtxType != "" {
		ctxParam = "ctx context.Context, "
	}
	local := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str("LocalBus is an in-process Bus. Publish calls every handler in subscription\n"+
		"order, in the publisher's goroutine, and returns their errors joined.\n"+
		"Handlers may subscribe and unsubscribe at any time, even from a handler.")))
	var methods []*sexp.Node
	for _, name := range in.Events {
		field := compile.LowerFirstWord(name) + "Handlers"
		handler := fmt.Sprintf("func(%s%s) error", in.CtxType, name)
		params := sexp.L(sexp.Sym("params"))
		if in.CtxType != "" {
			params.List = append(params.List, sexp.L(sexp.Sym("ctx"), sexp.Sym("context.Context")))
		}
		params.List = append(params.List, sexp.L(sexp.Sym("e"), sexp.Sym(name)))
		local.List = append(local.List, sexp.L(sexp.Sym("field"), sexp.Sym(field), sexp.Str("subscribers["+name+"]")))
		methods = append(methods,
			sexp.L(sexp.Sym("go/func"), sexp.Sym("Publish"+name), sexp.L(sexp.Sym("recv"), sexp.Sym("b"), sexp.Sym("*"+in.Impl)), params,
				sexp.L(sexp.Sym("returns"), sexp.Sym("error")), sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return b.%s.publish(%se)", field, in.CtxArg)))),
			sexp.L(sexp.Sym("go/func"), sexp.Sym("On"+name), sexp.L(sexp.Sym("recv"), sexp.Sym("b"), sexp.Sym("*"+in.Impl)), sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("h"), sexp.Str(handler))),
				sexp.L(sexp.Sym("returns"), sexp.Str("func()")), sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return b.%s.add(h)", field)))))
	}
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s returns an empty %s.", in.Impl, in.Impl))),
		sexp.L(sexp.Sym("params")), sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)), sexp.L(sexp.Sym("body"), sexp.Str("return &"+in.Impl+"{}")))
	out := append([]*sexp.Node{local, ctor}, methods...)
	return append(out, sexp.L(sexp.Sym("go/raw"), sexp.Str(subscribersHelper(in.CtxType, ctxParam, in.CtxArg)))), nil
}

// subscribersHelper is the generic handler list every LocalBus field uses.
func subscribersHelper(ctxType, ctxParam, ctxArg string) string {
	return strings.NewReplacer("CTXTYPE", ctxType, "CTXPARAM", ctxParam, "CTXARG", ctxArg).Replace(`// subscribers holds the handlers of one event type, in subscription order.
type subscribers[E any] struct {
	mu   sync.RWMutex
	next int
	list []subscriber[E]
}

type subscriber[E any] struct {
	id int
	fn func(CTXTYPEE) error
}

// add subscribes fn and returns a func that unsubscribes it.
func (s *subscribers[E]) add(fn func(CTXTYPEE) error) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := s.next
	s.list = append(s.list, subscriber[E]{id: id, fn: fn})
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, x := range s.list {
			if x.id == id {
				s.list = append(s.list[:i:i], s.list[i+1:]...)
				return
			}
		}
	}
}

// publish calls every handler subscribed when it starts, in order, and
// returns their errors joined.
func (s *subscribers[E]) publish(CTXPARAMe E) error {
	s.mu.RLock()
	list := append([]subscriber[E](nil), s.list...)
	s.mu.RUnlock()
	var errs []error
	for _, x := range list {
		if err := x.fn(CTXARGe); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}`)
}
