; "Save the payment and publish the event, or do neither."
;
;   tilegen explain examples/outbox/spec.sexp
;   tilegen -out out/outbox examples/outbox/spec.sexp
;
; (outbox Payment) is the whole requirement. Nothing here names a
; database, yet PaymentStore lands on Postgres: an outbox is one
; transaction over the payment and the event, and of the store tiles only
; postgres-sqlc generates one. explain says why each of the others is out.
(project ledger
  (module github.com/acme/ledger)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))

  (package ledger
    (doc "Package ledger records payments.")

    (entity Payment
      (field ID uuid.UUID)
      (field Cents int64)
      (store get list save))

    (events
      (event PaymentTaken (field ID uuid.UUID) (field Cents int64))
      (event PaymentRefunded (field ID uuid.UUID))
      (outbox Payment))))
