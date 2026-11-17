(tile cognito
  (offers auth)
  (doc "Amazon Cognito access tokens, checked with the user pool on every request")
  (type-name CognitoAuthenticator)
  (engine cognito)
  (import cognitoidentityprovider github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider)
  (import types github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types)
  (import aws github.com/aws/aws-sdk-go-v2/aws)
  (topics cognito aws)

  (satisfies (revocable)
    "the user pool is asked on every request, so a token it has revoked, or whose user signed out everywhere, stops working on the next one")
  (satisfies (third-party-idp)
    "people sign in to the user pool, which issues the token")
  (satisfies (expiring)
    "the user pool refuses an access token past its expiry")
  (illegal-when (offline-verify)
    "every request is a call to Cognito")

  (cost
    (llm-work 3 (source derived "one call with the token, an error type to tell refusal from failure, and an attribute list to read"))
    (maintenance 2 (source derived "the region and credentials to keep right, and a request quota to stay under"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 4 (source derived "a call to Cognito on every request, and nothing works while it cannot be reached"))
    (operations 5 (source derived "an identity provider to pay for, with its user pool, app clients and quotas to keep right"))
    (uncertainty 2 (source derived "written from the library's documentation; no authenticator generated from this tile has been run against the real thing")))

  (about
    "{impl} asks Amazon Cognito whose access token a request carries, on\nevery request. Nothing is verified here, and nothing is cached.")
  (holds (client *cognitoidentityprovider.Client))
  (about-new
    "New{impl} asks through the given client, configured for the user pool's\nregion.")
  (new (params (client *cognitoidentityprovider.Client))
    "return &{impl}{client: client}")

  (hint authenticate
    "Take the bearer token from the Authorization header; without one, return ErrUnauthenticated. Call out, err := s.client.GetUser(r.Context(), &cognitoidentityprovider.GetUserInput{AccessToken: aws.String(the token)}). It takes an access token, not an ID token. When errors.As finds a *types.NotAuthorizedException, the token is invalid, expired or revoked: return ErrUnauthenticated. Any other error is yours to return: Cognito could not be asked. Return a Principal whose Subject is the Value of the attribute in out.UserAttributes whose Name is \"sub\", or aws.ToString(out.Username) when there is none."))
