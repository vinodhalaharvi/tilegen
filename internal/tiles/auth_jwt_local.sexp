(tile jwt-local
  (offers auth)
  (doc "a token this service signs and verifies itself, with no lookup")
  (alias jwt)
  (type-name JWTAuthenticator)
  (import jwt github.com/golang-jwt/jwt/v5)
  (topics jwt)

  (satisfies (offline-verify)
    "the token carries its claims and its signature; verifying it touches nothing else")
  (satisfies (expiring)
    "the exp claim is inside the signed payload, so it cannot be altered")
  (illegal-when (revocable)
    "a signed token is valid until it expires; revoking one means a lookup on every request, which is the cost this scheme exists to avoid")
  (illegal-when (third-party-idp)
    "this issues its own tokens; it does not verify anyone else's")

  (cost
    (llm-work 3 (source derived "parse a bearer header, verify a signature, check iss and exp"))
    (maintenance 2 (source derived "the signing key has to be rotated, and rotation is a second key for a while"))
    (dependency 1)
    (runtime 0 (source derived "one HMAC per request, no I/O"))
    (operations 1 (source derived "a secret to distribute, nothing to run")))

  (about
    "{impl} verifies bearer tokens this service signed itself. A token is\ntrusted until it expires: there is no lookup, and so no way to take one back.")
  (holds (key "[]byte") (issuer string))
  (about-new
    "New{impl} verifies tokens signed with key and issued by issuer.")
  (new (params (key "[]byte") (issuer string))
    "return &{impl}{key: key, issuer: issuer}")

  (hint authenticate
    "Take the bearer token from the Authorization header, verify its signature with s.key, and check that the issuer is s.issuer and the expiry has not passed. Return a Principal whose Subject is the sub claim and whose Scopes are the scope claim split on spaces. A missing, malformed, unsigned, wrongly-signed, expired or wrongly-issued token is all ErrUnauthenticated: the client learns only that it failed. Reject a token whose algorithm is not the one you sign with, since accepting whatever the token claims is how this scheme is broken."))
