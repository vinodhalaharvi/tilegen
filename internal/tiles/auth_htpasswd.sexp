(tile htpasswd
  (offers auth)
  (doc "HTTP basic auth against an Apache htpasswd file")
  (type-name HtpasswdAuthenticator)
  (import htpasswd github.com/tg123/go-htpasswd)
  (topics basic-auth htpasswd)

  (satisfies (offline-verify)
    "the file is held in memory, and a password is checked against it with no call")
  (illegal-when (third-party-idp)
    "the users are lines in a file of your own")
  (illegal-when (expiring)
    "a line works until someone removes it")
  (unverified (revocable)
    "a removed user is refused once the file is read again, and reading it again is the caller's to trigger, with Reload")

  (cost
    (llm-work 2 (source derived "take the credentials from the request and ask the file"))
    (maintenance 3 (source derived "a file to edit, distribute to every instance and reload"))
    (dependency 1 (source derived "go-htpasswd and the hash modules it supports"))
    (runtime 2 (source derived "a password hash on every request, slow by design when it is bcrypt"))
    (operations 1 (source derived "nothing to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} checks HTTP basic credentials against an htpasswd file, as Apache\nand nginx do.")
  (holds (file *htpasswd.File))
  (about-new
    "New{impl} checks against file, loaded by the caller with htpasswd.New.")
  (new (params (file *htpasswd.File))
    "return &{impl}{file: file}")

  (hint authenticate
    "Call user, pass, ok := r.BasicAuth(); when ok is false, return ErrUnauthenticated. Call s.file.Match(user, pass); when it is false, return ErrUnauthenticated, without saying whether the user exists. Otherwise return a Principal whose Subject is user."))
