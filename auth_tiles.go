package main

import "fmt"

// The four ways to decide who a request is from. They differ on four
// questions a spec can ask, and the answers are properties of the scheme
// rather than of any library:
//
//	                revocable  third-party-idp  offline-verify  expiring
//	  jwt-local         no           no              yes           yes
//	  oidc              yes          yes             yes           yes
//	  session-store     yes          no              no            yes
//	  api-key           yes          no              no            no
//
// oidc is legal for everything and expensive to run, which is the shape
// the vocabulary should produce: the most capable answer wins only when
// something is actually asked for.

func init() {
	RegisterOffer(&Offer{
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
		Cost: Cost{
			{Dim: "llm-work", Value: 3, Source: "derived", Note: "parse a bearer header, verify a signature, check iss and exp"},
			{Dim: "maintenance", Value: 2, Source: "derived", Note: "the signing key has to be rotated, and rotation is a second key for a while"},
			{Dim: "dependency", Value: 1},
			{Dim: "runtime", Value: 0, Source: "derived", Note: "one HMAC per request, no I/O"},
			{Dim: "operations", Value: 1, Source: "derived", Note: "a secret to distribute, nothing to run"},
		},
		Impl: &AuthScheme{
			Suffix: "JWTAuthenticator",
			Import: [2]string{"jwt", "github.com/golang-jwt/jwt/v5"},
			Topics: []string{"jwt"},
			Emit:   emitJWTAuth,
			Hint:   jwtAuthHint,
		},
	})
	registerAlias("jwt-local", "jwt")

	RegisterOffer(&Offer{
		Tile:       "oidc",
		Capability: "auth",
		Doc:        "an identity provider issues tokens; this verifies them against its keys",
		Satisfies: map[string]string{
			"revocable":       "the provider stops honouring a session, and short-lived tokens stop being reissued",
			"third-party-idp": "this is the case it exists for: tokens are issued elsewhere and verified here",
			"offline-verify":  "verification uses cached public keys, with no call per request",
			"expiring":        "the exp claim is inside the signed payload",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "verify with the provider's verifier, then map claims to a principal"},
			{Dim: "maintenance", Value: 2},
			{Dim: "dependency", Value: 2},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "keys are cached; a refresh is occasional"},
			{Dim: "operations", Value: 5, Source: "derived", Note: "an identity provider to run or pay for, with clients, scopes and redirect URIs to keep right"},
		},
		Impl: &AuthScheme{
			Suffix: "OIDCAuthenticator",
			Import: [2]string{"oidc", "github.com/coreos/go-oidc/v3/oidc"},
			Topics: []string{"oidc"},
			Emit:   emitOIDCAuth,
			Hint:   oidcAuthHint,
		},
	})
	registerAlias("oidc", "oidc")

	RegisterOffer(&Offer{
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
		Cost: Cost{
			{Dim: "llm-work", Value: 3, Source: "derived", Note: "read a cookie, load the session, check it has not expired"},
			{Dim: "maintenance", Value: 2, Source: "derived", Note: "expired sessions have to be swept, or the store grows forever"},
			{Dim: "dependency", Value: 0, Source: "derived", Note: "net/http cookies and a store the project already has"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "one store read per request"},
			{Dim: "operations", Value: 1, Source: "derived", Note: "nothing new to run: the sessions live in a store already chosen"},
		},
		Impl: &AuthScheme{
			Suffix: "SessionAuthenticator",
			Emit:   emitSessionAuth,
			Hint:   sessionAuthHint,
		},
	})
	registerAlias("session-store", "session")

	RegisterOffer(&Offer{
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
		Cost: Cost{
			{Dim: "llm-work", Value: 2, Source: "derived", Note: "read a header and call the lookup; the store is the caller's"},
			{Dim: "maintenance", Value: 2, Source: "derived", Note: "keys are long-lived, so leaks are found late and rotation is manual"},
			{Dim: "dependency", Value: 0},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "one lookup per request"},
			{Dim: "operations", Value: 1},
		},
		Impl: &AuthScheme{
			Suffix: "APIKeyAuthenticator",
			Emit:   emitAPIKeyAuth,
			Hint:   apiKeyAuthHint,
		},
	})
	registerAlias("api-key", "api-key")
}

func emitJWTAuth(in AuthInput) ([]*Node, error) {
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s verifies bearer tokens this service signed itself. A token is\ntrusted until it expires: there is no lookup, and so no way to take one back.", in.Impl))),
		L(Sym("field"), Sym("key"), Str("[]byte")),
		L(Sym("field"), Sym("issuer"), Sym("string")))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s verifies tokens signed with key and issued by issuer.", in.Impl))),
		L(Sym("params"), L(Sym("key"), Str("[]byte")), L(Sym("issuer"), Sym("string"))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{key: key, issuer: issuer}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

func jwtAuthHint(in AuthInput) string {
	return "Take the bearer token from the Authorization header, verify its signature with s.key, " +
		"and check that the issuer is s.issuer and the expiry has not passed. Return a Principal " +
		"whose Subject is the sub claim and whose Scopes are the scope claim split on spaces. " +
		"A missing, malformed, unsigned, wrongly-signed, expired or wrongly-issued token is all " +
		"ErrUnauthenticated: the client learns only that it failed. Reject a token whose algorithm " +
		"is not the one you sign with, since accepting whatever the token claims is how this scheme is broken."
}

func emitOIDCAuth(in AuthInput) ([]*Node, error) {
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s verifies ID tokens issued by an identity provider, against the\nkeys that provider publishes. Nothing here issues a token.", in.Impl))),
		L(Sym("field"), Sym("verifier"), Sym("*oidc.IDTokenVerifier")))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s verifies tokens with the given verifier, which a caller builds\nfrom the provider's discovery document.", in.Impl))),
		L(Sym("params"), L(Sym("verifier"), Sym("*oidc.IDTokenVerifier"))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{verifier: verifier}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

func oidcAuthHint(in AuthInput) string {
	return "Take the bearer token from the Authorization header and verify it with s.verifier. " +
		"Return a Principal whose Subject is the verified token's Subject, and whose Scopes come " +
		"from the scope claim. Keep two failures apart: a missing or unverifiable token is " +
		"ErrUnauthenticated, but a failure to reach the provider's keys is your own error, returned " +
		"as it is, so Protect answers 500 rather than telling a caller with a perfectly good token that it was rejected."
}

func emitSessionAuth(in AuthInput) ([]*Node, error) {
	// The sessions live in a store, and the store comes from the spec:
	// this scheme reads what is there rather than inventing an entity.
	if in.Pkg.Ifaces["SessionStore"] == nil {
		return nil, fmt.Errorf("the session-store tile keeps sessions in a SessionStore, and package %s has none; add\n"+
			"  (entity Session (field ID string) (field Subject string) (field ExpiresAt time.Time)\n"+
			"    (store get save delete (durable)))\n"+
			"or choose another tile for auth", in.Pkg.Name)
	}
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s resolves an opaque session id from a cookie against the store.\nEvery request costs a read, which is what makes a session revocable.", in.Impl))),
		L(Sym("field"), Sym("sessions"), Sym("SessionStore")),
		L(Sym("field"), Sym("cookie"), Sym("string")))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s reads the session id from the named cookie.", in.Impl))),
		L(Sym("params"), L(Sym("sessions"), Sym("SessionStore")), L(Sym("cookie"), Sym("string"))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{sessions: sessions, cookie: cookie}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

func sessionAuthHint(in AuthInput) string {
	return "Read the session id from the cookie named s.cookie and load it with s.sessions.Get. " +
		"Return a Principal built from the session. A missing cookie, an unknown id and an expired " +
		"session are all ErrUnauthenticated, and they must be indistinguishable to the client: " +
		"telling a caller that an id exists but has expired tells them the id exists. Do not delete " +
		"the expired session here; Authenticate is a read, and sweeping belongs elsewhere."
}

func emitAPIKeyAuth(in AuthInput) ([]*Node, error) {
	lookup := fmt.Sprintf("func(%sstring) (*Principal, error)", in.CtxType)
	decl := L(Sym("go/struct"), Sym(in.Impl), L(Sym("doc"), Str(fmt.Sprintf(
		"%s resolves a long-lived key to a caller. Where the keys are kept is\nthe caller's business: this takes a lookup and asks it.", in.Impl))),
		L(Sym("field"), Sym("lookup"), Str(lookup)))
	ctor := L(Sym("go/func"), Sym("New"+in.Impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s resolves keys with lookup, which returns nil for a key it does not know.", in.Impl))),
		L(Sym("params"), L(Sym("lookup"), Str(lookup))),
		L(Sym("returns"), Sym("*"+in.Impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{lookup: lookup}", in.Impl))))
	return []*Node{decl, ctor}, nil
}

func apiKeyAuthHint(in AuthInput) string {
	return "Take the key from the Authorization header (accept both \"Bearer x\" and a bare key) and " +
		"pass it to s.lookup. Return the principal it gives back. A missing key, and a key the lookup " +
		"does not know, are both ErrUnauthenticated. If you ever compare a key to another string " +
		"yourself, use subtle.ConstantTimeCompare rather than ==, since == returns as soon as two " +
		"bytes differ and that timing is enough to guess a key one byte at a time."
}
