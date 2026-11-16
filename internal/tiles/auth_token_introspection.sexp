(tile token-introspection
  (offers auth)
  (doc "bearer tokens the issuer is asked about on every request (OAuth 2.0 token introspection)")
  (alias introspect)
  (type-name IntrospectionAuthenticator)
  (topics oauth2 introspection)

  (satisfies (revocable)
    "the issuer is asked on every request, so a token it has revoked stops working on the next one")
  (satisfies (third-party-idp)
    "tokens are issued elsewhere, and the issuer says who each belongs to")
  (satisfies (expiring)
    "the issuer answers that a token past its expiry is not active")
  (illegal-when (offline-verify)
    "every request is a call to the issuer")

  (cost
    (llm-work 3 (source derived "a form post with client credentials, and the reply to decode"))
    (maintenance 2 (source derived "the endpoint and this service's client credentials to keep right"))
    (dependency 0 (source derived "the standard library"))
    (runtime 4 (source derived "a call to the issuer on every request, and nothing works while it is down"))
    (operations 5 (source derived "an identity provider to run or pay for, with its clients and keys to keep right"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} asks the token's issuer whether a bearer token is still good,\non every request. Nothing is verified here, and nothing is cached.")
  (holds (client *http.Client) (endpoint string) (clientID string) (clientSecret string))
  (about-new
    "New{impl} asks the introspection endpoint at endpoint, authenticating\nas clientID. Give client a timeout: it is on the path of every request.")
  (new (params (client *http.Client) (endpoint string) (clientID string) (clientSecret string))
    "return &{impl}{client: client, endpoint: endpoint, clientID: clientID, clientSecret: clientSecret}")

  (hint authenticate
    "Take the bearer token from the Authorization header; without one, return ErrUnauthenticated. Build a POST to s.endpoint with http.NewRequestWithContext and the incoming request's context, the body url.Values{\"token\": {the token}}.Encode(), Content-Type application/x-www-form-urlencoded, and SetBasicAuth(s.clientID, s.clientSecret). Send it with s.client and close the reply's body. A transport error, or a status other than 200, is your own error to return, not ErrUnauthenticated: the issuer could not be asked. Decode the JSON reply into a struct with Active bool, Sub string and Scope string, tagged active, sub and scope. When Active is false, return ErrUnauthenticated. Otherwise return a Principal whose Subject is Sub and whose Scopes are strings.Fields(Scope)."))
