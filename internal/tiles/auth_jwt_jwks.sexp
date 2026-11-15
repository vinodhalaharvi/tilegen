(tile jwt-jwks
  (offers auth)
  (doc "bearer tokens signed by someone else, verified against the keys they publish")
  (alias jwks)
  (type-name JWKSAuthenticator)
  (import jwt github.com/golang-jwt/jwt/v5)
  (topics jwt jwks)

  (satisfies (third-party-idp)
    "tokens are issued elsewhere and verified here against the issuer's published keys")
  (satisfies (offline-verify)
    "verification uses keys the caller's keyfunc has fetched and cached, with no call per request")
  (satisfies (expiring)
    "a token with no exp claim, or one that has passed, is refused")
  (illegal-when (revocable)
    "a token is trusted until it expires: nothing is asked of the issuer on a request")

  (cost
    (llm-work 3 (source derived "parse with the right options, then map claims to a principal"))
    (maintenance 2 (source derived "the issuer, audience and accepted algorithms to keep right"))
    (dependency 1 (source derived "golang-jwt only; the key set's fetching is the caller's"))
    (runtime 1 (source derived "a signature check per request"))
    (operations 5 (source derived "an identity provider to run or pay for, with its clients and keys to keep right: the same one an OIDC verifier depends on"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} verifies bearer tokens signed by another party, against the\nkeys that party publishes. Nothing here issues a token or fetches a key.")
  (holds (keyfunc jwt.Keyfunc) (issuer string) (audience string))
  (about-new
    "New{impl} verifies tokens with keyfunc, which a caller builds from the\nissuer's JWKS URL and which picks the key a token names.")
  (new (params (keyfunc jwt.Keyfunc) (issuer string) (audience string))
    "return &{impl}{keyfunc: keyfunc, issuer: issuer, audience: audience}")

  (hint authenticate
    "Take the bearer token from the Authorization header; without one, return ErrUnauthenticated. Parse it with jwt.ParseWithClaims(token, &claims, s.keyfunc, jwt.WithIssuer(s.issuer), jwt.WithAudience(s.audience), jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{\"RS256\", \"ES256\"})), where claims is a struct embedding jwt.RegisteredClaims with a Scope string tagged json:\"scope\". Naming the methods matters: without it a token signed with an algorithm you never meant to accept can pass. Any error is ErrUnauthenticated. Return a Principal whose Subject is claims.Subject and whose Scopes are strings.Fields(claims.Scope)."))
