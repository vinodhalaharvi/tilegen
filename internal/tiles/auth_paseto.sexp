(tile paseto
  (offers auth)
  (doc "PASETO v4 public tokens, verified against the issuer's Ed25519 public key")
  (type-name PasetoAuthenticator)
  (import paseto aidanwoods.dev/go-paseto)
  (topics paseto tokens)

  (satisfies (offline-verify)
    "a signature check against a public key held here, with no call per request")
  (satisfies (expiring)
    "a token with no expiry, or one that has passed, is refused")
  (illegal-when (revocable)
    "a token is trusted until it expires: nothing is asked of the issuer on a request")
  (illegal-when (third-party-idp)
    "identity providers issue JWTs; a PASETO comes from a service of your own that holds the signing key")

  (cost
    (llm-work 3 (source derived "a parser with the right rules, one parse, then map claims to a principal"))
    (maintenance 2 (source derived "the issuer's public key to distribute and replace when it rotates"))
    (dependency 2 (source derived "go-paseto and its cryptography modules"))
    (runtime 1 (source derived "a signature check per request"))
    (operations 1 (source derived "nothing to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} verifies PASETO v4 public tokens against the public half of the\nkey that signed them. Nothing here issues a token.")
  (holds (key paseto.V4AsymmetricPublicKey) (issuer string) (audience string))
  (about-new
    "New{impl} verifies tokens with key, which the caller loads, for example\nwith paseto.NewV4AsymmetricPublicKeyFromHex.")
  (new (params (key paseto.V4AsymmetricPublicKey) (issuer string) (audience string))
    "return &{impl}{key: key, issuer: issuer, audience: audience}")

  (hint authenticate
    "Take the bearer token from the Authorization header; without one, return ErrUnauthenticated. Build parser := paseto.NewParser(), which already refuses a token past its expiry, and call parser.AddRule(paseto.IssuedBy(s.issuer), paseto.ForAudience(s.audience)). Call tok, err := parser.ParseV4Public(s.key, the token, nil); it verifies the signature and then every rule, and any error is ErrUnauthenticated. Return a Principal whose Subject is tok.GetSubject(); for scopes, read tok.GetString(\"scope\") and split it on spaces, treating a missing claim as none."))
