(tile jwt-jwx
  (offers auth)
  (doc "bearer tokens signed by someone else, verified against their key set through lestrrat-go/jwx")
  (alias jwx)
  (type-name JWXAuthenticator)
  (import jwt github.com/lestrrat-go/jwx/v2/jwt)
  (import jwk github.com/lestrrat-go/jwx/v2/jwk)
  (topics jwt jwks jwx)

  (satisfies (third-party-idp)
    "tokens are issued elsewhere and verified here against the issuer's published keys")
  (satisfies (offline-verify)
    "verification uses a key set the caller has fetched and keeps fresh, with no call per request")
  (satisfies (expiring)
    "a token with no exp claim, or one that has passed, is refused")
  (illegal-when (revocable)
    "a token is trusted until it expires: nothing is asked of the issuer on a request")

  (cost
    (llm-work 3 (source derived "one parse with the right options, then map claims to a principal"))
    (maintenance 2 (source derived "the issuer and audience to keep right"))
    (dependency 3 (source derived "jwx and its cryptography and JSON modules"))
    (runtime 1 (source derived "a signature check per request"))
    (operations 5 (source derived "an identity provider to run or pay for, with its clients and keys to keep right"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} verifies bearer tokens signed by another party, against that\nparty's key set. Nothing here issues a token or fetches a key.")
  (holds (keys jwk.Set) (issuer string) (audience string))
  (about-new
    "New{impl} verifies tokens with keys, which a caller fetches from the\nissuer's JWKS URL and refreshes, for example with a jwk.Cache.")
  (new (params (keys jwk.Set) (issuer string) (audience string))
    "return &{impl}{keys: keys, issuer: issuer, audience: audience}")

  (hint authenticate
    "Call tok, err := jwt.ParseRequest(r, jwt.WithKeySet(s.keys), jwt.WithIssuer(s.issuer), jwt.WithAudience(s.audience), jwt.WithRequiredClaim(jwt.ExpirationKey)). ParseRequest takes the bearer token from the Authorization header, verifies its signature with the key in the set that the token names, and validates its claims. Any error is ErrUnauthenticated. Return a Principal whose Subject is tok.Subject(); for scopes, read the scope claim with tok.Get(\"scope\") and split the string on spaces."))
