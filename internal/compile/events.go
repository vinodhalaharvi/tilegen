package compile

import (
	"fmt"
	goversion "go/version"
	"path"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/sexp"
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
	RegisterPackageTile(&PackageTile{
		Form: "events",
		Pass: Select,
		Rule: Rule{Name: "events", Pattern: sexp.Pat("(events ?items...)"), Then: selectEventsAndOutbox,
			Produces: "event-bus", Doc: "event structs, a typed Bus interface, and an in-process LocalBus",
			Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 0}, {Dim: "dependency", Value: 0}}},
		Validate: validateEvents,
	})
}

func validateEvents(v *Validator, n *sexp.Node, types map[string]bool) {
	if v.goVersion != "" && goversion.Compare("go"+v.goVersion, "go1.20") < 0 {
		v.Bad(n, "(events ...) needs (go 1.20) or later (generics and errors.Join); the project has (go %s)", v.goVersion)
	}
	for _, name := range []string{"Bus", "LocalBus"} {
		if types[name] {
			v.Bad(n, "(events ...) generates %s, but the package already declares it (one events form per package)", name)
		}
		types[name] = true
	}
	count := 0
	for _, it := range n.Args() {
		if it.IsList && len(it.List) == 1 && it.Head() == "outbox" {
			v.Bad(it, "(outbox Entity) names the entity saved together with its events, as in (outbox Order)")
			continue
		}
		if it.IsList && len(it.List) == 1 {
			validateRequirements("event-bus", []string{it.Head()}, it, v)
			continue
		}
		switch it.Head() {
		case "doc":
			v.Shape(it, "(doc ?text)")
		case "event":
			count++
			v.structLike(it, types) // an event is a struct: name, fields, doc
		case "outbox":
			validateOutbox(v, it, types)
		case "inbox":
			validateInbox(v, n, it, types)
		default:
			v.Bad(it, "events contain (event Name (field ...)...), (doc ...) and requirements like (durable), got %s%s",
				Short(it), DidYouMean(it.Head(), append([]string{"event", "outbox", "inbox", "doc"}, Capabilities["event-bus"].Requirements...)))
		}
	}
	if count == 0 {
		v.Bad(n, "(events ...) needs at least one (event ...)")
	}
}

// BusTransport is what an event-bus offer contributes: the implementation
// type's name and the code that emits it. The event structs and the Bus
// interface are the same whatever the transport, so the tile makes those.
type BusTransport struct {
	Suffix string    // the implementation type: LocalBus, NatsBus
	Topics []string  // GitHub topics a project using it gets
	Import [2]string // qualifier and import path its code needs, if any
	Emit   func(in BusInput) ([]*sexp.Node, error)

	// Hints, when the transport leaves its methods as holes. Empty means
	// the transport emits complete code, as local-bus does.
	Hint func(method, event string) string
}

// BusInput is what a transport needs to emit its implementation.
type BusInput struct {
	C       *Ctx
	Pkg     *PkgScope
	Impl    string       // the type name to declare
	Events  []string     // event type names, in spec order
	CtxType string       // "context.Context, " or ""
	CtxArg  string       // "ctx, " or ""
	Methods []*sexp.Node // the Bus interface's methods, for reference
}

// selectEvents makes the parts every transport shares: one struct per
// event, and the Bus interface. The transport chosen for this package's
// event-bus need supplies the implementation.
func selectEvents(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
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
	bus := sexp.L(sexp.Sym("go/interface"), sexp.Sym("Bus"), sexp.L(sexp.Sym("doc"), sexp.Str(doc)))
	var decls []*sexp.Node
	var events []string
	for _, ev := range n.FindAll("event") {
		name := ev.List[1].Atom
		events = append(events, name)
		evDoc := ev.Text("doc")
		if evDoc == "" {
			evDoc = fmt.Sprintf("%s is an event of package %s.", name, pkg.Name)
		}
		st := sexp.L(sexp.Sym("go/struct"), sexp.Sym(name), sexp.L(sexp.Sym("doc"), sexp.Str(evDoc)))
		st.List = append(st.List, ev.FindAll("field")...)
		decls = append(decls, st)

		params := sexp.L(sexp.Sym("params"))
		if c.Cfg.ContextFirst {
			params.List = append(params.List, sexp.L(sexp.Sym("ctx"), sexp.Sym("context.Context")))
		}
		params.List = append(params.List, sexp.L(sexp.Sym("e"), sexp.Sym(name)))
		bus.List = append(bus.List,
			sexp.L(sexp.Sym("method"), sexp.Sym("Publish"+name), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("Publish%s delivers e to every %s handler.", name, name))),
				params, sexp.L(sexp.Sym("returns"), sexp.Sym("error"))),
			sexp.L(sexp.Sym("method"), sexp.Sym("On"+name), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("On%s subscribes h to %s and returns a func that unsubscribes it.", name, name))),
				sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("h"), sexp.Str(fmt.Sprintf("func(%s%s) error", ctxType, name)))), sexp.L(sexp.Sym("returns"), sexp.Str("func()"))))
	}
	decls = append(decls, bus)

	impl, err := tr.Emit(BusInput{C: c, Pkg: pkg, Impl: tr.Suffix, Events: events,
		CtxType: ctxType, CtxArg: ctxArg, Methods: bus.FindAll("method")})
	if err != nil {
		return nil, err
	}
	assert := sexp.L(sexp.Sym("go/assert"), sexp.Sym("Bus"), sexp.Sym(tr.Suffix))

	// A transport that emits complete code (local-bus) goes in the
	// generated file with everything else. One that leaves its methods to
	// the LLM (nats-core, nats-jetstream) gets a scaffolded file of its own, kept and
	// reconciled like a store's, with one task per method.
	if tr.Hint == nil {
		gen, err := GoFile(c, path.Join(pkg.Dir, "events_gen.go"), "generated", "", append(append(decls, impl...), assert))
		if err != nil {
			return nil, err
		}
		return []*sexp.Node{gen}, nil
	}
	gen, err := GoFile(c, path.Join(pkg.Dir, "events_gen.go"), "generated", "", append(decls, assert))
	if err != nil {
		return nil, err
	}
	file := path.Join(pkg.Dir, Snake(tr.Suffix)+".go")
	stubs, tasks := StubsAndTasks(pkg, tr.Suffix, "Bus", file, bus.FindAll("method"),
		func(meth *sexp.Node) string {
			name := meth.List[1].Atom
			for _, ev := range events {
				if strings.HasSuffix(name, ev) {
					return tr.Hint(strings.TrimSuffix(name, ev), ev)
				}
			}
			return ""
		},
		[]string{path.Join(pkg.Dir, pkg.Name+"_gen.go")}, nil)
	implFile, err := GoFile(c, file, "keep", "", append(impl, stubs...))
	if err != nil {
		return nil, err
	}
	return append([]*sexp.Node{gen, implFile}, tasks...), nil
}
