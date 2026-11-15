(tile cookie-session
  (offers auth)
  (doc "a session in a signed cookie, read through gorilla/sessions")
  (alias cookie)
  (type-name CookieSessionAuthenticator)
  (import sessions github.com/gorilla/sessions)
  (topics sessions cookies)

  (satisfies (expiring)
    "the cookie carries a signed timestamp, checked against the store's maximum age on every read")
  (illegal-when (third-party-idp)
    "the session is one this service started, after a login of its own")
  (unverified (revocable)
    "with a CookieStore the session lives in the signed cookie and cannot be withdrawn before it expires; with a store kept on the server it can, and which Store this is given is the caller's choice")
  (unverified (offline-verify)
    "a CookieStore verifies the cookie's signature with no call; a store kept on the server reads it on every request, and which Store this is given is the caller's choice")

  (cost
    (llm-work 2 (source derived "read the session and take the subject from it"))
    (maintenance 3 (source derived "keys to keep secret and rotate, cookie flags to set, and a login that puts the subject there"))
    (dependency 1 (source derived "gorilla/sessions and securecookie"))
    (runtime 1 (source derived "a signature check per request"))
    (operations 1 (source derived "nothing to run with a cookie store"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} reads the caller from a session kept by gorilla/sessions. Signing\nin, and putting the subject in the session, happen elsewhere.")
  (holds (store sessions.Store) (name string))
  (about-new
    "New{impl} reads the session called name from store. The login that\nstarts a session sets its \"sub\" value to the caller's subject.")
  (new (params (store sessions.Store) (name string))
    "return &{impl}{store: store, name: name}")

  (hint authenticate
    "Call session, err := s.store.Get(r, s.name). An error means the cookie could not be decoded or its signature did not verify: return ErrUnauthenticated. Read sub, _ := session.Values[\"sub\"].(string). When session.IsNew or sub is empty there is no session: return ErrUnauthenticated. Otherwise return a Principal whose Subject is sub."))
