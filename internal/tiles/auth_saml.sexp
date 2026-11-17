(tile saml
  (offers auth)
  (doc "SAML single sign-on through crewjam/saml: the session its service-provider middleware keeps")
  (type-name SAMLAuthenticator)
  (import samlsp github.com/crewjam/saml/samlsp)
  (topics saml sso)

  (satisfies (third-party-idp)
    "people sign in at the identity provider, which asserts who they are")
  (satisfies (offline-verify)
    "the session is a signed cookie, verified here with no call per request")
  (satisfies (expiring)
    "the session cookie carries an expiry, an hour unless the middleware is told otherwise")
  (unverified (revocable)
    "a session lasts until its cookie expires unless the middleware is given a store of revocations to consult, which is the caller's to set up")

  (cost
    (llm-work 3 (source derived "read the session the middleware keeps, and take the subject from its claims"))
    (maintenance 4 (source derived "metadata to exchange with the identity provider, a certificate that expires, and the middleware's own routes to mount"))
    (dependency 3 (source derived "crewjam/saml and its XML signature modules"))
    (runtime 1 (source derived "a signature check per request"))
    (operations 5 (source derived "an identity provider to run or pay for, with its clients and keys to keep right"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} reads the caller from the session that crewjam/saml's middleware\nkeeps after a SAML sign-in. Redirecting to the identity provider and\nreceiving its assertion are the middleware's work, on its own routes.")
  (holds (sp *samlsp.Middleware))
  (about-new
    "New{impl} reads sessions from sp, built by the caller with samlsp.New and\nmounted at /saml/.")
  (new (params (sp *samlsp.Middleware))
    "return &{impl}{sp: sp}")

  (hint authenticate
    "Call session, err := s.sp.Session.GetSession(r). When errors.Is(err, samlsp.ErrNoSession), nobody is signed in: return ErrUnauthenticated. Any other error is yours to return. The default session is a samlsp.JWTSessionClaims: when session.(samlsp.JWTSessionClaims) holds, return a Principal whose Subject is its Subject, the name the identity provider asserted. For anything else that is a samlsp.SessionWithAttributes, take the subject from GetAttributes().Get(\"uid\"); with no subject either way, return ErrUnauthenticated."))
