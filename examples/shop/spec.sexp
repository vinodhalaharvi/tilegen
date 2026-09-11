; A tiny shop. This file is the single source of truth: change it and
; re-run tilegen; never edit files marked "DO NOT EDIT".
(project shop
  (module github.com/acme/shop)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))

  (package orders
    (doc "Package orders tracks customer orders.")

    ; An entity is sugar: expand lowers it to a struct, an OrderStore
    ; interface, and an (impl ...) that becomes LLM tasks.
    (enum Status
      (doc "Status is where an order is in its lifecycle.")
      pending paid shipped cancelled)

    (entity Order
      (doc "Order is a placed customer order.")
      (field ID uuid.UUID)
      (field CustomerEmail string)
      (field TotalCents int64 (doc "Total in minor currency units."))
      (field PlacedAt time.Time)
      (field ShippedAt *time.Time)
      (field Status Status)
      (store get list save delete
        (list-by CustomerEmail)
        count
        (constraint "Save must be idempotent for the same ID.")
        (constraint "Never log CustomerEmail.")))

    ; Plain forms pass straight through to Go.
    (interface Pricer
      (doc "Pricer computes order totals.")
      (method Quote
        (doc "Quote returns the total for o in minor units.")
        (params (o *Order))
        (returns int64 error)))

    ; An explicit hole: pure intent, no structure yet.
    (llm "Write a Pricer that applies a 10% discount to orders over 100.00."))

  ; A second package. Types from other project packages are written
  ; qualified (orders.Order); tilegen adds the import and rejects
  ; import cycles between packages.
  (package billing
    (doc "Package billing invoices orders.")

    (entity Invoice
      (doc "Invoice bills one order.")
      (field ID uuid.UUID)
      (field OrderID uuid.UUID)
      (field AmountCents int64)
      (field IssuedAt time.Time)
      (store get save))

    (interface Invoicer
      (doc "Invoicer turns orders into invoices.")
      (method InvoiceFor
        (doc "InvoiceFor builds the invoice for o, priced by p.")
        (params (o *orders.Order) (p orders.Pricer))
        (returns *Invoice error)))))
