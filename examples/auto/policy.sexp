; What this team values. Tiles declare costs; the policy prices them.
; Try `tilegen explain examples/auto` with this file changed or removed.
(policy
  (weights
    (llm-work 4)        ; how much the LLM must write
    (maintenance 3)     ; how much there is to keep working
    (dependency 1)      ; how much it drags in
    (runtime 1)
    (uncertainty 5))    ; how unsure we are it will work

  ; (prefer postgres-sqlc) (margin 8)     ; break near-ties its way
  ; (avoid pgx "we standardised on sqlc") ; never choose it
  )
