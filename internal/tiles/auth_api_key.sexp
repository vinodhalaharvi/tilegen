(tile api-key
  (offers auth)
  (doc "a long-lived key per caller, resolved by a lookup you supply")
  (type-name APIKeyAuthenticator)

  (satisfies (revocable)
    "the key is resolved on every request, so removing it takes effect on the next one")
  (illegal-when (third-party-idp)
    "an API key is issued by you, not by an identity provider")
  (illegal-when (offline-verify)
    "a key carries no claims: every request is a lookup")
  (illegal-when (expiring)
    "a key is valid until someone deletes it; nothing in the credential says when to stop trusting it")

  (cost
    (llm-work 2 (source derived "read a header and call the lookup; the store is the caller's"))
    (maintenance 2 (source derived "keys are long-lived, so leaks are found late and rotation is manual"))
    (dependency 0)
    (runtime 2 (source derived "one lookup per request"))
    (operations 1))

  (about
    "{impl} resolves a long-lived key to a caller. Where the keys are kept is\nthe caller's business: this takes a lookup and asks it.")
  (holds (lookup "func({ctx-type}string) (*Principal, error)"))
  (about-new
    "New{impl} resolves keys with lookup, which returns nil for a key it does not know.")
  (new (params (lookup "func({ctx-type}string) (*Principal, error)"))
    "return &{impl}{lookup: lookup}")

  (hint authenticate
    "Take the key from the Authorization header (accept both \"Bearer x\" and a bare key) and pass it to s.lookup. Return the principal it gives back. A missing key, and a key the lookup does not know, are both ErrUnauthenticated. If you ever compare a key to another string yourself, use subtle.ConstantTimeCompare rather than ==, since == returns as soon as two bytes differ and that timing is enough to guess a key one byte at a time."))
