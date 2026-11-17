(tile branca
  (offers auth)
  (doc "Branca tokens: encrypted and timestamped with a key this service shares with whoever issues them")
  (type-name BrancaAuthenticator)
  (import branca github.com/essentialkaos/branca/v2)
  (topics branca tokens)

  (satisfies (offline-verify)
    "a token is decrypted and authenticated with a key held here, with no call per request")
  (satisfies (expiring)
    "a token carries the time it was made, and one older than the lifetime given here is refused")
  (illegal-when (revocable)
    "a token is trusted until it is too old: nothing is asked of anyone on a request")
  (illegal-when (third-party-idp)
    "the tokens are your own, made by a service that holds the same key")

  (cost
    (llm-work 3 (source derived "decode, check the age, then read the payload"))
    (maintenance 3 (source derived "one 32-byte key that every verifier holds and could mint tokens with, and a payload format of your own"))
    (dependency 2 (source derived "the library and its cipher module"))
    (runtime 1 (source derived "one decryption per request"))
    (operations 1 (source derived "nothing to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} accepts Branca tokens made with the same key. The payload is\nencrypted, so only holders of the key can read or make one.")
  (holds (codec branca.Branca) (ttl uint32))
  (about-new
    "New{impl} decodes with codec, made by the caller with branca.NewBranca\nand a 32-byte key, and refuses a token older than ttl seconds.")
  (new (params (codec branca.Branca) (ttl uint32))
    "return &{impl}{codec: codec, ttl: ttl}")

  (hint authenticate
    "Take the bearer token from the Authorization header; without one, return ErrUnauthenticated. Call tok, err := s.codec.DecodeString(the token); any error means it was not made with this key: ErrUnauthenticated. When tok.IsExpired(s.ttl), return ErrUnauthenticated. Decode tok.Payload() as JSON into a struct with Sub string and Scopes []string, tagged sub and scopes; the payload's format is this service's own, and an empty Sub is ErrUnauthenticated. Return a Principal with that Subject and those Scopes."))
