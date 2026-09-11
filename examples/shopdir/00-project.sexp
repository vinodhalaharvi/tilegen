; The shop example, split across files. tilegen reads every .sexp file in
; this directory in name order (hence the number prefixes) and the merge
; pass links them into one project. It compiles to exactly the same Go as
; ../shop/spec.sexp - a test checks that.
(project shop
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))

  ; No (module ...): it is derived from the repository, github.com/acme/shop.
  ; Run `tilegen -git examples/shopdir` to clone it if it exists on GitHub,
  ; or to git init, commit, create it with gh, and add topics if it does not.
  (repo
    (github acme/shop)
    (visibility private)
    (description "Orders and billing, scaffolded by tilegen.")
    (topics orders billing)))

; The house style can live next to the spec instead of in a -config file.
(config
  (json-tags snake)
  (context-first yes)
  (storage memory)
  (layout flat))
