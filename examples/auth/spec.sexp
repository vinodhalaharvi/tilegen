; Three packages that authenticate differently, from the same vocabulary.
; No package names a tile: each says what it needs, and selection answers.
;
;   tilegen explain examples/auth/spec.sexp
;
; console  asks for (third-party-idp), so only oidc is left standing
; portal   asks for (revocable) and (expiring), so sessions win on cost
; ingest   asks for nothing, so the cheapest legal scheme wins
;
; The generated Protect middleware is not wired into Routes(): compose it
; yourself, so a package can have public routes as well as guarded ones.
;
;   http.Handle("/", console.Protect(a, h.Routes()))

(project gate
  (module example.com/gate)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))

  (package console
    (doc "The staff console. Staff sign in through the company's identity provider.")

    (entity Report
      (field ID uuid.UUID)
      (field Title string)
      (field Body string)
      (store get list (durable)))

    (auth
      (doc "Principal is the member of staff a console request is from.")
      (third-party-idp)   ; identities come from the company IdP, not from here
      (revocable))        ; a leaver must stop working before their token expires

    (http
      (doc "The reports API, to be served behind Protect.")
      (route GET "/reports"      (list Report))
      (route GET "/reports/{id}" (get Report))))

  (package portal
    (doc "The customer portal. Sessions are issued here and can be ended here.")

    (entity Session
      (doc "Session is one signed-in customer.")
      (field ID string)
      (field Subject string)
      (field ExpiresAt time.Time)
      (store get save delete (durable)))

    (auth
      (doc "Principal is the customer a portal request is from.")
      (revocable)         ; signing out has to take effect on the next request
      (expiring)))        ; an abandoned session must stop working on its own

  (package ingest
    (doc "Machines, not people: one key per device, revoked by deleting it.")

    (entity Sample
      (field ID uuid.UUID)
      (field DeviceID string)
      (field Value float64)
      (store save (durable)))

    (auth
      (doc "Principal is the device a sample came from."))))
