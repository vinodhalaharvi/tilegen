(tile gorilla-sessions
  (offers auth)
  (doc "a session through gorilla/sessions, in whichever store the caller chose")
  (alias gorilla)
  (type-name GorillaSessionAuthenticator)
  (import sessions github.com/gorilla/sessions)
  (topics sessions gorilla cookies)

  (satisfies (expiring)
    "a session ends with the store's MaxAge, thirty days unless changed")
  (illegal-when (third-party-idp)
    "the session is one this service started, after a login of its own")
  (unverified (revocable)
    "it depends on the store: a cookie store keeps the session in the browser, where nothing can end it early, and a server-side store can delete it")
  (unverified (offline-verify)
    "it depends on the store: a cookie store verifies a signature here, and a server-side store is read on every request")

  (cost
    (llm-work 2 (source derived "read the subject the session carries"))
    (maintenance 4 (source derived "keys to keep secret and rotate, a store to choose, and a login that puts the subject there"))
    (dependency 1 (source derived "gorilla/sessions and securecookie"))
    (runtime 1 (source derived "a signature check per request, or a store read"))
    (operations 1 (source derived "nothing to run with the cookie store"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} reads the caller from a gorilla/sessions session. Signing in, and\nputting the subject in the session, happen elsewhere.")
  (holds (store sessions.Store) (name string))
  (about-new
    "New{impl} reads the session called name from store. The login that\nstarts a session puts the caller's subject under \"sub\".")
  (new (params (store sessions.Store) (name string))
    "return &{impl}{store: store, name: name}")

  (hint authenticate
    "Call sess, err := s.store.Get(r, s.name). Get returns a new, empty session together with an error when the cookie cannot be decoded, so an error is ErrUnauthenticated, as is a session whose IsNew is true. Read sub, ok := sess.Values[\"sub\"].(string); when it is missing or empty, return ErrUnauthenticated. Otherwise return a Principal whose Subject is sub."))
