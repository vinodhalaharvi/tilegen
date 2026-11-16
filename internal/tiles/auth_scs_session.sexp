(tile scs-session
  (offers auth)
  (doc "a server-side session through alexedwards/scs, found by the cookie it sets")
  (alias scs)
  (type-name SCSAuthenticator)
  (import scs github.com/alexedwards/scs/v2)
  (topics sessions scs)

  (satisfies (revocable)
    "the session's data lives in the manager's store, and destroying it ends the session on the next request")
  (satisfies (expiring)
    "the manager's Lifetime and IdleTimeout end a session by themselves")
  (illegal-when (third-party-idp)
    "the session is one this service started, after a login of its own")
  (illegal-when (offline-verify)
    "the session is read from the manager's store on every request")

  (cost
    (llm-work 2 (source derived "read the subject the session carries"))
    (maintenance 3 (source derived "a middleware that must wrap every route, cookie settings to get right, and a login that puts the subject there"))
    (dependency 1 (source derived "scs only; a store other than memory is one more module"))
    (runtime 2 (source derived "a store read on every request"))
    (operations 1 (source derived "nothing to run with the in-memory store, which also forgets every session on restart"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} reads the caller from a session kept by scs. Signing in, and\nputting the subject in the session, happen elsewhere.")
  (holds (sessions *scs.SessionManager))
  (about-new
    "New{impl} reads sessions from the given manager, whose LoadAndSave\nmiddleware must wrap every handler this protects. The login that starts a\nsession puts the caller's subject under \"sub\".")
  (new (params (sessions *scs.SessionManager))
    "return &{impl}{sessions: sessions}")

  (hint authenticate
    "The request must already have passed through s.sessions.LoadAndSave, the middleware that loads the session into the request's context; without it GetString panics. Read sub := s.sessions.GetString(r.Context(), \"sub\"). Empty means nobody is signed in: return ErrUnauthenticated. Otherwise return a Principal whose Subject is sub."))
