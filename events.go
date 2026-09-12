package main

import (
	"fmt"
	goversion "go/version"
	"path"
	"strings"
)

// (package sharing
//   (events
//     (doc "Events about sharing notes.")
//     (event NoteShared   (field NoteID int64) (field Email string))
//     (event NoteUnshared (field NoteID int64))))
//
// generates, with no holes, in <package>/events_gen.go:
//
//   - one struct per event (fields get json tags per the house style);
//   - a typed Bus interface: PublishNoteShared(ctx, e) error and
//     OnNoteShared(h) (unsubscribe func());
//   - LocalBus, an in-process Bus: Publish calls every handler in
//     subscription order, in the publisher's goroutine, and returns their
//     errors joined. No goroutines to leak, ordering guaranteed, errors
//     reach the publisher. Asynchronous or networked buses are future
//     event-bus tiles, chosen like storage backends.
//
// It is the first tile that registers itself: nothing in the core names it.

// local-bus is the in-process transport: synchronous delivery, no
// dependencies, no service to run. It cannot serve a bus that must
// survive a restart or reach another process.
func init() {
	RegisterOffer(&Offer{
		Tile:       "local-bus",
		Capability: "event-bus",
		Doc:        "in-process bus: synchronous delivery, ordered, errors joined",
		Cost:       Cost{{"llm-work", 0}, {"maintenance", 0}, {"dependency", 0}, {"runtime", 1}},
		IllegalFor: map[string]string{
			"durable":       "an in-process bus loses undelivered events on restart",
			"cross-process": "an in-process bus only reaches handlers in this program",
		},
		Impl: &BusTransport{Suffix: "LocalBus", Emit: emitLocalBus},
	})
	registerAlias("local-bus", "local")

	RegisterPackageTile(&PackageTile{
		Form: "events",
		Pass: Select,
		Rule: Rule{Name: "events", Pattern: Pat("(events ?items...)"), Then: selectEvents,
			Produces: "event-bus", Doc: "event structs, a typed Bus interface, and an in-process LocalBus",
			Cost: Cost{{"llm-work", 0}, {"maintenance", 0}, {"dependency", 0}}},
		Validate: validateEvents,
	})
}

func validateEvents(v *validator, n *Node, types map[string]bool) {
	if v.goVersion != "" && goversion.Compare("go"+v.goVersion, "go1.20") < 0 {
		v.bad(n, "(events ...) needs (go 1.20) or later (generics and errors.Join); the project has (go %s)", v.goVersion)
	}
	for _, name := range []string{"Bus", "LocalBus"} {
		if types[name] {
			v.bad(n, "(events ...) generates %s, but the package already declares it (one events form per package)", name)
		}
		types[name] = true
	}
	count := 0
	for _, it := range n.Args() {
		if it.IsList && len(it.List) == 1 {
			validateRequirements("event-bus", []string{it.Head()}, it, v)
			continue
		}
		switch it.Head() {
		case "doc":
			v.shape(it, "(doc ?text)")
		case "event":
			count++
			v.structLike(it, types) // an event is a struct: name, fields, doc
		default:
			v.bad(it, "events contain (event Name (field ...)...), (doc ...) and requirements like (durable), got %s%s",
				short(it), didYouMean(it.Head(), append([]string{"event", "doc"}, capabilities["event-bus"].Requirements...)))
		}
	}
	if count == 0 {
		v.bad(n, "(events ...) needs at least one (event ...)")
	}
}

// BusTransport is what an event-bus offer contributes: the implementation
// type's name and the code that emits it. The event structs and the Bus
// interface are the same whatever the transport, so the tile makes those.
type BusTransport struct {
	Suffix string    // the implementation type: LocalBus, NatsBus
	Topics []string  // GitHub topics a project using it gets
	Import [2]string // qualifier and import path its code needs, if any
	Emit   func(in BusInput) ([]*Node, error)

	// Hints, when the transport leaves its methods as holes. Empty means
	// the transport emits complete code, as local-bus does.
	Hint func(method, event string) string
}

// BusInput is what a transport needs to emit its implementation.
type BusInput struct {
	C       *Ctx
	Pkg     *PkgScope
	Impl    string   // the type name to declare
	Events  []string // event type names, in spec order
	CtxType string   // "context.Context, " or ""
	CtxArg  string   // "ctx, " or ""
	Methods []*Node  // the Bus interface's methods, for reference
}

// selectEvents makes the parts every transport shares: one struct per
// event, and the Bus interface. The transport chosen for this package's
// event-bus need supplies the implementation.
func selectEvents(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	cov := c.Cover[pkg.Name+".Bus"]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s.Bus", pkg.Name)
	}
	tr, ok := cov.Offer.Impl.(*BusTransport)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s.Bus does not implement event buses", pkg.Name)
	}
	ctxType, ctxArg := "", ""
	if c.Cfg.ContextFirst {
		ctxType, ctxArg = "context.Context, ", "ctx, "
	}
	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("Bus publishes the events of package %s and delivers them to handlers.", pkg.Name)
	}
	bus := L(Sym("go/interface"), Sym("Bus"), L(Sym("doc"), Str(doc)))
	var decls []*Node
	var events []string
	for _, ev := range n.FindAll("event") {
		name := ev.List[1].Atom
		events = append(events, name)
		evDoc := ev.Text("doc")
		if evDoc == "" {
			evDoc = fmt.Sprintf("%s is an event of package %s.", name, pkg.Name)
		}
		st := L(Sym("go/struct"), Sym(name), L(Sym("doc"), Str(evDoc)))
		st.List = append(st.List, ev.FindAll("field")...)
		decls = append(decls, st)

		params := L(Sym("params"))
		if c.Cfg.ContextFirst {
			params.List = append(params.List, L(Sym("ctx"), Sym("context.Context")))
		}
		params.List = append(params.List, L(Sym("e"), Sym(name)))
		bus.List = append(bus.List,
			L(Sym("method"), Sym("Publish"+name), L(Sym("doc"), Str(fmt.Sprintf("Publish%s delivers e to every %s handler.", name, name))),
				params, L(Sym("returns"), Sym("error"))),
			L(Sym("method"), Sym("On"+name), L(Sym("doc"), Str(fmt.Sprintf("On%s subscribes h to %s and returns a func that unsubscribes it.", name, name))),
				L(Sym("params"), L(Sym("h"), Str(fmt.Sprintf("func(%s%s) error", ctxType, name)))), L(Sym("returns"), Str("func()"))))
	}
	decls = append(decls, bus)

	impl, err := tr.Emit(BusInput{C: c, Pkg: pkg, Impl: tr.Suffix, Events: events,
		CtxType: ctxType, CtxArg: ctxArg, Methods: bus.FindAll("method")})
	if err != nil {
		return nil, err
	}
	assert := L(Sym("go/assert"), Sym("Bus"), Sym(tr.Suffix))

	// A transport that emits complete code (local-bus) goes in the
	// generated file with everything else. One that leaves its methods to
	// the LLM (nats-bus) gets a scaffolded file of its own, kept and
	// reconciled like a store's, with one task per method.
	if tr.Hint == nil {
		gen, err := goFile(c, path.Join(pkg.Dir, "events_gen.go"), "generated", "", append(append(decls, impl...), assert))
		if err != nil {
			return nil, err
		}
		return []*Node{gen}, nil
	}
	gen, err := goFile(c, path.Join(pkg.Dir, "events_gen.go"), "generated", "", append(decls, assert))
	if err != nil {
		return nil, err
	}
	file := path.Join(pkg.Dir, snake(tr.Suffix)+".go")
	stubs, tasks := stubsAndTasks(pkg, tr.Suffix, "Bus", file, bus.FindAll("method"),
		func(meth *Node) string {
			name := meth.List[1].Atom
			for _, ev := range events {
				if strings.HasSuffix(name, ev) {
					return tr.Hint(strings.TrimSuffix(name, ev), ev)
				}
			}
			return ""
		},
		[]string{path.Join(pkg.Dir, pkg.Name+"_gen.go")}, nil)
	implFile, err := goFile(c, file, "keep", "", append(impl, stubs...))
	if err != nil {
		return nil, err
	}
	return append([]*Node{gen, implFile}, tasks...), nil
}

// emitLocalBus is the in-process transport: one handler list per event,
// delivered synchronously in the publisher's goroutine.
func emitLocalBus(in BusInput) ([]*Node, error) {
	ctxParam := ""
	if in.CtxType != "" {
		ctxParam = "ctx context.Context, "
	}
	local := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str("LocalBus is an in-process Bus. Publish calls every handler in subscription\n"+
		"order, in the publisher's goroutine, and returns their errors joined.\n"+
		"Handlers may subscribe and unsubscribe at any time, even from a handler.")))
	var methods []*Node
	for _, name := range in.Events {
		field := lowerFirstWord(name) + "Handlers"
		handler := fmt.Sprintf("func(%s%s) error", in.CtxType, name)
		params := L(Sym("params"))
		if in.CtxType != "" {
			params.List = append(params.List, L(Sym("ctx"), Sym("context.Context")))
		}
		params.List = append(params.List, L(Sym("e"), Sym(name)))
		local.List = append(local.List, L(Sym("field"), Sym(field), Str("subscribers["+name+"]")))
		methods = append(methods,
			L(Sym("go/func"), Sym("Publish"+name), L(Sym("recv"), Sym("b"), Sym("*"+in.Impl)), params,
				L(Sym("returns"), Sym("error")), L(Sym("body"), Str(fmt.Sprintf("return b.%s.publish(%se)", field, in.CtxArg)))),
			L(Sym("go/func"), Sym("On"+name), L(Sym("recv"), Sym("b"), Sym("*"+in.Impl)), L(Sym("params"), L(Sym("h"), Str(handler))),
				L(Sym("returns"), Str("func()")), L(Sym("body"), Str(fmt.Sprintf("return b.%s.add(h)", field)))))
	}
	ctor := L(Sym("go/func"), Sym("New"+in.Impl), L(Sym("doc"), Str(fmt.Sprintf("New%s returns an empty %s.", in.Impl, in.Impl))),
		L(Sym("params")), L(Sym("returns"), Sym("*"+in.Impl)), L(Sym("body"), Str("return &"+in.Impl+"{}")))
	out := append([]*Node{local, ctor}, methods...)
	return append(out, L(Sym("go/raw"), Str(subscribersHelper(in.CtxType, ctxParam, in.CtxArg)))), nil
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
