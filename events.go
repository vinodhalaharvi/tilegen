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

func init() {
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
		switch it.Head() {
		case "doc":
			v.shape(it, "(doc ?text)")
		case "event":
			count++
			v.structLike(it, types) // an event is a struct: name, fields, doc
		default:
			v.bad(it, "events contain (event Name (field ...)...) and (doc ...), got %s%s", short(it), didYouMean(it.Head(), []string{"event", "doc"}))
		}
	}
	if count == 0 {
		v.bad(n, "(events ...) needs at least one (event ...)")
	}
}

func selectEvents(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	ctxParam, ctxType, ctxArg := "", "", ""
	if c.Cfg.ContextFirst {
		ctxParam, ctxType, ctxArg = "ctx context.Context, ", "context.Context, ", "ctx, "
	}
	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("Bus publishes the events of package %s and delivers them to handlers.", pkg.Name)
	}
	bus := L(Sym("go/interface"), Sym("Bus"), L(Sym("doc"), Str(doc)))
	local := L(Sym("go/struct"), Sym("LocalBus"), L(Sym("doc"), Str("LocalBus is an in-process Bus. Publish calls every handler in subscription\n"+
		"order, in the publisher's goroutine, and returns their errors joined.\n"+
		"Handlers may subscribe and unsubscribe at any time, even from a handler.")))
	var decls, methods []*Node
	for _, ev := range n.FindAll("event") {
		name := ev.List[1].Atom
		evDoc := ev.Text("doc")
		if evDoc == "" {
			evDoc = fmt.Sprintf("%s is an event of package %s.", name, pkg.Name)
		}
		st := L(Sym("go/struct"), Sym(name), L(Sym("doc"), Str(evDoc)))
		st.List = append(st.List, ev.FindAll("field")...)
		decls = append(decls, st)

		handler := fmt.Sprintf("func(%s%s) error", ctxType, name)
		field := lowerFirstWord(name) + "Handlers"
		params := L(Sym("params"))
		if c.Cfg.ContextFirst {
			params.List = append(params.List, L(Sym("ctx"), Sym("context.Context")))
		}
		params.List = append(params.List, L(Sym("e"), Sym(name)))
		bus.List = append(bus.List,
			L(Sym("method"), Sym("Publish"+name), L(Sym("doc"), Str(fmt.Sprintf("Publish%s delivers e to every %s handler.", name, name))),
				params, L(Sym("returns"), Sym("error"))),
			L(Sym("method"), Sym("On"+name), L(Sym("doc"), Str(fmt.Sprintf("On%s subscribes h to %s and returns a func that unsubscribes it.", name, name))),
				L(Sym("params"), L(Sym("h"), Str(handler))), L(Sym("returns"), Str("func()"))))
		local.List = append(local.List, L(Sym("field"), Sym(field), Str("subscribers["+name+"]")))
		methods = append(methods,
			L(Sym("go/func"), Sym("Publish"+name), L(Sym("recv"), Sym("b"), Sym("*LocalBus")), params,
				L(Sym("returns"), Sym("error")), L(Sym("body"), Str(fmt.Sprintf("return b.%s.publish(%se)", field, ctxArg)))),
			L(Sym("go/func"), Sym("On"+name), L(Sym("recv"), Sym("b"), Sym("*LocalBus")), L(Sym("params"), L(Sym("h"), Str(handler))),
				L(Sym("returns"), Str("func()")), L(Sym("body"), Str(fmt.Sprintf("return b.%s.add(h)", field)))))
	}
	ctor := L(Sym("go/func"), Sym("NewLocalBus"), L(Sym("doc"), Str("NewLocalBus returns an empty LocalBus.")),
		L(Sym("params")), L(Sym("returns"), Sym("*LocalBus")), L(Sym("body"), Str("return &LocalBus{}")))

	decls = append(decls, bus, local, ctor)
	decls = append(decls, methods...)
	decls = append(decls, L(Sym("go/raw"), Str(subscribersHelper(ctxType, ctxParam, ctxArg))),
		L(Sym("go/assert"), Sym("Bus"), Sym("LocalBus")))
	f, err := goFile(c, path.Join(pkg.Dir, "events_gen.go"), "generated", "", decls)
	if err != nil {
		return nil, err
	}
	return []*Node{f}, nil
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
