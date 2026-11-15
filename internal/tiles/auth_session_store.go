package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "session-store",
		Capability: "auth",
		Doc:        "an opaque session id in a cookie, looked up in a store on every request",
		IllegalFor: map[string]string{
			"offline-verify": "the cookie carries an id and nothing else; there is nothing in it to verify without the store",
			"third-party-idp": "a session is issued here, after whatever login you run; it says nothing about who " +
				"vouched for the user",
		},
		Satisfies: map[string]string{
			"revocable": "the session is read on every request, so deleting it takes effect on the next one",
			"expiring":  "the stored session carries an expiry, checked on every read",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 3, Source: "derived", Note: "read a cookie, load the session, check it has not expired"},
			{Dim: "maintenance", Value: 2, Source: "derived", Note: "expired sessions have to be swept, or the store grows forever"},
			{Dim: "dependency", Value: 0, Source: "derived", Note: "net/http cookies and a store the project already has"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "one store read per request"},
			{Dim: "operations", Value: 1, Source: "derived", Note: "nothing new to run: the sessions live in a store already chosen"},
		},
		Impl: &compile.AuthScheme{
			Suffix: "SessionAuthenticator",
			Emit:   emitSessionAuth,
			Hint:   sessionAuthHint,
		},
	})
	compile.RegisterAlias("session-store", "session")
}

func emitSessionAuth(in compile.AuthInput) ([]*sexp.Node, error) {
	// The sessions live in a store, and the store comes from the spec:
	// this scheme reads what is there rather than inventing an entity.
	if in.Pkg.Ifaces["SessionStore"] == nil {
		return nil, fmt.Errorf("the session-store tile keeps sessions in a SessionStore, and package %s has none; add\n"+
			"  (entity Session (field ID string) (field Subject string) (field ExpiresAt time.Time)\n"+
			"    (store get save delete (durable)))\n"+
			"or choose another tile for auth", in.Pkg.Name)
	}
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s resolves an opaque session id from a cookie against the store.\nEvery request costs a read, which is what makes a session revocable.", in.Impl))),
		sexp.L(sexp.Sym("field"), sexp.Sym("sessions"), sexp.Sym("SessionStore")),
		sexp.L(sexp.Sym("field"), sexp.Sym("cookie"), sexp.Sym("string")))
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s reads the session id from the named cookie.", in.Impl))),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("sessions"), sexp.Sym("SessionStore")), sexp.L(sexp.Sym("cookie"), sexp.Sym("string"))),
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &%s{sessions: sessions, cookie: cookie}", in.Impl))))
	return []*sexp.Node{decl, ctor}, nil
}

func sessionAuthHint(in compile.AuthInput) string {
	return "Read the session id from the cookie named s.cookie and load it with s.sessions.Get. " +
		"Return a Principal built from the session. A missing cookie, an unknown id and an expired " +
		"session are all ErrUnauthenticated, and they must be indistinguishable to the client: " +
		"telling a caller that an id exists but has expired tells them the id exists. Do not delete " +
		"the expired session here; Authenticate is a read, and sweeping belongs elsewhere."
}
