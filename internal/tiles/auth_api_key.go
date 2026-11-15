package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "api-key",
		Capability: "auth",
		Doc:        "a long-lived key per caller, resolved by a lookup you supply",
		IllegalFor: map[string]string{
			"expiring":        "a key is valid until someone deletes it; nothing in the credential says when to stop trusting it",
			"offline-verify":  "a key carries no claims: every request is a lookup",
			"third-party-idp": "an API key is issued by you, not by an identity provider",
		},
		Satisfies: map[string]string{
			"revocable": "the key is resolved on every request, so removing it takes effect on the next one",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 2, Source: "derived", Note: "read a header and call the lookup; the store is the caller's"},
			{Dim: "maintenance", Value: 2, Source: "derived", Note: "keys are long-lived, so leaks are found late and rotation is manual"},
			{Dim: "dependency", Value: 0},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "one lookup per request"},
			{Dim: "operations", Value: 1},
		},
		Impl: &compile.AuthScheme{
			Suffix: "APIKeyAuthenticator",
			Emit:   emitAPIKeyAuth,
			Hint:   apiKeyAuthHint,
		},
	})
	compile.RegisterAlias("api-key", "api-key")
}

func emitAPIKeyAuth(in compile.AuthInput) ([]*sexp.Node, error) {
	lookup := fmt.Sprintf("func(%sstring) (*Principal, error)", in.CtxType)
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s resolves a long-lived key to a caller. Where the keys are kept is\nthe caller's business: this takes a lookup and asks it.", in.Impl))),
		sexp.L(sexp.Sym("field"), sexp.Sym("lookup"), sexp.Str(lookup)))
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s resolves keys with lookup, which returns nil for a key it does not know.", in.Impl))),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("lookup"), sexp.Str(lookup))),
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &%s{lookup: lookup}", in.Impl))))
	return []*sexp.Node{decl, ctor}, nil
}

func apiKeyAuthHint(in compile.AuthInput) string {
	return "Take the key from the Authorization header (accept both \"Bearer x\" and a bare key) and " +
		"pass it to s.lookup. Return the principal it gives back. A missing key, and a key the lookup " +
		"does not know, are both ErrUnauthenticated. If you ever compare a key to another string " +
		"yourself, use subtle.ConstantTimeCompare rather than ==, since == returns as soon as two " +
		"bytes differ and that timing is enough to guess a key one byte at a time."
}
