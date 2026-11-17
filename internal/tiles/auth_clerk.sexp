(tile clerk
  (offers auth)
  (doc "Clerk session tokens, verified against Clerk's published keys through its SDK")
  (type-name ClerkAuthenticator)
  (engine clerk)
  (import jwt github.com/clerk/clerk-sdk-go/v2/jwt)
  (import clerk github.com/clerk/clerk-sdk-go/v2)
  (topics clerk sessions)

  (satisfies (third-party-idp)
    "people sign in with Clerk, which issues the session token")
  (satisfies (expiring)
    "a session token lasts a minute and is refused after it; the browser keeps getting new ones while the session lives")
  (unverified (revocable)
    "a session ended at Clerk stops getting new tokens, so it is refused within about a minute, not on the next request")
  (unverified (offline-verify)
    "the signature is checked here, and the key it is checked with is fetched from Clerk unless the caller passes one in")

  (cost
    (llm-work 2 (source derived "one verify call, then take the subject from the claims"))
    (maintenance 2 (source derived "a secret key to keep and rotate, and the SDK to keep current"))
    (dependency 2 (source derived "the Clerk SDK and its JOSE module"))
    (runtime 2 (source derived "a signature check per request, and a key fetch now and then"))
    (operations 5 (source derived "an identity provider to pay for, with its application and keys to keep right"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} verifies the session token Clerk's frontend sends with each\nrequest. Signing in happens at Clerk.")
  (holds (verify "func(context.Context, *jwt.VerifyParams) (*clerk.SessionClaims, error)"))
  (about-new
    "New{impl} verifies with the Clerk SDK, which reads the secret key the\ncaller set with clerk.SetKey.")
  (new (params )
    "return &{impl}{verify: jwt.Verify}")

  (hint authenticate
    "Take the bearer token from the Authorization header, or failing that the __session cookie, which is where Clerk's frontend puts it for same-origin requests; with neither, return ErrUnauthenticated. Call claims, err := s.verify(r.Context(), &jwt.VerifyParams{Token: the token}). Any error is ErrUnauthenticated. Return a Principal whose Subject is claims.Subject, Clerk's user id."))
