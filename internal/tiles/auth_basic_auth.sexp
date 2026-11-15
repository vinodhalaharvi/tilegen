(tile basic-auth
  (offers auth)
  (doc "HTTP Basic: a user name and password on every request, checked by a function you supply")
  (alias basic)
  (type-name BasicAuthenticator)
  (topics basic-auth)

  (satisfies (revocable)
    "the credentials are checked on every request, so changing or removing them takes effect on the next one")
  (illegal-when (third-party-idp)
    "the user names and passwords are yours to keep, not an identity provider's")
  (illegal-when (offline-verify)
    "a password proves nothing by itself: every request is a check against what is stored")
  (illegal-when (expiring)
    "a password works until someone changes it; nothing in the credential says when to stop trusting it")

  (cost
    (llm-work 2 (source derived "read the header and call the check"))
    (maintenance 3 (source derived "passwords to store hashed, compare in constant time and rotate, and they cross the wire on every request"))
    (dependency 0 (source derived "the standard library"))
    (runtime 2 (source derived "one check per request, and a password hash is slow on purpose"))
    (operations 1 (source derived "nothing to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} checks the user name and password of HTTP Basic authentication.\nWhere they are kept is the caller's business: this takes a check and asks it.")
  (holds (check "func({ctx-type}string, string) (*Principal, error)"))
  (about-new
    "New{impl} checks credentials with check, which returns nil for a user\nname and password it does not accept.")
  (new (params (check "func({ctx-type}string, string) (*Principal, error)"))
    "return &{impl}{check: check}")

  (hint authenticate
    "Take the user name and password with r.BasicAuth(). When it reports none, return ErrUnauthenticated. Otherwise pass both to s.check and return the principal it gives back; a nil principal is ErrUnauthenticated too. Do not compare the password here: the check owns that, with a slow hash and a constant-time comparison."))
