(tile hmac-signature
  (offers auth)
  (doc "signed requests: an HMAC over a timestamp and the body, with a key per caller")
  (alias hmac)
  (type-name HMACAuthenticator)
  (topics hmac signatures)

  (satisfies (revocable)
    "the caller's key is looked up on every request, so removing it takes effect on the next one")
  (illegal-when (third-party-idp)
    "the keys are yours to issue and keep, not an identity provider's")
  (illegal-when (expiring)
    "a key works until someone removes it; only a single signed request ages out, after five minutes")
  (unverified (offline-verify)
    "the check is arithmetic on the request, and where the key comes from is the caller's lookup, which may be a call")

  (cost
    (llm-work 4 (source derived "read the body without consuming it, sign the right bytes, compare in constant time, and bound the clock"))
    (maintenance 3 (source derived "a key per caller to issue and rotate, and clients that must sign exactly the same bytes"))
    (dependency 0 (source derived "the standard library"))
    (runtime 1 (source derived "one hash per request"))
    (operations 1 (source derived "nothing to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} accepts requests signed with a key the caller shares with this\nservice: an HMAC-SHA256 over the request's time and body.")
  (holds (secret "func({ctx-type}string) ([]byte, *Principal, error)"))
  (about-new
    "New{impl} finds a caller's key with secret, which is given a key id and\nreturns the key and whose it is, or a nil principal for an id it does not know.")
  (new (params (secret "func({ctx-type}string) ([]byte, *Principal, error)"))
    "return &{impl}{secret: secret}")

  (hint authenticate
    "Read the key id from the X-Key-Id header, the time from X-Timestamp (Unix seconds) and the signature from X-Signature (hex). Any one missing or malformed is ErrUnauthenticated. Refuse a time more than five minutes from now in either direction: that bounds how long a captured request can be replayed. Look the key up with s.secret; a nil principal is ErrUnauthenticated. Read the body with io.ReadAll and put it back with r.Body = io.NopCloser(bytes.NewReader(body)), so the handler can still read it. Compute HMAC-SHA256 with the key over the timestamp exactly as sent, a newline, and the body, and compare it to the decoded signature with hmac.Equal, never ==. On a mismatch, ErrUnauthenticated. Otherwise return the principal."))
