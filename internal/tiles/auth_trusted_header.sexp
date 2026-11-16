(tile trusted-header
  (offers auth)
  (doc "a reverse proxy signs people in and names them in a header this service trusts only from it")
  (alias header)
  (type-name HeaderAuthenticator)
  (topics reverse-proxy)

  (satisfies (third-party-idp)
    "the proxy in front signs people in, with whatever identity provider it is given")
  (satisfies (offline-verify)
    "nothing is called: the proxy already did the asking")
  (unverified (revocable)
    "the proxy decides on every request, and how soon it stops vouching for someone is its configuration, not anything here")
  (unverified (expiring)
    "how long a sign-in lasts is the proxy's session lifetime, not anything here")

  (cost
    (llm-work 2 (source derived "check where the request came from, then read a header"))
    (maintenance 4 (source derived "the network must make the proxy the only way in, and the list of its addresses must follow it"))
    (dependency 0 (source derived "the standard library"))
    (runtime 1 (source derived "nothing to compute"))
    (operations 4 (source derived "an authenticating proxy to run in front of every instance"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} takes the caller's name from a header set by a reverse proxy that\nhas already signed them in. The header is believed only from that proxy.")
  (holds (header string) (proxies "[]netip.Prefix"))
  (about-new
    "New{impl} reads the caller from header, such as X-Forwarded-User, on\nrequests that arrive from an address inside proxies.")
  (new (params (header string) (proxies "[]netip.Prefix"))
    "return &{impl}{header: header, proxies: proxies}")

  (hint authenticate
    "Believe the header only from the proxy. Parse r.RemoteAddr with netip.ParseAddrPort and require its address to be inside one of s.proxies; otherwise return ErrUnauthenticated whatever the header says, because anyone who can reach this service directly can set it. Do not use X-Forwarded-For for this check: it is as forgeable as the header itself. Then read r.Header.Get(s.header); empty is ErrUnauthenticated. Return a Principal whose Subject is the header's value."))
