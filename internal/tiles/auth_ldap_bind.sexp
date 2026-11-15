(tile ldap-bind
  (offers auth)
  (doc "a directory checks the password: HTTP Basic credentials, tried as an LDAP bind")
  (alias ldap)
  (type-name LDAPAuthenticator)
  (engine ldap)
  (import ldap github.com/go-ldap/ldap/v3)
  (topics ldap)

  (satisfies (revocable)
    "every request binds, so an account disabled in the directory stops working on the next one")
  (satisfies (third-party-idp)
    "identities and passwords live in the directory, not here")
  (illegal-when (offline-verify)
    "every request is a connection and a bind to the directory")
  (illegal-when (expiring)
    "a password works until the directory says otherwise; nothing in the credential says when to stop trusting it")

  (cost
    (llm-work 3 (source derived "dial, bind, and tell a wrong password from a directory that is down"))
    (maintenance 3 (source derived "the DN pattern and the directory's TLS to keep right, and passwords cross the wire on every request"))
    (dependency 1 (source derived "go-ldap and a few small modules"))
    (runtime 4 (source derived "a connection and a bind per request"))
    (operations 5 (source derived "a directory to run, or to depend on someone else running"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} checks HTTP Basic credentials by binding to an LDAP directory as\nthat user. The directory decides; nothing is stored here.")
  (holds (url string) (userDN string))
  (about-new
    "New{impl} binds to the directory at url, an ldaps:// address. userDN is\nthe pattern for a user's distinguished name, with %s where the user name\ngoes: \"uid=%s,ou=people,dc=example,dc=org\".")
  (new (params (url string) (userDN string))
    "return &{impl}{url: url, userDN: userDN}")

  (hint authenticate
    "Take the user name and password with r.BasicAuth(). When it reports none, or the password is empty, return ErrUnauthenticated: a directory treats a bind with an empty password as anonymous and accepts it. Call conn, err := ldap.DialURL(s.url); a failure to reach the directory is your own error, returned as it is. Defer conn.Close(). Call conn.Bind(fmt.Sprintf(s.userDN, ldap.EscapeDN(user)), password): escaping the name keeps it from changing the DN. When ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials), return ErrUnauthenticated; any other error is your own. Return a Principal whose Subject is the user name."))
