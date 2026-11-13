; A service you can run: (app) asks for the program as well as its parts.
;
;   tilegen -out out/app examples/app/spec.sexp
;   cd out/app && go mod tidy && API_TOKEN=x go run ./cmd/notes
;
; cmd/notes/wire_gen.go builds the store, the bus and the handler in
; dependency order, and Serve listens on $ADDR (default :8080). It is
; worked out from the constructors this run generates, so nothing here
; says how the parts connect. Every tile chosen below builds from the
; others, so Deps is empty and the program starts as generated; the
; requests it serves reach the store's methods, which are holes until
; they are filled.
(project notes
  (module github.com/acme/notes)
  (go 1.22)
  (app)

  (package notes
    (doc "Package notes keeps short notes.")

    (entity Note
      (field ID string)
      (field Text string)
      (store get list save))

    (events
      (event NoteSaved (field ID string)))

    (api (get Note) (list Note) (save Note))

    (secrets
      (secret APIToken (key api_token)))))
