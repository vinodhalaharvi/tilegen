; A service's secrets, read from wherever its requirements allow.
;
;   tilegen explain examples/secrets/spec.sexp
;   tilegen -out out/secrets examples/secrets/spec.sexp
;
; With no requirement the environment wins: nothing to install, nothing
; to run. Add (rotating) and the environment is ruled out, since a process
; cannot see a variable change; mounted files take over. Add (audited) and
; only Vault is left.
(project orders
  (module github.com/acme/orders)
  (go 1.22)

  (package orders
    (doc "Package orders takes and tracks customer orders.")

    (secrets
      (secret DatabaseURL)
      (secret SigningKey (doc "signs session tokens."))
      (secret OAuthClientID (key oauth_client_id)))))   ; OAUTH_CLIENT_ID, not O_AUTH_CLIENT_ID
