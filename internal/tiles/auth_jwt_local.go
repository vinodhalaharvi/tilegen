package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "jwt-local",
		Capability: "auth",
		Doc:        "a token this service signs and verifies itself, with no lookup",
		IllegalFor: map[string]string{
			"revocable": "a signed token is valid until it expires; revoking one means a lookup on every request, " +
				"which is the cost this scheme exists to avoid",
			"third-party-idp": "this issues its own tokens; it does not verify anyone else's",
		},
		Satisfies: map[string]string{
			"offline-verify": "the token carries its claims and its signature; verifying it touches nothing else",
			"expiring":       "the exp claim is inside the signed payload, so it cannot be altered",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 3, Source: "derived", Note: "parse a bearer header, verify a signature, check iss and exp"},
			{Dim: "maintenance", Value: 2, Source: "derived", Note: "the signing key has to be rotated, and rotation is a second key for a while"},
			{Dim: "dependency", Value: 1},
			{Dim: "runtime", Value: 0, Source: "derived", Note: "one HMAC per request, no I/O"},
			{Dim: "operations", Value: 1, Source: "derived", Note: "a secret to distribute, nothing to run"},
		},
		Impl: &compile.AuthScheme{
			Suffix: "JWTAuthenticator",
			Import: [2]string{"jwt", "github.com/golang-jwt/jwt/v5"},
			Topics: []string{"jwt"},
			Emit:   emitJWTAuth,
			Hint:   jwtAuthHint,
		},
	})
	compile.RegisterAlias("jwt-local", "jwt")
}

func emitJWTAuth(in compile.AuthInput) ([]*sexp.Node, error) {
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s verifies bearer tokens this service signed itself. A token is\ntrusted until it expires: there is no lookup, and so no way to take one back.", in.Impl))),
		sexp.L(sexp.Sym("field"), sexp.Sym("key"), sexp.Str("[]byte")),
		sexp.L(sexp.Sym("field"), sexp.Sym("issuer"), sexp.Sym("string")))
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s verifies tokens signed with key and issued by issuer.", in.Impl))),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("key"), sexp.Str("[]byte")), sexp.L(sexp.Sym("issuer"), sexp.Sym("string"))),
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &%s{key: key, issuer: issuer}", in.Impl))))
	return []*sexp.Node{decl, ctor}, nil
}

func jwtAuthHint(in compile.AuthInput) string {
	return "Take the bearer token from the Authorization header, verify its signature with s.key, " +
		"and check that the issuer is s.issuer and the expiry has not passed. Return a Principal " +
		"whose Subject is the sub claim and whose Scopes are the scope claim split on spaces. " +
		"A missing, malformed, unsigned, wrongly-signed, expired or wrongly-issued token is all " +
		"ErrUnauthenticated: the client learns only that it failed. Reject a token whose algorithm " +
		"is not the one you sign with, since accepting whatever the token claims is how this scheme is broken."
}
