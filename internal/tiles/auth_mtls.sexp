(tile mtls
  (offers auth)
  (doc "mutual TLS: the caller is whoever its client certificate says, verified in the handshake")
  (type-name MTLSAuthenticator)
  (topics mtls)

  (satisfies (offline-verify)
    "the certificate is verified against the CA's certificate during the handshake, with no call to anyone")
  (satisfies (expiring)
    "a certificate past its NotAfter fails the handshake")
  (illegal-when (third-party-idp)
    "certificates come from a certificate authority you run, not from an identity provider's login")
  (unverified (revocable)
    "a certificate is trusted until it expires unless the server checks a revocation list or OCSP, and Go's TLS does neither by itself")

  (cost
    (llm-work 2 (source derived "read the verified certificate and map its name"))
    (maintenance 5 (source derived "a certificate for every caller to issue, distribute and renew before it lapses"))
    (dependency 0 (source derived "the standard library"))
    (runtime 1 (source derived "nothing per request: the handshake did the work"))
    (operations 4 (source derived "a certificate authority to run and keep safe"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} names the caller from the client certificate the TLS handshake\nverified. It verifies nothing itself: the server's tls.Config must require\nand verify client certificates.")
  (holds (principal "func({ctx-type}string, []string) (*Principal, error)"))
  (about-new
    "New{impl} maps a verified certificate's common name and DNS names to a\ncaller with principal, which returns nil for a name it does not know.")
  (new (params (principal "func({ctx-type}string, []string) (*Principal, error)"))
    "return &{impl}{principal: principal}")

  (hint authenticate
    "The handshake has already verified the certificate, provided the server's tls.Config sets ClientAuth to tls.RequireAndVerifyClientCert and ClientCAs to the issuing CA; this method only reads it. When r.TLS is nil or r.TLS.PeerCertificates is empty, return ErrUnauthenticated. Otherwise take cert := r.TLS.PeerCertificates[0], the caller's own certificate, and return s.principal(cert.Subject.CommonName, cert.DNSNames); a nil principal is ErrUnauthenticated."))
