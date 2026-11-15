(tile oidc
  (offers auth)
  (doc "an identity provider issues tokens; this verifies them against its keys")
  (type-name OIDCAuthenticator)
  (import oidc github.com/coreos/go-oidc/v3/oidc)
  (topics oidc)

  (satisfies (revocable)
    "the provider stops honouring a session, and short-lived tokens stop being reissued")
  (satisfies (third-party-idp)
    "this is the case it exists for: tokens are issued elsewhere and verified here")
  (satisfies (offline-verify)
    "verification uses cached public keys, with no call per request")
  (satisfies (expiring)
    "the exp claim is inside the signed payload")

  (cost
    (llm-work 4 (source derived "verify with the provider's verifier, then map claims to a principal"))
    (maintenance 2)
    (dependency 2)
    (runtime 1 (source derived "keys are cached; a refresh is occasional"))
    (operations 5 (source derived "an identity provider to run or pay for, with clients, scopes and redirect URIs to keep right")))

  (about
    "{impl} verifies ID tokens issued by an identity provider, against the\nkeys that provider publishes. Nothing here issues a token.")
  (holds (verifier *oidc.IDTokenVerifier))
  (about-new
    "New{impl} verifies tokens with the given verifier, which a caller builds\nfrom the provider's discovery document.")
  (new (params (verifier *oidc.IDTokenVerifier))
    "return &{impl}{verifier: verifier}")

  (hint authenticate
    "Take the bearer token from the Authorization header and verify it with s.verifier. Return a Principal whose Subject is the verified token's Subject, and whose Scopes come from the scope claim. Keep two failures apart: a missing or unverifiable token is ErrUnauthenticated, but a failure to reach the provider's keys is your own error, returned as it is, so Protect answers 500 rather than telling a caller with a perfectly good token that it was rejected."))
