(tile spiffe
  (offers auth)
  (doc "workload identity: the caller is the SPIFFE ID in its certificate, verified in the handshake")
  (type-name SPIFFEAuthenticator)
  (engine spire)
  (import spiffeid github.com/spiffe/go-spiffe/v2/spiffeid)
  (import x509svid github.com/spiffe/go-spiffe/v2/svid/x509svid)
  (topics spiffe spire mtls)

  (satisfies (offline-verify)
    "the certificate is verified against the trust bundle during the handshake, with no call per request")
  (satisfies (expiring)
    "an SVID past its expiry fails the handshake, and they are issued for hours, not years")
  (illegal-when (third-party-idp)
    "the identities are workloads', issued by your own SPIRE server, not people's from an identity provider's login")
  (unverified (revocable)
    "an SVID is not checked against a revocation list: a workload taken out of the registry keeps working until its certificate expires")

  (cost
    (llm-work 2 (source derived "read the verified certificate and take the identity from it"))
    (maintenance 3 (source derived "registration entries to keep in step with what is deployed"))
    (dependency 3 (source derived "go-spiffe and its gRPC tree"))
    (runtime 1 (source derived "nothing per request: the handshake did the work"))
    (operations 6 (source derived "a SPIRE server and an agent on every node to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} names the caller by the SPIFFE ID in the certificate the TLS\nhandshake verified. It verifies nothing itself: the server's tls.Config\nmust come from go-spiffe's tlsconfig.MTLSServerConfig.")
  (holds (domain spiffeid.TrustDomain))
  (about-new
    "New{impl} accepts workloads whose SPIFFE ID is in the given trust domain.")
  (new (params (domain spiffeid.TrustDomain))
    "return &{impl}{domain: domain}")

  (hint authenticate
    "The handshake has already verified the certificate against the trust bundle; this method only reads the identity. When r.TLS is nil or r.TLS.PeerCertificates is empty, return ErrUnauthenticated. Call id, err := x509svid.IDFromCert(r.TLS.PeerCertificates[0]); an error means the certificate carries no SPIFFE ID: ErrUnauthenticated. Require id.MemberOf(s.domain), and return ErrUnauthenticated for an identity from another trust domain. Return a Principal whose Subject is id.String()."))
