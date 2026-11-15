package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "oidc",
		Capability: "auth",
		Doc:        "an identity provider issues tokens; this verifies them against its keys",
		Satisfies: map[string]string{
			"revocable":       "the provider stops honouring a session, and short-lived tokens stop being reissued",
			"third-party-idp": "this is the case it exists for: tokens are issued elsewhere and verified here",
			"offline-verify":  "verification uses cached public keys, with no call per request",
			"expiring":        "the exp claim is inside the signed payload",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "verify with the provider's verifier, then map claims to a principal"},
			{Dim: "maintenance", Value: 2},
			{Dim: "dependency", Value: 2},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "keys are cached; a refresh is occasional"},
			{Dim: "operations", Value: 5, Source: "derived", Note: "an identity provider to run or pay for, with clients, scopes and redirect URIs to keep right"},
		},
		Impl: &compile.AuthScheme{
			Suffix: "OIDCAuthenticator",
			Import: [2]string{"oidc", "github.com/coreos/go-oidc/v3/oidc"},
			Topics: []string{"oidc"},
			Emit:   emitOIDCAuth,
			Hint:   oidcAuthHint,
		},
	})
	compile.RegisterAlias("oidc", "oidc")
}

func emitOIDCAuth(in compile.AuthInput) ([]*sexp.Node, error) {
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s verifies ID tokens issued by an identity provider, against the\nkeys that provider publishes. Nothing here issues a token.", in.Impl))),
		sexp.L(sexp.Sym("field"), sexp.Sym("verifier"), sexp.Sym("*oidc.IDTokenVerifier")))
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s verifies tokens with the given verifier, which a caller builds\nfrom the provider's discovery document.", in.Impl))),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("verifier"), sexp.Sym("*oidc.IDTokenVerifier"))),
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &%s{verifier: verifier}", in.Impl))))
	return []*sexp.Node{decl, ctor}, nil
}

func oidcAuthHint(in compile.AuthInput) string {
	return "Take the bearer token from the Authorization header and verify it with s.verifier. " +
		"Return a Principal whose Subject is the verified token's Subject, and whose Scopes come " +
		"from the scope claim. Keep two failures apart: a missing or unverifiable token is " +
		"ErrUnauthenticated, but a failure to reach the provider's keys is your own error, returned " +
		"as it is, so Protect answers 500 rather than telling a caller with a perfectly good token that it was rejected."
}
