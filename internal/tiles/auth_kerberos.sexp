(tile kerberos
  (offers auth)
  (doc "Kerberos over HTTP (SPNEGO) through gokrb5: the identity its middleware verified")
  (alias spnego)
  (type-name KerberosAuthenticator)
  (engine kerberos)
  (import goidentity github.com/jcmturner/goidentity/v6)
  (topics kerberos spnego active-directory)

  (satisfies (third-party-idp)
    "the key distribution centre, often Active Directory, signs people in and issues the ticket")
  (satisfies (offline-verify)
    "the ticket is verified with this service's keytab, with no call to the key distribution centre per request")
  (satisfies (expiring)
    "a service ticket carries an end time, hours away, and is refused after it")
  (illegal-when (revocable)
    "a ticket is good until it expires: a disabled account keeps working for as long as its ticket lasts")

  (cost
    (llm-work 2 (source derived "read the identity the middleware put on the request"))
    (maintenance 4 (source derived "a keytab to issue and renew, a service principal name that must match the host, and clocks that must agree"))
    (dependency 3 (source derived "gokrb5 and its ASN.1 and cryptography modules"))
    (runtime 1 (source derived "a ticket decrypted per request"))
    (operations 6 (source derived "a key distribution centre, and every client joined to its realm"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} names the caller from the Kerberos ticket that gokrb5's SPNEGO\nmiddleware verified. It verifies nothing itself: the handler must be wrapped\nin spnego.SPNEGOKRB5Authenticate with this service's keytab.")
  (holds (realm string))
  (about-new
    "New{impl} accepts callers whose ticket is from the given realm.")
  (new (params (realm string))
    "return &{impl}{realm: realm}")

  (hint authenticate
    "The request must already have passed through gokrb5's spnego.SPNEGOKRB5Authenticate, which answers the negotiation, verifies the ticket and puts the caller on the request's context. Read id := goidentity.FromHTTPRequestContext(r). A nil id, or one whose Authenticated() is false, is ErrUnauthenticated. Require id.Domain() to equal s.realm, and return ErrUnauthenticated for a ticket from another realm. Return a Principal whose Subject is id.UserName() + \"@\" + id.Domain()."))
